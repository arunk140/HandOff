# HandOff — Wiki

Quirks, edge cases, and design decisions in the current implementation.

---

## Table of Contents

- [Body Buffering](#body-buffering)
- [Webhook Context Lifecycle](#webhook-context-lifecycle)
- [Response Status Capture](#response-status-capture)
- [Path Matching Details](#path-matching-details)
- [First-Match-Wins Routing](#first-match-wins-routing)
- [Backend Proxy Caching](#backend-proxy-caching)
- [Secrets Loading](#secrets-loading)
- [Config Hot Reload Edge Cases](#config-hot-reload-edge-cases)
- [follow_redirects — Unused in Proxy](#follow_redirects--unused-in-proxy)
- [No X-Forwarded Headers](#no-x-forwarded-headers)
- [Template Engine Details](#template-engine-details)
- [Webhook Header Rendering](#webhook-header-rendering)
- [Retry Backoff Math](#retry-backoff-math)
- [Request ID Format](#request-id-format)
- [Client IP Detection](#client-ip-detection)
- [Timeout: Proxy vs Webhook](#timeout-proxy-vs-webhook)
- [TLS Reload on Hot Reload](#tls-reload-on-hot-reload)
- [Graceful Shutdown Behavior](#graceful-shutdown-behavior)
- [Thread Safety](#thread-safety)

---

## Body Buffering

The entire request body is read into memory (`io.ReadAll`) before forwarding to the backend. This is necessary because:

1. The body must be available for webhook `body` payload mode after proxying.
2. `httputil.ReverseProxy` consumes the `io.ReadCloser` — after it reads the body, it's gone.
3. The webhook fires in a goroutine after the proxy has already consumed the body.

**Implication:** Large request bodies (>10MB) will use significant memory. Streaming is not supported in v1. If your service handles large file uploads, consider:
- Not including those routes in HandOff
- Using `metadata` or `custom` payload modes instead of `body`
- Adding middleware in front of HandOff that rejects oversized bodies

**Implementation detail:**
```go
bodyBytes, _ := io.ReadAll(r.Body)
r.Body.Close()
r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
```

This replaces the original body with a re-readable buffer, allowing the proxy to consume it while keeping a copy for the webhook.

---

## Webhook Context Lifecycle

**Current behavior:** Webhooks fire with `context.Background()`. There is no timeout context wrapping the dispatcher.

**Why:** The initial implementation used `context.WithTimeout(context.Background(), 60*time.Second)` with `defer cancel()`. However, `Dispatcher.Fire()` spawns goroutines and returns immediately. This means the `defer cancel()` ran before the spawned goroutines completed, canceling their HTTP requests with "context canceled" errors.

**Current design:** Each `HTTPAction` has its own `http.Client` with a 30-second timeout, providing per-request timeout protection. The dispatcher itself runs unbounded — if a webhook hangs forever, the goroutine leaks. This is acceptable for v1 since webhooks are fire-and-forget by definition.

**Future improvement:** Track goroutines with a `sync.WaitGroup` or `errgroup` and cancel context after all complete.

---

## Response Status Capture

The status code is captured via a custom `responseRecorder` that wraps `http.ResponseWriter`:

```go
type responseRecorder struct {
    http.ResponseWriter
    statusCode int
    wrote      bool
}
```

**Quirk:** If the backend writes a response body without calling `WriteHeader()`, Go defaults to `200 OK`. The recorder catches this by setting `statusCode = 200` on the first `Write()` call. This means:
- Streaming responses that never call `Write()` (e.g., WebSocket upgrades, 101 Switching Protocols) will show status 0 in webhook metadata.
- Empty 204/304 responses are captured correctly since they call `WriteHeader()`.

The status code in webhook metadata is the backend's **response** status, not the status HandOff sends to the client (which is identical since HandOff doesn't modify responses).

---

## Path Matching Details

### Stripping of leading/trailing slashes

Paths are trimmed of leading and trailing slashes before segmentation:

```
pattern: "/api/v2/"   → segments: ["api", "v2"]
path:    "/api/v2/"   → segments: ["api", "v2"]
```

This means `/api/v2` and `/api/v2/` are considered equal. A pattern with a trailing slash and one without are also considered equal.

### `*` matches exactly one non-empty segment

```
Pattern: /users/*/posts
Matches: /users/123/posts       ✅
Does NOT match: /users//posts   ❌  (empty segment)
Does NOT match: /users/123/456/posts  ❌  (* can't match /)
```

### `**` matches zero or more segments

```
Pattern: /api/**
Matches: /api                   ✅  (zero segments after api)
Matches: /api/                  ✅  (zero segments after api)
Matches: /api/users             ✅  (one segment)
Matches: /api/users/123/posts   ✅  (three segments)
```

**Implementation:** Recursive segment matching. When `**` is encountered:
- If `**` is the last pattern segment, it matches everything (returns immediately).
- Otherwise, the matcher tries matching the remaining pattern at each position in the remaining path (backtracking).

### Regex limitations

Regex patterns are compiled with `regexp.Compile` (not `MustCompile`). Invalid regex silently returns false (no match). This means a typo in a regex pattern will cause the route to never match, rather than crashing.

### First-match wins

Routes are evaluated in order. The first route that matches both path and method wins. Reorder routes to control priority:

```yaml
routes:
  - path: "/api/special"     # Checked first
    backend: "https://special.example.com"
  - path: "/api/**"          # Fallback catch-all
    backend: "https://general.example.com"
```

---

## First-Match-Wins Routing

Routes are evaluated in array order. If multiple routes could match a request, only the first one is used. This includes webhook dispatch — only that first route's webhooks fire.

**No composite matching:** There is no concept of "match multiple routes and fire all their webhooks." If you need webhook A to fire on `/api/**` and webhook B to fire on `/api/users/**`, you must configure both webhooks on a single route, or use a single route with `body` payload that includes path information.

---

## Backend Proxy Caching

`httputil.ReverseProxy` instances are cached in a `sync.Map` keyed by backend URL:

```go
h.proxies.Store(backend, proxy)
```

**Implications:**
- First request to a new backend creates the proxy (one-time overhead).
- Same backend URL across multiple routes shares the same proxy (connection pooling).
- On config hot reload: old proxies for removed backends stay in the cache until GC. This is a small memory leak for configs that change backends frequently.
- The proxy's transport settings (timeout) are set at proxy creation time. If `global.timeout` changes on reload, existing cached proxies keep their old timeout. Only new backend URLs get the new timeout.

**Mitigation:** If you change `global.timeout`, restart the process or use `SIGUSR1` reload with a new backend URL to ensure fresh proxy instances.

---

## Secrets Loading

Secrets are loaded from a YAML file via the `-secrets` flag:

```bash
./handoff -c config.yaml -secrets secrets.yaml
```

**Current behavior:**
- Loaded once at startup
- NOT reloaded on `SIGUSR1` or fsnotify events
- Available in templates as `{{.Secrets.key_name}}`
- If the secrets file is missing, a warning is logged and secrets default to empty map

**Template access:** Secrets are populated into the `PayloadContext.Secrets` map before template rendering. This is done in `HTTPAction.Execute()`, not globally — each webhook execution gets a fresh copy.

**Security note:** `{{.Secrets}}` values are excluded from JSON serialization (tagged `json:"-"`) so they never appear in metadata payloads sent over the network.

---

## Config Hot Reload Edge Cases

| Scenario | Behavior |
|----------|----------|
| Config file changed on disk | `fsnotify` triggers reload → validates → atomically swaps |
| `SIGUSR1` sent | Triggers immediate reload regardless of file state |
| Invalid config on reload | Error logged, old config remains active. No downtime. |
| Config file deleted | `fsnotify` may stop receiving events. `SIGUSR1` still works. |
| New routes added on reload | Active immediately via atomic pointer. Next request picks them up. |
| Routes removed on reload | Active immediately. In-flight requests still use old config pointer (already loaded). |
| `global.timeout` changed on reload | Only new proxy instances use new value (see Backend Proxy Caching) |
| TLS settings changed on reload | Server TLS is set at startup. TLS changes require restart. |

**Architecture:** The config is stored in an `atomic.Pointer[config.Config]`. Every request calls `h.cfg.Load()` to get the current config, then builds a new `matcher.Matcher` from it. The `sync.Map` proxy cache is not tied to the config — it's managed separately.

---

## follow_redirects — Unused in Proxy

The `global.follow_redirects` field is present in the config but **not enforced** by the reverse proxy.

**Why:** `httputil.ReverseProxy` uses `http.Transport` (a `RoundTripper`) directly, not `http.Client`. `http.Transport` does not follow redirects — it returns the redirect response as-is. Only `http.Client` follows redirects (via its `CheckRedirect` callback).

The field is kept in the config for future use, potentially for:
- Webhook HTTP client redirect behavior
- A future `http.Client`-based proxy mode

For now, the field is parsed and validated but has no effect.

---

## No X-Forwarded Headers

HandOff does **not** add `X-Forwarded-For`, `X-Forwarded-Proto`, or `X-Forwarded-Host` headers to the backend request.

**Impact:** If your backend relies on these headers for client IP detection, it will see the IP of the HandOff proxy, not the original client.

**Workaround:** If your network setup already includes a load balancer or CDN that sets these headers, HandOff preserves them (they're part of the original request headers and are forwarded as-is).

**Client IP for webhooks:** The `client_ip` field in webhook metadata is extracted by HandOff (from `X-Forwarded-For`, `X-Real-IP`, or `RemoteAddr`) and is always available regardless of backend header forwarding.

---

## Template Engine Details

### text/template, not html/template

Templates use Go's `text/template` package. This means:
- No HTML escaping. Values are rendered as-is.
- Suitable for JSON, URL parameters, and header values.
- Unsuitable for rendering HTML webhook bodies (use `custom` payload with proper HTML escaping if needed).

### Sprig v3 functions

All [Sprig v3](https://masterminds.github.io/sprig/) functions are available. Notable ones:

```
String:   upper, lower, trim, replace, quote, trunc
Date:     date, dateModify, now, dateInZone
Math:     add, sub, mul, div, max, min
List:     list, first, last, join, sortAlpha
Dict:     dict, set, unset, hasKey, keys, values
Type:     typeOf, kindOf
Encoding: b64enc, b64dec, fromJson, toJson
Crypto:   sha256sum
Env:      env (read environment variable)
```

Example: `{{.Request.Path | upper | replace "/" "-"}}`

### Template in URL and headers

Both the webhook URL and header keys/values are rendered as templates:

```yaml
url: "https://hooks.example.com/{{.Request.Path | b64enc}}"
headers:
  X-Event: "{{.Request.Method}}_{{.Response.StatusCode}}"
  X-Request-ID: "{{.Request.ID}}"
```

If template rendering fails for a header, a warning is logged and that header is skipped. The webhook still sends.

### Template syntax gotchas

YAML's block scalar (pipe `|`) is the safest way to write multi-line templates:

```yaml
template: |
  {
    "text": "{{.Request.Method}} {{.Request.Path}} -> {{.Response.StatusCode}}"
  }
```

For inline templates in YAML, double the curly braces or YAML may misinterpret them:
```yaml
template: '{"text":"{{"{{"}}.Request.Path{{"}}"}}"}'
```

---

## Webhook Header Rendering

Both header **keys** and **values** are template-rendered. This is unusual but powerful:

```yaml
headers:
  "X-{{.Request.Method}}": "{{.Request.Path}} via HandOff"
```

Produces: `X-GET: /api/users via HandOff`

If a header key template fails to render, that header is skipped (with a warning). If a header value template fails, that header is skipped.

---

## Retry Backoff Math

| Strategy | Delay formula | Delays for attempts 2,3,4 |
|----------|--------------|---------------------------|
| `exponential` (default) | `2^attempt * 1s` | 2s, 4s, 8s |
| `linear` | `attempt * 1s` | 2s, 3s, 4s |

Attempt numbering starts at 0. The first attempt is always immediate (no delay). Delays apply to retries (attempts ≥ 1).

```go
func backoff(strategy string, attempt int) time.Duration {
    switch strategy {
    case "linear":
        return time.Duration(attempt) * time.Second
    default:
        return time.Duration(math.Pow(2, float64(attempt))) * time.Second
    }
}
```

**Quirk:** The `attempts` field means total attempts (including the first). Setting `attempts: 1` means "try once, no retry." Setting `attempts: 3` means "try once, then retry up to 2 more times."

**If all attempts fail:** The error is logged at ERROR level. No fallback action is taken.

---

## Request ID Format

Request IDs are 8 random bytes hex-encoded, producing 16-character strings like `a1b2c3d4e5f6a7b8`.

Implementation:
```go
func generateID() string {
    b := make([]byte, 8)
    _, _ = rand.Read(b)  // crypto/rand
    return hex.EncodeToString(b)
}
```

This is not a UUID. Collision probability is ~10^-19 for a single pair, negligible for practical use. The ID is included in every log line and webhook metadata, making request tracing straightforward.

---

## Client IP Detection

Resolution order:

1. `X-Forwarded-For` header (first IP in the comma-separated list)
2. `X-Real-IP` header
3. `r.RemoteAddr` (strips port via `net.SplitHostPort`)

```go
func getClientIP(r *http.Request) string {
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        parts := strings.Split(xff, ",")
        return strings.TrimSpace(parts[0])
    }
    if xri := r.Header.Get("X-Real-IP"); xri != "" {
        return xri
    }
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return r.RemoteAddr
    }
    return host
}
```

**Security note:** `X-Forwarded-For` and `X-Real-IP` are client-supplied headers. In a trusted network setup (behind a load balancer that strips/overrides them), they're reliable. In a direct Internet-facing deployment, clients can spoof these headers.

---

## Timeout: Proxy vs Webhook

There are two distinct timeout mechanisms:

| Component | Timeout | Configurable | Behavior on timeout |
|-----------|---------|-------------|---------------------|
| Backend proxy transport | `global.timeout` (response header timeout) | Yes, via config | 504 Gateway Timeout to client |
| Webhook HTTP client | 30 seconds (hardcoded) | No | Webhook fails, logged as error |

The `global.timeout` is applied to the `http.Transport.ResponseHeaderTimeout`, which is the time to wait for response headers after the connection is established. It does NOT include:
- DNS resolution time
- TCP connection time (10s hardcoded in Dialer)
- Response body streaming time

The webhook HTTP client has a total timeout of 30s that covers the entire request lifecycle (DNS, connect, send, receive).

---

## TLS Reload on Hot Reload

TLS certificate changes on config hot reload do **not** take effect. The TLS configuration is applied to `net/http.Server` at startup and cannot be changed without restarting the process.

If you need to rotate TLS certificates:
1. Place new cert files at the configured paths
2. Restart HandOff (`SIGTERM` + start again)

The current config hot reload only affects routes, backends, webhooks, and timeout (for new proxy instances).

---

## Graceful Shutdown Behavior

On `SIGINT` or `SIGTERM`:

1. Log "shutting down" with signal name
2. Cancel the watcher context (stops fsnotify and signal goroutines)
3. Call `server.Shutdown(context.Background())` — which:
   - Stops accepting new connections
   - Waits for active connections to finish (no deadline on the context)
   - Returns `http.ErrServerClosed`

**Quirk:** The shutdown context has no deadline. If a long-running request is in flight, shutdown blocks indefinitely. Consider wrapping in `signal.NotifyContext` for a hard timeout in production.

**Webhooks during shutdown:** In-flight webhook goroutines are NOT waited on. They continue running in the background. If they haven't completed by the time the process exits, they're terminated by the OS. This is intentional — webhooks are fire-and-forget.

---

## Thread Safety

| Component | Mechanism |
|-----------|-----------|
| Config | `atomic.Pointer[config.Config]` — lock-free reads |
| Proxy cache | `sync.Map` — concurrent read/write safe |
| Secrets | `map[string]string` — read-only after startup, no locks needed |
| Webhook dispatch | Each webhook runs in its own goroutine, no shared mutable state |
| Logging | `slog` defaults are goroutine-safe |

No mutexes are used in the hot path. The only locking is internal to `sync.Map` during proxy cache writes (which happen once per backend URL).

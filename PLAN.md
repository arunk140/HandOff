# HandOff — Plan

A fire-and-forget webhook proxy. HandOff sits between clients and your backend services, matches incoming requests against configurable route patterns, and triggers webhooks asynchronously — without ever blocking or modifying the response.

```
Client ──► HandOff Proxy ──► Backend Service
                │
                ▼ (async, fire-and-forget)
           Webhook Actions
```

---

## Configuration

Routes are defined in a YAML file. Each route specifies a path pattern, HTTP methods, a backend to forward to, and one or more webhooks to trigger.

```yaml
listen:
  host: "0.0.0.0"
  port: 8080
  tls:
    enabled: false
    cert_file: "/etc/handoff/cert.pem"
    key_file: "/etc/handoff/cert.key"

global:
  timeout: 30s
  follow_redirects: true

routes:
  - path: "/api/v2/**"
    methods: ["POST", "PUT", "DELETE"]
    backend: "https://legacy.example.com"
    webhooks:
      - type: http
        url: "https://hooks.slack.com/..."
        method: POST
        headers:
          Authorization: "Bearer {{.Secrets.slack_key}}"
          Content-Type: application/json
        payload: metadata
        template: |
          {
            "event": "{{.Request.Method}} {{.Request.Path}}",
            "client_ip": "{{.Request.ClientIP}}",
            "status": {{.Response.StatusCode}},
            "latency_ms": {{.Response.LatencyMs}},
            "time": "{{.Timestamp.Format "2006-01-02T15:04:05Z07:00"}}",
            "request_id": "{{.Request.ID}}"
          }
        retry:
          attempts: 3
          backoff: exponential

  - path: "~/users/[0-9]+/activate$"
    methods: []
    backend: "https://api.example.com"
    webhooks:
      - type: http
        url: "https://internal-audit.company.com/log"
        payload: body
```

### Path Matching

| Pattern | Meaning |
|---------|---------|
| `/api/v2/**` | Glob — `**` matches any number of path segments (including zero) |
| `/users/*` | Glob — `*` matches exactly one non-empty path segment |
| `~/users/[0-9]+$` | Regex — `~` prefix switches to regex matching |
| (empty `methods`) | Matches all HTTP methods |

Implementation: glob patterns are split by `/` and matched segment-by-segment. `filepath.Match` is used for per-segment matching. Regex uses Go's `regexp.Compile`. Both trailing and leading slashes are stripped before matching.

### Payload Modes

| Mode    | Description |
|---------|-------------|
| `metadata` | Structured JSON with path, method, IP, status, latency, timestamp, request ID — no request/response body |
| `body`    | Raw request body forwarded as-is. Content-Type inherits from the original request or falls back to `application/octet-stream` |
| `custom`  | User-supplied Go template (`text/template` + Sprig v3) rendered with full request/response context |

### Templating

Webhook headers, URLs, and `custom` payload bodies support Go `text/template` with:

- `{{.Request}}` — method, path, headers (map of `[]string`), client IP, query string, request ID, content type
- `{{.Response}}` — status code, latency in milliseconds (`int64`)
- `{{.Timestamp}}` — `time.Time` of the request (supports `.Format` and Sprig date functions)
- `{{.Secrets}}` — key-value map from the secrets file
- [Sprig v3](https://masterminds.github.io/sprig/) function library (~70 functions: string, date, math, list, dict, etc.)

Both header keys and header values are template-rendered.

### TLS Support

Both HTTP and HTTPS are supported. Enable TLS by setting `tls.enabled: true` and providing paths to certificate and key files.

### Multiple Backends

`backend` is configured per-route. Different path patterns can forward to different backend services. The proxy creates and caches `httputil.ReverseProxy` instances per backend URL using `sync.Map`.

### Config Hot Reload

The proxy watches the config file for changes (via `fsnotify`) and reloads on `SIGUSR1`. The new config is validated before atomically swapping in via `atomic.Pointer`, ensuring zero-downtime updates.

**Note:** Proxy instances cached in `sync.Map` are not invalidated on reload. Old backends that are no longer referenced stay in memory until GC. New backends get fresh proxy instances on first request.

---

## Architecture

### Implemented Packages

| Package   | Responsibility | Status |
|-----------|---------------|--------|
| `main`    | Entry point — parse flags, load config, wire components, start server, graceful shutdown | ✅ Done |
| `config`  | Config structs, YAML unmarshalling, `Validate()`, secrets file loading | ✅ Done |
| `matcher` | Path matching engine — glob with `**` support, regex with `~` prefix, method filtering | ✅ Done |
| `proxy`   | `ServeHTTP` handler: match route, buffer body, forward via `httputil.ReverseProxy`, capture status, fire webhooks async | ✅ Done |
| `webhook` | `Action` interface, `HTTPAction` (retry + backoff), payload builder (metadata/body/custom), async dispatcher, template rendering | ✅ Done |
| `watcher` | `fsnotify` file watcher + `SIGUSR1` signal handler, atomic config swap via `atomic.Pointer` | ✅ Done |
| `logging` | `slog` JSON handler, configurable log level via `DEBUG` env var | ✅ Done |

### File Tree (actual)

```
HandOff/
├── main.go                    # Entry point, flag parsing, server lifecycle
├── go.mod / go.sum
├── config.yaml.example
├── PLAN.md / README.md / WIKI.md
├── config/
│   ├── config.go              # Type definitions + Validate()
│   ├── loader.go              # Load config/secrets from YAML files
│   └── config_test.go         # 9 tests
├── matcher/
│   ├── matcher.go             # Path + method matching
│   └── matcher_test.go        # 7 tests (glob, regex, method, first-match)
├── proxy/
│   ├── proxy.go               # HTTP handler, body buffering, proxy cache
│   └── proxy_test.go          # 3 integration tests
├── webhook/
│   ├── action.go              # Action interface
│   ├── payload.go             # PayloadContext, BuildPayload, RenderTemplate
│   ├── http.go                # HTTPAction with retry + exponential/linear backoff
│   ├── dispatcher.go          # Async fan-out dispatcher
│   ├── payload_test.go        # 5 tests
│   └── dispatcher_test.go     # 2 tests
├── watcher/
│   └── watcher.go             # fsnotify + signal handler
└── logging/
    └── logging.go             # slog JSON logger
```

### Execution Flow (as implemented)

1. `main.go` loads config from YAML → validates → stores in `atomic.Pointer`
2. Loads secrets from secrets file (if provided)
3. Starts `fsnotify` watcher + `SIGUSR1` signal handler
4. Creates `ProxyHandler` with config pointer + secrets
5. Starts `net/http.Server` (TLS or plain HTTP based on config)
6. On each request:
   - `matcher.Match(path, method)` → finds matching `*Route` (returns `nil` if none → 502)
   - Request body is fully buffered via `io.ReadAll`
   - `httputil.ReverseProxy` forwards to the route's backend
   - Custom `responseRecorder` captures status code from backend response
   - PayloadContext is built with request metadata + response metadata
   - If route has webhooks: goroutine fires `dispatcher.Fire()` (which spawns one goroutine per webhook)
   - Each webhook runs independently: builds payload, renders templates, sends HTTP request with retry+backoff
7. Client receives the backend response, untouched

---

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `gopkg.in/yaml.v3` | v3.0.1 | YAML config parsing |
| `github.com/fsnotify/fsnotify` | v1.10.0 | Config file watching for hot reload |
| `github.com/Masterminds/sprig/v3` | v3.3.0 | Template function library |
| `github.com/google/uuid` | v1.6.0 | (transitive, not directly used) |

---

## v1 Scope

| Feature | Status |
|---------|--------|
| HTTP + HTTPS listen | ✅ |
| Multi-backend routing | ✅ |
| Path matching (glob + regex) | ✅ |
| Method matching | ✅ |
| HTTP webhooks (fire-and-forget) | ✅ |
| Retry + exponential/linear backoff | ✅ |
| Payload modes (metadata, body, custom template) | ✅ |
| Template engine with Sprig funcs | ✅ |
| Config hot reload (fsnotify + SIGUSR1) | ✅ |
| Structured JSON logging (slog) | ✅ |
| Secrets file support | ✅ |
| Body buffering (io.ReadAll) | ✅ |
| Proxy connection pooling per backend | ✅ |
| Request/response header modification | 🚫 (future) |
| Response header capture (caching header, etc.) | 🚫 (future) |
| Non-HTTP webhook types (exec, SQS, Kafka) | 🚫 (future) |
| Webhook payload compression | 🚫 (future) |
| Tracing / OpenTelemetry | 🚫 (future) |
| Admin API / metrics endpoint | 🚫 (not planned) |
| GUI | 🚫 (not planned) |

---

## Known Limitation Summary

See [WIKI.md](WIKI.md) for detailed quirks, edge cases, and design decisions.

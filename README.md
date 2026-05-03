# HandOff

A fire-and-forget webhook proxy. Place it between your clients and backend services, define route patterns, and HandOff triggers webhooks asynchronously on matching requests — without ever blocking or modifying the response.

## Quick Start

### Prerequisites

- Go 1.23+
- A backend service to proxy to
- (Optional) A webhook receiver (e.g. [webhook.site](https://webhook.site), Slack, Discord)

### 1. Build

```bash
git clone <your-repo-url> handoff
cd handoff
go build -o handoff .
```

### 2. Create config

```bash
cp config.yaml.example config.yaml
```

Edit `config.yaml` to set your backend and webhook URL. Minimal example:

```yaml
listen:
  host: "127.0.0.1"
  port: 8080

global:
  timeout: 30s

routes:
  - path: "/api/**"
    methods: []
    backend: "https://httpbin.org"
    webhooks:
      - type: http
        url: "https://webhook.site/your-uuid-here"
        payload: metadata
        retry:
          attempts: 3
```

### 3. (Optional) Create secrets

```bash
echo 'slack_key: xoxb-your-token' > secrets.yaml
```

### 4. Run

```bash
./handoff -c config.yaml -secrets secrets.yaml
```

### 5. Test

```bash
curl -X POST http://127.0.0.1:8080/api/test -d '{"hello":"world"}'
```

The request is forwarded to the backend. The webhook fires async with metadata — check your webhook.site dashboard.

### Hot Reload

Edit `config.yaml` and send `SIGUSR1` to reload without restarting:

```bash
kill -SIGUSR1 $(pgrep handoff)
```

The watcher also picks up file saves automatically via `fsnotify`.

### Development

```bash
go test ./...         # Run all tests
go test -race ./...   # With race detector
go build -o handoff . # Build binary
go vet ./...          # Static analysis
```

---

## CLI

| Flag | Default | Description |
|------|---------|-------------|
| `-c` | `config.yaml` | Path to configuration file |
| `-secrets` | (none) | Path to secrets file (YAML key-value map) |

| Signal | Effect |
|--------|--------|
| `SIGUSR1` | Reload config from disk |
| `SIGINT`, `SIGTERM` | Graceful shutdown (drains connections) |

| Env Var | Effect |
|----------|--------|
| `DEBUG` | Set to any value to enable debug-level logging |

---

## Configuration

### Full Structure

```yaml
listen:
  host: "0.0.0.0"        # default: 0.0.0.0
  port: 8080              # required, 1–65535
  tls:
    enabled: false
    cert_file: "/etc/handoff/cert.pem"   # required if enabled
    key_file: "/etc/handoff/cert.key"    # required if enabled

global:
  timeout: 30s            # backend connection timeout (default: 30s)

routes:
  - path: "/api/**"       # glob or ~regex — required
    methods: []            # empty = all methods
    backend: "https://..." # required
    webhooks:
      - type: http         # default: http
        url: "https://..." # required, supports templates
        method: POST       # default: POST, supports templates
        headers:           # keys AND values support templates
          Content-Type: application/json
          Authorization: "Bearer {{.Secrets.token}}"
        payload: metadata  # metadata | body | custom
        template: ...      # required if payload=custom
        retry:
          attempts: 3      # default: 1
          backoff: exponential  # exponential | linear
```

### Path Patterns

| Pattern | Type | Matches |
|---------|------|---------|
| `/api/**` | Glob | Any path under `/api/`, including `/api` and `/api/` |
| `/users/*` | Glob | Exactly one segment under `/users/`, e.g. `/users/123` (not `/users/`) |
| `~/users/\d+$` | Regex | `/users/123` but not `/users/abc` |

If `methods` is empty or omitted, the route matches **all** HTTP methods.

### Payload Modes

| Mode | Webhook receives | Content-Type |
|------|-----------------|--------------|
| `metadata` | JSON: `{"request":{...},"response":{...},"timestamp":"..."}` | `application/json` |
| `body` | Raw request body bytes | Inherited from request, or `application/octet-stream` |
| `custom` | Rendered Go template (Sprig functions available) | `application/json` |

### Template Reference

```go
{{.Request.Method}}          // string: "GET", "POST", ...
{{.Request.Path}}            // string: "/api/users"
{{.Request.ClientIP}}        // string: "1.2.3.4"
{{.Request.ID}}              // string: 16-char hex request ID
{{.Request.Query}}           // string: "page=1&limit=10"
{{.Request.ContentType}}     // string: "application/json"
{{.Request.Headers}}         // map[string][]string
{{.Response.StatusCode}}     // int: 200
{{.Response.LatencyMs}}      // int64: 42
{{.Timestamp}}               // time.Time (use .Format or Sprig date functions)
{{.Secrets.my_key}}          // string from secrets file
```

Available Sprig functions: `{{.Timestamp \| date "2006-01-02"}}`, `{{.Request.Path \| upper}}`, `{{.Response.LatencyMs \| add 10}}`, etc. See [Sprig docs](https://masterminds.github.io/sprig/).

### Secrets

Secrets are loaded from a YAML file:

```yaml
# secrets.yaml
slack_key: xoxb-...
api_token: sk-...
```

Referenced in templates via `{{.Secrets.key_name}}`. Secrets are loaded once at startup — restart or send `SIGUSR1` to reload (note: secrets reload is not yet implemented; restart to pick up new secrets).

### Multiple Backends

```yaml
routes:
  - path: "/v1/**"
    backend: "https://legacy-api.example.com"
    webhooks: [...]

  - path: "/v2/**"
    backend: "https://api.example.com"
    webhooks: [...]
```

### TLS

```yaml
listen:
  port: 8443
  tls:
    enabled: true
    cert_file: "/etc/handoff/cert.pem"
    key_file: "/etc/handoff/cert.key"
```

---

## How It Works

```
Client ──► HandOff ──► Backend ──► HandOff ──► Client
                │
                ▼ (async goroutine)
           Webhook 1, Webhook 2, ...
```

1. Request arrives at HandOff.
2. Path and method are matched (first-match-wins) against configured routes. No match → **502 Bad Gateway**.
3. Request body is buffered into memory.
4. Request is forwarded using `httputil.ReverseProxy` to the matching backend.
5. Backend response status code is captured (but the response body streams directly to the client).
6. In a separate goroutine (non-blocking), each webhook: builds its payload (metadata, body, or custom template), renders template strings, sends HTTP request.
7. Webhook failures are retried (up to N attempts with configurable backoff — exponential by default, linear as an option).
8. The client receives the backend response — **entirely untouched and never delayed by webhooks**.

---

## Further Reading

- [PLAN.md](PLAN.md) — architecture and design decisions
- [WIKI.md](WIKI.md) — quirks, edge cases, and known limitations
- [config.yaml.example](config.yaml.example) — annotated example config

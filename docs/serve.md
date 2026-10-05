# Serving Sandboxed Network Services

Milestone 5 introduces the `Serve` API, allowing you to run network servers and HTTP services inside the sandbox and make them reachable from the host machine **without ever granting the untrusted code access to the host network**.

## Key Security Principles

### 1. The Host Listener is Owned by the Proxy
Untrusted guest code never opens public host ports directly:
- **Our host proxy** binds to the host port.
- All inbound traffic flows: `Client -> Host Proxy (limits & auth applied) -> Guest Service`.
- The guest cannot receive any traffic that does not pass the host proxy's security controls.
- The guest network mode remains isolated so the guest cannot scan or connect to the host or local network.

### 2. Loopback by Default
By default, all proxies bind strictly to `127.0.0.1` (`sandbox.Loopback`):
```go
svc, err := sb.Serve(ctx, spec, sandbox.ServeOpts{
    Ports: []sandbox.PortMap{{Guest: 8000}}, // Host port auto-assigned on 127.0.0.1
    Bind:  sandbox.Loopback,                 // Default
})
```
Exposing a service to other network interfaces (e.g. `0.0.0.0`) requires explicit configuration (`sandbox.Interface("0.0.0.0")`) and triggers a warning log.

### 3. Proxy-Level Bearer Token Authentication
To prevent other local processes on the host machine from calling the sandboxed service, you can enable token authentication:
```go
svc, err := sb.Serve(ctx, spec, sandbox.ServeOpts{
    Auth: sandbox.BearerToken(""), // Empty string generates an unguessable token
})
fmt.Printf("Access token: %s\n", svc.Token())
```
Unauthenticated requests receive `401 Unauthorized` directly from the proxy and **never reach the guest**.

### 4. Enforced Resource & Threat Limits
The proxy enforces:
- **Slowloris protection**: Header read timeout (`ReadHeaderTimeout`, default 5s) terminates clients that trickle headers.
- **Payload limits**: `MaxRequestBody` (default 10 MB). Exceeding requests are rejected with `413 Payload Too Large`.
- **Header size limits**: `MaxHeaderBytes` (default 1 MB).
- **Concurrency capping**: `MaxConnections` (default 256) bounds concurrent client sockets to avoid host resource exhaustion.
- **WebSocket pass-through**: Full duplex streaming and HTTP upgrade protocol pass-through.

---

## Backend Mechanisms

| Backend | Mechanism | Servers Supported |
|---|---|---|
| **Docker** | Publishes guest port to ephemeral loopback (`127.0.0.1:0`). Discovers port via inspect. Host proxy forwards inbound connections. | Any server in any language (Express, Flask, Spring, Actix, Gin, etc.) |
| **Wasm** | **In-process Handler Bridge**: Host proxy turns each HTTP request into an in-process call into the Wasm session; the response is serialized back. | Cloudflare Worker / WinterCG-style `fetch(req)` handlers (JS) and WSGI apps (Python). Zero daemon, pure Go. |

---

## Service Lifecycle

```
sb.Serve() ─► start guest ─► probe readiness ─► open host proxy ─► svc.URL()
     ▲                                                                   │
     └─────────────────── TTL / svc.Stop() / guest crash ◄───────────────┘
```

1. **Readiness Probing**:
   Before the host proxy accepts any traffic, `Serve` probes the guest service using `sandbox.TCPReady` or `sandbox.HTTPReady`. If the guest fails to become ready within the timeout, `Serve` aborts and returns the guest's recent stdout/stderr logs for immediate troubleshooting.
2. **Streaming Log Ring Buffer**:
   Guest logs are continuously recorded in a bounded circular buffer (default 256 KB) accessible via `svc.Logs()` or `svc.RecentLogs()`, preventing unbounded memory growth.
3. **Graceful Teardown & TTL**:
   Calling `svc.Stop()`, context cancellation, or TTL expiration closes the host listener, shuts down the guest container/runtime, and notifies callers via `<-svc.Done()`.

---

## Example Usage

### JavaScript on Wasm (Handler Bridge)
```go
jsCode := `
function fetch(req) {
    return {
        status: 200,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: "Hello from Wasm!", path: req.path })
    };
}
`

svc, err := sb.Serve(ctx, sandbox.Spec{
    Lang: "js",
    Code: jsCode,
}, sandbox.ServeOpts{
    Ports: []sandbox.PortMap{{Guest: 8080}},
    Auth:  sandbox.BearerToken("my-secret-key"),
    TTL:   10 * time.Minute,
})
if err != nil {
    log.Fatal(err)
}
defer svc.Stop()

fmt.Println("Service running at:", svc.URL())
```

### Node.js or Python on Docker
```go
nodeCode := `
const http = require('http');
http.createServer((req, res) => {
    res.end("Hello from Node.js server inside Docker!");
}).listen(3000, '0.0.0.0');
`

svc, err := sb.Serve(ctx, sandbox.Spec{
    Lang: "node",
    Code: nodeCode,
}, sandbox.ServeOpts{
    Ports: []sandbox.PortMap{{Guest: 3000}},
    Ready: sandbox.TCPReady(15 * time.Second),
})
```

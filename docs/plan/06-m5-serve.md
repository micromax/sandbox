# 06 — M5: Serve (expose ports on the host)

## Goal

Run a server inside the sandbox and make it reachable from the host machine, **without ever giving the code access to the host network**.

```go
svc, err := sb.Serve(ctx, spec, sandbox.ServeOpts{
    Ports:    []sandbox.PortMap{{Guest: 8000}},   // host port auto-assigned
    Bind:     sandbox.Loopback,                   // default
    TTL:      10 * time.Minute,
    Auth:     sandbox.BearerToken(""),            // optional; "" = generate
    Ready:    sandbox.TCPReady(15 * time.Second),
})
svc.URL()      // http://127.0.0.1:49213
svc.Addr(8000) // 127.0.0.1:49213
svc.Logs()     // io.Reader streaming guest stdout/stderr
svc.Stop()
```

## Design

### Principle: the library owns the host listener

The host port is opened by **our proxy**, not by the guest. All inbound traffic goes proxy → guest. The guest can only receive traffic that passes the proxy's rules.

### Per backend

| Backend | Mechanism | Servers supported |
|---|---|---|
| Docker | Publish guest port to `127.0.0.1:<ephemeral>` (works on Docker Desktop where container IPs are unreachable); our proxy listens on the user-visible address and forwards | Any server in any language |
| Wasm (tier 1) | **Handler bridge**: host listener turns each HTTP request into a call into the guest; response comes back as a function result | Handler-style apps: JS `fetch(req)` handlers, Python WSGI/ASGI-style apps. Not apps that open their own sockets. |
| Linux-in-Wasm (M6, if it works) | Port forwarding through the emulated NIC to a host listener | Any server |

Unsupported combinations return `ErrUnsupported` with a message pointing to the backend that does support it.

### Proxy (`proxy/`)

- TCP proxy with an HTTP-aware mode (for request limits, logging, auth).
- Enforced limits: max connections, max request body size, header size, per-IP rate limit, idle and read timeouts, max bytes transferred.
- WebSocket supported through HTTP upgrade pass-through.
- Optional `Authorization: Bearer <token>` check at the proxy, so other local processes cannot talk to the service.
- Binds to `127.0.0.1` by default. `0.0.0.0` or a specific interface requires `Bind: sandbox.Interface("...")` and logs a warning.

### Service lifecycle

```
Create sandbox ─► start guest ─► wait for readiness ─► open host listener
        ▲                                                   │
        └────────── TTL / Stop / guest exit ◄───────────────┘
```

- **Readiness**: probe the guest port until it accepts a connection or the timeout elapses. On failure, return an error containing the guest's recent logs.
- **Host listener opens only after readiness**, so clients never see half-started services.
- **Teardown**: TTL expiry, `Stop`, guest exit, or context cancel all close the listener, kill the sandbox, and release the port.
- Logs ring buffer (bounded) so log volume cannot exhaust memory.
- Guest crash triggers `svc.Done()` channel closing with an error and last logs.

### Wasm handler bridge details

- Pack defines an adapter that wraps user code:
  - JS: user code `export default { fetch(req) {...} }` — adapter serializes request (method, URL, headers, body) in and response out.
  - Python: WSGI callable `app(environ, start_response)`.
- Each request is a call into a long-lived session instance (reuses M3 machinery).
- Per-request timeout; a timeout kills the instance, and the service reports unhealthy (or restarts if enabled).

## Execution steps

1. Implement `proxy/`: TCP listener, connection tracking, limits, graceful close; then HTTP mode with body and header limits, auth, WebSocket pass-through.
2. Implement `Service` type: lifecycle state machine, logs ring buffer, `Done()`, TTL timer.
3. Implement readiness probes (TCP, optional HTTP path).
4. Docker backend: publish port to loopback, discover the assigned port via inspect, connect proxy to it, handle container exit.
5. Implement `Serve` routing in the root package, including `ErrUnsupported` messages.
6. Wasm handler bridge: JS adapter, then Python WSGI adapter, built on M3 sessions.
7. Example apps: Python Flask on Docker, Node Express on Docker, JS fetch handler on Wasm, Python WSGI on Wasm, Java HTTP server on Docker.
8. CLI `sandbox serve` (see M7).
9. Document the exposure threat model clearly.

## Deliverables

`proxy/`, `service.go`, Docker and Wasm `Serve` implementations, `examples/serve-*`, `docs/serve.md`.

## Tests

| Test | Expectation |
|---|---|
| Flask or http.server in Docker | `GET` through `svc.URL()` returns the expected body |
| JS fetch handler on Wasm | Same |
| Connect from another interface (LAN IP) with default bind | Refused |
| Request without the token when auth is on | 401, never reaches the guest |
| 10 MB body with a 1 MB limit | 413, guest unaffected |
| 1,000 rapid connections | Capped, no host resource exhaustion |
| Slowloris-style slow headers | Closed by timeouts |
| TTL expiry | Listener closed, port released, container or instance gone |
| Guest process exits | `Done()` fires, port released |
| Server inside tries outbound connection | Blocked (network off) |
| Parallel services | Distinct ports, no cross-talk |
| WebSocket echo | Works through the proxy |

## Exit criteria

- [ ] Server examples work on Docker for at least Python, Node and Java
- [ ] Wasm handler bridge works for JS and Python
- [ ] Binding defaults to loopback; non-loopback is explicit
- [ ] All proxy limit tests pass
- [ ] No leaked ports, containers or goroutines after teardown

## Risks

| Risk | Fallback |
|---|---|
| Docker Desktop port-publish behavior differs per OS | Always publish to `127.0.0.1:0` and read back the assigned port |
| Handler bridge doesn't cover all frameworks | Document clearly; direct users to the Docker backend for raw-socket servers |
| Proxy becomes an attack surface | Keep it small, use `net/http` primitives, fuzz the HTTP layer |

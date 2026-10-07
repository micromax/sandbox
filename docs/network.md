# Outbound Network Policy & Security Guide

Milestone 7 introduces optional, policy-controlled outbound networking for `github.com/micromax/sandbox`. By default, all sandboxes run in strict air-gap isolation (`Spec.Net == nil`), denying all outbound traffic. When network access is required (e.g. calling an external API or fetching a dataset), `sandbox.NetPolicy` allows fine-grained, defense-in-depth control.

---

## 1. Network Policy Configuration

Outbound network access is enabled by attaching a `*sandbox.NetPolicy` to `sandbox.Spec`:

```go
spec := sandbox.Spec{
    Lang: "python",
    Code: script,
    Net: &sandbox.NetPolicy{
        AllowHosts:   []string{"api.github.com", "*.pypi.org"},
        AllowPorts:   []int{443},
        MaxRequests:  50,
        MaxBytes:     10 << 20, // 10 MiB payload cap
        AllowPrivate: false,    // Block RFC1918, loopback, link-local, cloud metadata
    },
}
```

### Policy Options

| Option | Type | Default | Description |
|---|---|---|---|
| `AllowHosts` | `[]string` | `nil` | Exact hostnames (`api.example.com`) or single-label wildcards (`*.example.com`). Root domain (`example.com`) is also matched. |
| `AllowPorts` | `[]int` | `[]int{80, 443}` | Permitted destination TCP ports. Traffic to unlisted ports is blocked immediately. |
| `MaxRequests`| `int` | `100` | Hard cap on the number of outbound HTTP requests per execution. Exceeding triggers `ErrMaxRequestsExceeded`. |
| `MaxBytes` | `int64` | `10 MiB` | Total bandwidth budget (request + response bytes). Exceeding terminates the connection with `ErrMaxBytesExceeded`. |
| `AllowPrivate`| `bool` | `false` | When `false`, strictly denies private IPs, loopback, link-local, and cloud metadata. |

---

## 2. SSRF Protection & DNS Rebinding Defenses

Untrusted code may attempt Server-Side Request Forgery (SSRF) to attack local microservices, access the Docker socket, or steal cloud credentials. `netpolicy` provides robust protection:

### 1. Resolve-Then-Pin Dialer (Anti-DNS Rebinding)
Standard HTTP clients resolve hostnames on every connection or follow-up redirect. An attacker could configure a DNS name with a 0-second TTL that initially resolves to an allowed public IP, but resolves to `169.254.169.254` on the second request.

`netpolicy` mitigates this with a custom dialer:
1. Resolves the hostname once on the host.
2. Validates the resolved IP against blocked IP ranges.
3. Pins the connection directly to the validated IP address.
4. On HTTP redirects (301/302/307/308), re-validates the target host and re-resolves the IP before following.

### 2. Blocked Address Ranges (When `AllowPrivate == false`)
* **Loopback**: `127.0.0.0/8`, `::1`
* **Private Networks (RFC 1918)**: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `fc00::/7`
* **Link-Local & Cloud Metadata**: `169.254.0.0/16` (including AWS/GCP/Azure `169.254.169.254`), `fe80::/10`
* **Broadcast & Multicast**: `224.0.0.0/4`, `255.255.255.255`

---

## 3. Implementation per Backend

### WebAssembly Backend (`backend/wasm`)
* **No Raw Sockets**: WASI Preview 1 does not expose BSD sockets. The guest runtime cannot open raw TCP/UDP streams.
* **Host Function HTTP Bridge**: Outbound HTTP requests are mediated through an in-process host function (`http_request`).
* The host Go process executes the HTTP request through the `netpolicy` dialer and streams bounded response bytes back into the Wasm memory space.
* **Guest Adapters**: Packs supply standard runtime shims (e.g. global `fetch()` in JS, patched `urllib` / `requests` in Python).

### Docker Backend (`backend/docker`)
* **Isolated Bridge Network**: The container is placed on an isolated bridge network with no default gateway or direct internet routing.
* **Filtering Forward Proxy**: Container egress is routed through an in-process HTTP CONNECT proxy managed by the host. Direct TCP connections to arbitrary external IPs are dropped by Docker network isolation.

---

## 4. Audit Logging (`Result.NetLog`)

Every outbound network attempt is captured in `sandbox.Result.NetLog`:

```go
for _, entry := range res.NetLog {
    fmt.Printf("[%s] %s %s -> %d (%d bytes, %v)\n",
        entry.Timestamp.Format(time.RFC3339),
        entry.Method,
        entry.URL,
        entry.StatusCode,
        entry.BytesTransferred,
        entry.Duration,
    )
}
```

If a request is blocked by policy or SSRF filters, `entry.Error` records the exact violation reason.

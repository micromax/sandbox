# 08 — M7: Network option, CLI, release

## Goal

Ship `v0.1.0`: optional outbound network with an allow-list, a CLI for humans and CI, complete documentation.

## Part A — Network option

### Design

```go
sandbox.Spec{ Net: &sandbox.NetPolicy{
    AllowHosts: []string{"api.example.com", "*.pypi.org"},
    AllowPorts: []int{443},
    MaxRequests: 100,
    MaxBytes:    10 << 20,
    AllowPrivate: false, // default: block RFC1918, loopback, link-local, metadata
}}
```

- **Default is no network.** `Net == nil` means none.
- **Wasm backend:** WASI preview1 has no sockets, so network is provided as a **host-function HTTP bridge**. The host performs requests on behalf of the guest after policy checks. Packs ship small guest shims: JS global `fetch`, Python `urllib`/`requests` patch (documented limits: HTTP(S) only, no raw sockets).
- **Docker backend:** a user-defined network with egress forced through a **filtering proxy** run by the library (HTTP CONNECT allow-list). Direct internet access is never given.
- **SSRF guard (`netpolicy/`):** resolve DNS **once** in the host, validate the resolved IP against the block-list, then connect to that IP (prevents DNS rebinding). Re-validate on every redirect. Block `169.254.169.254`, `127.0.0.0/8`, `10/8`, `172.16/12`, `192.168/16`, `::1`, `fc00::/7`, `fe80::/10`.
- Every request is logged into `Result.NetLog`.

### Execution steps

1. Implement `netpolicy`: host matching (exact and wildcard), IP classification, resolve-then-pin dialer, redirect handling.
2. Wasm host function `http_request` with size, time and count limits.
3. JS `fetch` shim and Python `urllib` shim in the packs.
4. Docker egress proxy and isolated network setup.
5. `NetLog` in the result.

### Tests

| Test | Expectation |
|---|---|
| Fetch an allowed host | Success |
| Fetch a non-listed host | `ErrNetworkDenied` |
| Fetch `http://127.0.0.1`, `169.254.169.254`, `10.x` | Denied |
| Allowed host that redirects to an internal IP | Denied |
| DNS name that resolves to an internal IP | Denied |
| DNS rebinding (changes between check and use) | Safe: the dialer connects to the pinned IP |
| Response larger than `MaxBytes` | Truncated and errored |
| Docker container tries a direct connection bypassing the proxy | Fails |

## Part B — CLI (`cmd/sandbox`)

```
sandbox run   --lang python [--timeout 5s] [--mem 128M] [--net host1,host2] file.py
sandbox repl  --lang python
sandbox serve --lang python --port 8000 --ttl 10m file.py
sandbox packs list | install <name> | verify
sandbox doctor            # checks wasm cache, Docker availability, versions
sandbox version
```

- Built with the standard library `flag` (or `cobra` if it earns its weight).
- `doctor` prints actionable guidance, e.g. when Docker is missing.
- JSON output mode (`--json`) for scripting.

### Execution steps

1. Implement commands over the public API only (the CLI is a consumer, ensuring the API is sufficient).
2. `packs install` pre-downloads artifacts and images for offline use.
3. Cross-compile and verify for windows, linux and darwin on amd64 and arm64.

## Part C — Docs and release

### Documentation

- `README.md`: what it is, security model in plain words, quick start, supported languages matrix.
- `docs/security.md`: threat model, what is and isn't protected, backend comparison.
- `docs/languages.md`: per-pack capabilities and limits.
- `docs/serve.md`, `docs/network.md`, `docs/docker-security.md`.
- GoDoc on every exported symbol; runnable `Example` functions.

### Release steps

1. Final API review with you; freeze for `v0.1.x`.
2. Choose and add the license.
3. Add `CHANGELOG.md` and `CONTRIBUTING.md` and `SECURITY.md` (vulnerability reporting).
4. Tag `v0.1.0`; for multi-module packs, tag each submodule (see doc 10).
5. CLI binaries via GoReleaser, with checksums.
6. Verify that `go get github.com/micromax/sandbox@v0.1.0` works from a clean project.

## Exit criteria

- [ ] Network allow-list and SSRF tests pass on all backends that support network
- [ ] CLI works on three OSes; `doctor` gives clear diagnostics
- [ ] Docs complete; every example in the README is executed in CI
- [ ] Clean-project `go get` and example run succeed
- [ ] You approve the release

## Risks

| Risk | Fallback |
|---|---|
| Python network shim is incomplete | Ship `urllib` first; document `requests`/`httpx` support as best-effort |
| Docker egress proxy is complex | Ship Docker with no-network only in v0.1.0; add proxy in v0.2 |
| Scope creep | Network on Docker is the first thing cut |

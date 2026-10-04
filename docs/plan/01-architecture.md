# 01 — Architecture

## Goal

Define the shape of the library so every later milestone slots in without changing the public API.

## Threat model

| Asset | Threat | Mitigation |
|---|---|---|
| Host files, env vars, secrets | Code reads or writes them | Guest sees only a virtual FS containing what we inject |
| Host CPU / RAM | Infinite loops, fork bombs, memory bombs | Memory page cap, wall-time deadline, instruction interruption, output cap |
| Host disk | Filling disk | VFS byte and file-count quota |
| Host network / LAN | Scanning, SSRF, data exfiltration | No network by default; allow-list + private-IP block when enabled |
| Other sandboxes | Cross-tenant leakage | One instance per run/session; nothing shared but read-only compiled code |
| The library itself | Tampered runtime artifacts | SHA-256 pinned artifacts, verified before use |
| Output channel | Giant output, terminal escape abuse | Output size caps, raw bytes returned unmodified |

**Out of scope:** side-channel attacks (timing, speculative execution) and bugs in the underlying runtimes (wazero, Docker). We document these.

## Core concepts

```
Pack      — describes a language and how to run it on each backend
Backend   — an isolation mechanism (wasm, docker, linux-in-wasm)
Spec      — what to run (code, files, args, env) and under which limits
Sandbox   — the entry point; holds registered packs and a routing policy
Session   — a long-lived stateful instance (REPL)
Service   — a long-lived instance that exposes ports (Serve)
Result    — stdout, stderr, exit code, output files, resource usage
```

## Package layout

```
github.com/micromax/sandbox
├── sandbox.go          Sandbox, New, options, Run
├── session.go          Session, Eval
├── service.go          Service, Serve
├── manager.go          concurrent sessions, caps, idle reaping
├── limits.go           Limits, defaults
├── result.go           Result, Usage, errors
├── pack.go             Pack, registry, Router policy
├── backend/
│   ├── backend.go      Backend interface
│   ├── wasm/           wazero backend
│   ├── docker/         Docker/Podman API backend
│   └── l2w/            Linux-in-Wasm (experimental, M6)
├── vfs/                in-memory FS with quotas
├── netpolicy/          allow-list, SSRF guard
├── proxy/              host-side TCP/HTTP proxy for Serve
├── artifact/           download, cache, SHA-256 verification
├── packs/              one sub-module per language (see doc 10)
│   ├── js/
│   ├── python/
│   └── ...
├── cmd/sandbox/        CLI
└── internal/           shared helpers (not public API)
```

## Public API sketch

```go
sb, err := sandbox.New(
    sandbox.WithBackends(wasm.New()),
    sandbox.WithPacks(js.Pack(), python.Pack()),
    sandbox.WithPolicy(sandbox.PreferWasm),
    sandbox.WithDefaultLimits(sandbox.Limits{...}),
)

// One-shot
res, err := sb.Run(ctx, sandbox.Spec{Lang: "python", Code: src})

// REPL
s, err := sb.NewSession(ctx, "python")
r, err := s.Eval(ctx, "x = 21")
r, err = s.Eval(ctx, "print(x*2)")
s.Close()

// Serve
svc, err := sb.Serve(ctx, sandbox.Spec{Lang: "python", Code: src},
    sandbox.ServeOpts{GuestPort: 8000, TTL: 10 * time.Minute})
fmt.Println(svc.URL())
svc.Stop()
```

## Backend interface (as implemented in M1)

The contract lives in the root package so backends import the core, never the reverse (no import cycles, and the core stays free of heavy dependencies). Sessions and serving are **optional interfaces** added in M3 and M5, so a backend only implements what it supports.

```go
type Backend interface {
    Name() string
    Supports(p *Pack) bool
    Run(ctx context.Context, req *Request) (Outcome, error)
}

// Request carries resolved Limits, the guest FS, and capped Stdout/Stderr writers.
// Outcome carries ExitCode and PeakMemory.

type NetworkCapable interface{ SupportsNetwork() bool } // required before a NetPolicy is accepted
// M3: type SessionBackend interface { NewSession(...) }
// M5: type ServeBackend   interface { Serve(...) }
```

If a backend does not implement an optional interface, the core returns `ErrUnsupported` for that operation.

## Routing policy

| Policy | Behavior |
|---|---|
| `PreferWasm` (default) | Use Wasm if the pack supports it, else Docker if enabled, else error |
| `WasmOnly` | Never use anything else |
| `DockerOnly` | Never use anything else |

**Rule: never silently downgrade isolation.** If the required backend is unavailable, return `ErrBackendUnavailable`.

## Error model

Sentinel errors, usable with `errors.Is`: `ErrTimeout`, `ErrMemoryLimit`, `ErrOutputLimit`, `ErrFSQuota`, `ErrSessionKilled`, `ErrBackendUnavailable`, `ErrUnsupported`, `ErrNetworkDenied`.

A non-zero exit code from guest code is **not** a Go error — it is reported in `Result.ExitCode`.

## Cross-cutting rules

- `context.Context` is the first parameter of every blocking call.
- Zero values of option structs are safe defaults.
- No global state; everything hangs off `*Sandbox`.
- Minimal dependencies: wazero, plus small helpers only when justified.
- Go version: latest stable at project start; set in `go.mod`.
- Windows, Linux and macOS are tested in CI from M1 onward.

## How I will execute this document

1. Create the repo skeleton and `go.mod` (`module github.com/micromax/sandbox`).
2. Write the public types and interfaces with doc comments but no implementation.
3. Confirm with you before M1 starts if any API shape should change.

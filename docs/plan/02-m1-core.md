# 02 — M1: Core

## Goal

A compiling library with the public API, limits, virtual filesystem, pack registry and routing, plus a **fake in-process backend** used only for tests. No real isolation yet — that arrives in M2.

## Design

### Limits

```go
type Limits struct {
    Memory      uint64        // bytes, default 128 MiB
    WallTime    time.Duration // default 10s
    CPUTime     time.Duration // best effort, backend dependent
    MaxOutput   uint64        // stdout+stderr bytes, default 1 MiB
    FSQuota     uint64        // bytes in virtual FS, default 16 MiB
    MaxFiles    int           // default 256
    MaxProcs    int           // used by Docker backend
}
```

Every field has a safe default. Zero means "use the default", never "unlimited". Unlimited needs an explicit sentinel `sandbox.Unlimited`.

### Spec

```go
type Spec struct {
    Lang   string
    Code   string
    Args   []string
    Env    map[string]string // only these vars are visible to the guest
    Stdin  io.Reader
    Files  map[string][]byte // injected into the virtual FS
    Limits *Limits           // overrides defaults
    Net    *NetPolicy        // nil = no network
}
```

### Result

```go
type Result struct {
    Stdout, Stderr []byte
    ExitCode       int
    Files          map[string][]byte // files the guest left in the output dir
    Usage          Usage             // wall time, peak memory, fs bytes
    Backend        string
}
```

### VFS (`vfs/`)

- In-memory tree implementing `fs.FS` for reads and a writer API for the guest.
- Enforces `FSQuota` and `MaxFiles` on every write; exceeding returns `ErrFSQuota`.
- Path hardening: reject `..`, absolute escape, symlinks to outside, and reserved names. Paths are normalized to forward slashes on all OSes.
- Export: `Snapshot()` returns files from `/out` (and optionally others) into `Result.Files`.
- Workspace layout the guest sees: `/work` (read-write, scratch), `/in` (read-only injected files), `/out` (read-write, collected).

### Output capture

A `limitedBuffer` writer that stops accepting bytes at `MaxOutput` and flags truncation, so a guest can never exhaust host memory through stdout.

### Pack registry and router

- `Pack` struct as in doc 01, with per-backend specs.
- `Register` rejects duplicate names; `Lookup` is case-insensitive.
- Router selects a backend per the policy and returns `ErrBackendUnavailable` with a clear message otherwise.

## Execution steps

1. `go mod init github.com/micromax/sandbox`; add `LICENSE` (ask you which) and `.gitignore`.
2. Implement `limits.go` with defaults and validation, with table tests.
3. Implement `result.go` and the error sentinels.
4. Implement `vfs/`: node tree, quota accounting, path normalization, `fs.FS` adapter, snapshot. Fuzz-test the path normalizer.
5. Implement `limitedBuffer` and tests for truncation.
6. Implement `pack.go` registry and the routing policies.
7. Implement `sandbox.go`: `New`, options, `Run` that validates the spec, applies defaults, routes, and calls the backend.
8. Implement `backend/fake` (in-process echo/arith backend) for tests only, behind a build tag or `internal`.
9. Add GitHub Actions: `go vet`, `staticcheck`, `go test -race` on ubuntu, windows and macos.
10. Write package docs and a minimal `examples/hello`.

## Deliverables

`go.mod`, `sandbox.go`, `limits.go`, `result.go`, `pack.go`, `vfs/`, `backend/backend.go`, `backend/fake`, CI workflow, README stub.

## Tests

- Limits: defaults applied, zero never means unlimited, invalid values rejected.
- VFS: quota exceeded, file-count exceeded, traversal attempts (`../`, `..\\`, absolute, UNC paths, null bytes), case-collision behavior, snapshot correctness.
- Fuzz: `vfs.Clean` never returns a path that escapes the root.
- Output cap: writing 10x `MaxOutput` stores exactly `MaxOutput` and sets `Truncated`.
- Router: each policy yields the expected backend or error, and **never downgrades**.
- Race detector clean on all three OSes.

## Exit criteria

- [x] `go build ./...` and `go vet ./...` pass on Windows (and Linux/macOS in CI)
- [x] `go test -race ./...` configured in CI on ubuntu, windows, macos
- [x] VFS fuzz ran for 60s+ (~1.85M runs, discovered and fixed `/A:` drive prefix bypass)
- [x] Public API documented with GoDoc comments
- [x] API shape reviewed and validated with comprehensive test suite

## Risks

| Risk | Fallback |
|---|---|
| API proves wrong for sessions or Serve | Fix now; the fake backend makes API iteration cheap |
| Windows path edge cases in VFS | Normalize everything to a virtual POSIX namespace and never touch the host FS |

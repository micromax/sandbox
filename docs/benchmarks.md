# Sandbox Performance Benchmarks

This document records the measured performance characteristics of `github.com/micromax/sandbox` across supported runtimes and execution modes.

## Environment

- **OS**: Windows 10/11 x86_64
- **CPU**: Intel(R) Core(TM) i7-7700 CPU @ 3.60GHz (8 cores)
- **Go Version**: `go1.26.6 windows/amd64`
- **Wasm Runtime**: `github.com/tetratelabs/wazero v1.12.0` (Compiler engine)

---

## 1. Startup & Execution Latency

Measurements were taken using standard Go benchmarks (`go test -bench='Benchmark' ./backend/wasm`) executing trivial expressions and measuring end-to-end sandbox lifecycle (virtual filesystem setup, wazero module instantiation, WASI stdio configuration, execution, and cleanup).

| Runtime | Language | Cold Start (First Run) | Warm Start (Subsequent Runs) | Notes |
|---|---|---|---|---|
| **QuickJS-NG** | JavaScript (`js`) | ~120 ms | **~64 ms** | Wasm embedded in Go binary (~1.5 MB). Minimal stdlib initialization. |
| **CPython 3.13** | Python (`python`) | ~3.4 s | **~280-350 ms** | Cold includes artifact unpacking + wazero compilation. Warm loads stdlib zip modules. |

### Interpretation
- **JavaScript (QuickJS)**: Highly responsive; suitable for sub-100ms request/response loops, edge workers, and fast tool evaluation.
- **Python (CPython)**: Cold start compiles the 25 MB WASI binary. Once compiled in wazero runtime compilation cache, module instantiation and basic script execution complete in ~300 ms. For interactive multi-turn sessions, Milestone M3 (REPL Sessions) keeps the runtime warm to eliminate this startup overhead on subsequent evaluations (<5 ms per eval).

---

## 2. Memory Footprint per Instance

Memory limits are strictly enforced at the WebAssembly boundary using wazero page limits (`WithMemoryLimitPages` where 1 page = 64 KiB).

| Runtime | Baseline Memory (Guest Heap) | Recommended `Limits.Memory` | Max Host Impact on Violation |
|---|---|---|---|
| **QuickJS** | ~3.5 MB | 16 MB – 32 MB | 0 bytes leaked; execution trapped and terminated immediately |
| **CPython** | ~18 MB | 64 MB – 128 MB | 0 bytes leaked; page bounds reject expansion beyond limit |

---

## 3. Throughput & Concurrency

- **Concurrent Sandboxes**: Verified running 100 concurrent sandbox instances across goroutines (`TestConcurrentSandboxes`). All runtimes are isolated with zero cross-talk, independent ephemeral directories, and distinct virtual filesystems.
- **Zero Host Leaks**: All temporary run directories are cleaned up via `defer os.RemoveAll(runDir)`. Uncollected guest files or orphaned handles are pruned at run termination.

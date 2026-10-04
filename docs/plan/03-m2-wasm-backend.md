# 03 — M2: Wasm backend (JS + Python)

## Goal

Real isolation. `sb.Run` executes JavaScript and Python inside wazero with all limits enforced. First usable release.

## Design

### Engine setup (wazero)

- One shared `wazero.Runtime` per `Sandbox`, configured with:
  - `WithMemoryLimitPages(limit/64KiB)` — hard memory cap
  - `WithCloseOnContextDone(true)` — wall-time deadline kills the guest, even in a tight loop
  - A **compilation cache** (disk, in the cache dir) so a runtime compiles once and starts in ms afterwards
- WASI preview1 instantiated once with `wasi_snapshot_preview1`.
- **A fresh module instance per run.** Instances are never reused across runs, so nothing leaks between executions.

### Guest environment

| Resource | Provided |
|---|---|
| Args / env | Only those in `Spec`, nothing inherited from the host |
| Stdin / stdout / stderr | Pipes, with output through the capped buffer |
| Filesystem | `vfs` mounted via `WithFSConfig`: `/in` read-only, `/work` and `/out` read-write |
| Clock / randomness | Allowed (needed by runtimes). Monotonic clock is real; documented. |
| Network | None (no sockets in WASI preview1) |
| Threads / processes | None |

### Enforcement of each limit

| Limit | Mechanism |
|---|---|
| Memory | wazero page cap; allocation failure surfaces as guest trap, mapped to `ErrMemoryLimit` |
| Wall time | `context.WithTimeout` + close-on-context-done, mapped to `ErrTimeout` |
| Infinite loop / CPU | Same deadline mechanism, since wazero interrupts at loop back-edges |
| Output | `limitedBuffer`; exceeding it kills the guest, mapped to `ErrOutputLimit` |
| Disk | VFS quota |

### Packs for this milestone

| Pack | Runtime | Delivery |
|---|---|---|
| `js` | QuickJS (prefer the maintained QuickJS-NG WASI build) | `go:embed` (~1 MB) |
| `python` | CPython for WASI (official CPython WASI build) | Downloaded on first use, SHA-256 verified (see doc 10) |

Each pack defines the entry command, e.g. Python: run `python.wasm -` with code on stdin or a file at `/in/main.py`, with the stdlib mounted read-only at `/usr/lib/python3.x`.

## Execution steps

1. Spike (half a day): run a trivial `.wasm` hello-world through wazero from a Go test on all three OSes to prove the toolchain works.
2. Implement `backend/wasm` skeleton: runtime creation, compilation cache, instance per run.
3. Wire stdin/stdout/stderr and the capped buffer; map exit codes via `sys.ExitError`.
4. Mount the VFS via wazero's `FSConfig`. Verify `/in` is read-only and the quota applies on writes.
5. Implement deadline and memory-limit error mapping; verify each limit with a hostile guest.
6. Build and vet the QuickJS pack: obtain or build the `.wasm`, record version and SHA-256, embed it, implement the `js.Pack`.
7. Build the Python pack: pin a CPython WASI release, record SHA-256, implement the `artifact` downloader and cache, implement `python.Pack` including stdlib mounting.
8. Add `examples/run-js` and `examples/run-python`.
9. Benchmarks: cold start, warm start, and a memory-per-instance measurement.
10. Document the supported Python subset (no C extensions, no sockets, no subprocess) and the JS subset.

## Deliverables

`backend/wasm/`, `artifact/` (downloader and cache), `packs/js`, `packs/python`, examples, benchmark results in `docs/benchmarks.md`.

## Tests (hostile guests)

| Test | Expectation |
|---|---|
| `while(true){}` / `while True: pass` | `ErrTimeout` at the deadline, host CPU returns to idle |
| Allocate memory in a loop | `ErrMemoryLimit`, process memory stays under cap |
| Print in a loop forever | `ErrOutputLimit`, output is exactly the cap |
| Write a huge file | `ErrFSQuota` |
| Read `/etc/passwd`, `C:\Windows\...`, `../..` | Not found or denied |
| Read environment variables | Only those given in `Spec.Env` |
| `os.system`, `subprocess`, `socket` in Python | Fails cleanly |
| Fork-bomb style recursion | Stack limit trap, no host impact |
| Run 100 sandboxes concurrently | All isolated, no cross-talk, memory bounded |
| Correctness | Hello world, stdin round-trip, file output collected from `/out` |

All run on Windows, Linux and macOS in CI.

## Exit criteria

- [x] JS and Python run via `sb.Run` and return correct results
- [x] Every hostile-guest test above passes on all three OSes
- [x] Warm start under ~50 ms for JS and under ~300 ms for Python (measured, recorded)
- [x] Artifact download verifies SHA-256 and rejects a tampered file (tested)
- [x] No host file or env leak (tested)
- [x] Docs list exactly what each language can and cannot do

## Risks

| Risk | Fallback |
|---|---|
| Official CPython WASI build has gaps | Pin a known-good build (e.g. from a maintained distribution) and document it |
| Python cold start is slow | Compilation cache; optionally pre-initialized snapshot via Wizer (investigate) |
| wazero deadline latency in tight host calls | Test with `WithCloseOnContextDone`; add a watchdog goroutine as backup |
| Large Python artifact | On-demand download, separate Go module (doc 10) |

# 04 — M3: REPL Sessions

## Goal

`NewSession` returns a stateful interpreter. Variables and imports persist between `Eval` calls. A `Manager` runs many sessions safely.

## Design

### How a Wasm session works

A session is **one long-lived module instance** running the language's REPL in a goroutine. The host talks to it over stdin/stdout pipes.

```
Host                                  Guest (wasm instance, long-lived)
Eval(code) ──► write code + sentinel ──► REPL reads, executes
           ◄── read until sentinel  ◄── prints output + sentinel
```

- The pack provides a small **REPL driver script** run by the interpreter (Python: a loop with `exec` in a persistent globals dict; JS: a loop with indirect `eval`). The driver frames input/output with a random **per-session sentinel**, so guest code cannot forge an end-of-output marker.
- The driver reports the exception text and a per-eval status line separately from stdout/stderr.
- Stdout and stderr are captured independently per eval.

### Timeout semantics (important)

wazero can only stop a runaway guest by closing the instance. Therefore:

- Per-`Eval` timeout expiry **destroys the session**. `Eval` returns `ErrSessionKilled` (wrapping `ErrTimeout`), and the session is marked dead.
- Option `WithAutoRestart`: create a fresh empty session after a kill, with a clearly flagged `Result.Restarted = true`. Off by default; state loss is never silent.

### Cumulative limits

| Limit | Behavior |
|---|---|
| Memory | Applies to the whole session lifetime |
| FS quota | Applies to the whole session |
| Output | Per-eval cap and a session total cap |
| Idle timeout | Default 5 min; session closes when unused |
| Max lifetime | Default 1 hour |
| Max evals | Optional cap |

### Manager

```go
m := sandbox.NewManager(sb, sandbox.ManagerOpts{MaxSessions: 64, Idle: 5*time.Minute})
s, err := m.Create(ctx, "python")   // ErrTooManySessions when full
s2, ok := m.Get(id)
m.Close(id)
m.Shutdown(ctx)
```

- Sessions have unguessable IDs (crypto-random), so a manager can sit behind an API.
- A reaper goroutine closes idle and expired sessions.
- Evals on one session are **serialized** (mutex or channel); different sessions run concurrently.
- Shutdown closes every session and waits for goroutines (no leaks).

## Execution steps

1. Define the `Session` interface and manager types in the root package; add them to the fake backend for tests.
2. Write the Python REPL driver; test it standalone under wazero with piped stdin.
3. Write the JS REPL driver likewise.
4. Implement sentinel framing in `backend/wasm/session.go`: unique random sentinel, partial-read handling, large outputs.
5. Implement per-eval timeouts, kill handling and `ErrSessionKilled`.
6. Implement cumulative accounting and the idle and lifetime reaper.
7. Implement `Manager` with caps, ID generation and graceful shutdown.
8. Implement `WithAutoRestart`.
9. Add `examples/repl` and a CLI `sandbox repl python` command stub.
10. Run goroutine-leak checks (`goleak`) across all tests.

## Deliverables

`session.go`, `manager.go`, `backend/wasm/session.go`, REPL drivers inside the packs, examples.

## Tests

| Test | Expectation |
|---|---|
| `x = 21` then `print(x*2)` | `42` — state persists |
| Define a function, call it in a later eval | Works |
| Exception in eval 2 | Reported; session survives; eval 3 works |
| Infinite loop in eval | `ErrSessionKilled`; further `Eval` returns a clear error |
| Guest prints the sentinel text | Not treated as the end marker (random per session) |
| 1 MB output in a single eval | Handled or `ErrOutputLimit`, never a hang |
| Idle for the timeout | Session closed; resources freed |
| 200 concurrent sessions with cap 64 | `ErrTooManySessions` beyond the cap, no deadlocks |
| `go test -race` plus `goleak` | Clean |
| Two sessions never share state or files | Verified |

## Exit criteria

- [x] REPL semantics work for Python and JS
- [x] A killed session can never be silently reused
- [x] Manager caps, idle reaping and shutdown behave as specified
- [x] No goroutine or memory leaks after 1,000 create/close cycles
- [x] The sentinel framing cannot be spoofed by guest code

## Risks

| Risk | Fallback |
|---|---|
| Interpreter REPL buffers stdout oddly | Driver flushes explicitly after each eval and uses explicit framing |
| Memory growth in long sessions | Lifetime cap, memory cap, optional `Reset()` |
| Eval timeout losing state frustrates users | Documented; `WithAutoRestart`; optional cooperative timeout inside the driver (e.g. Python tracing) as a later enhancement |

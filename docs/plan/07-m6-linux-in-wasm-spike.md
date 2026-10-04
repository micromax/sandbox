# 07 — M6: Linux-in-Wasm spike (go / no-go)

## Goal

Answer one question with evidence: **can a container image run inside Wasm under wazero, usably, with a forwardable port?** If yes, it becomes the no-Docker route for "any language". If no, we document why and keep Docker as the universal backend.

This is a **time-boxed experiment** (target: 3–5 working days), not a feature.

## Hypothesis

[container2wasm](https://github.com/ktock/container2wasm) converts a container image into a Wasm module that embeds a CPU emulator and boots a real Linux userland. If it runs under wazero we get strong, daemonless, cross-platform isolation for any language.

> Unverified assumptions to test: wazero compatibility of the generated module, usable boot time, filesystem and stdin/stdout plumbing, and port forwarding for servers.

## Experiments

| # | Experiment | Success threshold |
|---|---|---|
| E1 | Convert `alpine` and run `echo hello` under wazero | Output correct |
| E2 | Convert `python:3.x-alpine`, run a script via stdin | Output correct |
| E3 | Convert a JDK image, compile and run Hello World | Works in reasonable time |
| E4 | Measure cold boot, memory and image size per image | Boot ≤ ~10 s, size ≤ a few hundred MB — to be judged with you |
| E5 | Measure compute slowdown vs native (e.g. fib(30), JSON parse) | Recorded, no hard threshold |
| E6 | Enforce limits: memory cap, deadline, output cap | Same mechanisms as M2 work |
| E7 | Run an HTTP server inside and reach it from the host through forwarding | `curl` works through a host listener |
| E8 | Isolation checks: host FS, env and network not visible | All hidden |
| E9 | Windows, Linux and macOS | Works on all three |
| E10 | Snapshot or pre-boot optimization (reuse booted state) | Evaluate feasibility only |

## Execution steps

1. Install the `c2w` converter (it needs Docker or BuildKit **only at build time**, on my machine or CI — not for end users). Record versions.
2. Run E1 under wazero with a minimal Go harness; capture exact errors if it fails and check upstream issues for wazero support status.
3. If the module requires host imports wazero doesn't provide, evaluate: implementing those imports in Go vs. giving up on wazero for this tier.
4. Run E2–E3, recording boot time, peak memory and output.
5. Run E4–E5 and write the benchmark table.
6. Run E6 and E8 against the harness with hostile guests (reuse M2's hostile tests).
7. Run E7: investigate how the generated module's networking works (virtual NIC to a host-side network stack) and whether it can be driven from Go under wazero.
8. Run E9 on all three OSes.
9. Evaluate E10 (booting once and reusing for multiple runs would amortize startup but weakens per-run isolation; assess the tradeoff).
10. Write the decision memo.

## Deliverable

`docs/spikes/linux-in-wasm.md` containing:

- Results table for E1–E10 with numbers
- A clear **GO / NO-GO / PARTIAL** recommendation
- If GO: a design for `backend/l2w` and how it plugs into the `Backend` interface and `Serve`
- If NO-GO: the exact blocker, and what would need to change upstream

## Decision rules

| Outcome | Action |
|---|---|
| E1–E3, E6, E8, E9 pass and boot is acceptable | **GO** — implement `backend/l2w` as an experimental backend |
| Runs, but port forwarding (E7) fails | **PARTIAL** — ship for `Run` only; `Serve` stays Docker-only |
| Too slow or doesn't run on wazero | **NO-GO** — Docker stays the universal backend; revisit if upstream improves |

## Exit criteria

- [ ] Results recorded for every experiment
- [ ] Decision memo written and shared with you
- [ ] You make the go/no-go call

## Risks

| Risk | Fallback |
|---|---|
| Emulation too slow for practical use | Valid finding; NO-GO |
| Large images (hundreds of MB) | Smaller base images; on-demand download; possibly acceptable for a niche tier |
| wazero lacks required features | Evaluate contributing or implementing the host imports; otherwise NO-GO |
| Time overrun | Hard stop at the time-box; report what was learned |

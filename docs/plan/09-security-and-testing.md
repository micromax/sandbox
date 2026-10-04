# 09 — Security & Testing Strategy (cross-cutting)

## Security principles

1. **Deny by default.** No network, no host files, no env, no processes, no persistence unless explicitly granted.
2. **No silent downgrades.** If strong isolation is unavailable, fail; never fall back to weaker.
3. **Defense in depth.** Backend isolation, plus resource caps, plus host-side validation of everything coming out of the guest.
4. **The guest's output is hostile.** Files, tar archives, stdout, HTTP responses, and sentinels are all validated and bounded.
5. **Least exposure.** Loopback binds, random ports, short TTLs, tokens.
6. **Pinned and verified artifacts.** SHA-256 for Wasm, digests for images.
7. **Honest docs.** Say what is *not* protected.

## Isolation comparison (goes into `docs/security.md`)

| | Wasm | Docker | Docker + gVisor | Linux-in-Wasm |
|---|---|---|---|---|
| Host kernel exposed to guest | No | Yes (shared) | Mostly no | No |
| Escape surface | wazero + Go | Kernel + runc | gVisor | wazero + emulator |
| Needs daemon | No | Yes | Yes | No |
| Speed | Fast | Fast | Medium | Slow |

## Test layers

| Layer | What | Where |
|---|---|---|
| Unit | limits, vfs, router, netpolicy, proxy, tar sanitizer | every PR, 3 OSes |
| Fuzz | path normalizer, tar extraction, HTTP proxy parsing, sentinel framing | nightly CI, plus seed corpus in repo |
| Hostile-guest suite | a fixed corpus of attack programs per language and backend (loops, memory bombs, fork bombs, FS probing, env probing, network probing, output flood) | every PR for Wasm; Linux CI for Docker |
| Integration | real language runs, sessions, serve | every PR |
| Race and leak | `-race`, `goleak`, port and container leak checks | every PR |
| Benchmarks | cold and warm start, memory per instance, throughput | tracked per release, regression alert |
| Cross-platform | Windows, Linux, macOS (amd64, arm64 where available) | CI matrix |

## Hostile-guest corpus (shared across backends)

Stored in `internal/hostile/` as per-language sources with an expected outcome file:

- CPU: infinite loop, deep recursion, regex catastrophic backtracking
- Memory: unbounded allocation, huge single allocation
- Disk: unbounded file writes, many tiny files, path traversal in names
- Output: infinite print, giant single line, ANSI/NUL bytes, fake sentinel text
- Info leaks: read env, `/proc`, host paths, hostname, home dirs, mounted drives
- Process: fork bomb, exec of host binaries, `subprocess`, `os.system`
- Network: connect out, scan loopback and LAN, cloud metadata IP, DNS exfiltration
- Escape: syscalls (`ptrace`, `mount`), unusual file descriptors, `/proc/self/exe` tricks (Docker)
- Time: very long sleep, clock manipulation attempts

A new attack found anywhere is added to the corpus as a regression test.

## Review checklist per milestone

- [ ] New code paths handle hostile input (sizes, encodings, paths)
- [ ] Every new limit has a test that proves it triggers
- [ ] Every new default was chosen as the safe one
- [ ] No goroutine, port, file or container leaks
- [ ] Docs updated, including known limitations

## Supply chain

- Minimal dependencies, reviewed on addition; `go mod verify` in CI.
- `govulncheck` in CI.
- Artifact manifest (name, version, URL, SHA-256, license) lives in the repo and is reviewed on change.
- Release binaries get checksums; consider signing later.

## Vulnerability handling

`SECURITY.md` defines a private reporting address and expected response times. Security fixes get a patch release and a changelog entry.

## How I will execute this

This is not a separate milestone. Each milestone's tests and exit criteria include the relevant parts. The hostile corpus starts in M2 and grows with every milestone.

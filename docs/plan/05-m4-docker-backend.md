# 05 — M4: Docker/Podman backend

## Goal

An **opt-in** backend that runs any language available as a container image (Java, Go, Rust, C#, Node, ...) under strict hardening, using the same `Run` and `NewSession` API.

## Design

### Talking to the engine

- Use the Docker Engine HTTP API directly (no heavy SDK):
  - Linux/macOS: unix socket (`/var/run/docker.sock`, plus Podman's socket)
  - Windows: named pipe `\\.\pipe\docker_engine` (small dependency such as `go-winio`, justified because it is required to dial the pipe)
- Auto-detect the socket. Backend reports `ErrBackendUnavailable` if the daemon isn't reachable.
- Pin the API version.

### Hardening profile (applied to every container, not configurable downward)

| Setting | Value |
|---|---|
| Network | `none` (unless network policy enables it) |
| Root FS | `ReadonlyRootfs: true` |
| Writable space | Small `tmpfs` at `/work` with size = `FSQuota`, `noexec` unless the language needs exec |
| Capabilities | `CapDrop: ALL`, no `CapAdd` |
| Privileges | `no-new-privileges`, never `--privileged`, never host PID/IPC/net |
| User | Non-root UID/GID (e.g. 65534) |
| Resources | `Memory`, `MemorySwap = Memory`, `NanoCPUs`, `PidsLimit` |
| Seccomp / AppArmor | Engine default profile, or a stricter custom profile |
| Mounts | **Never** bind-mount host paths. Code and files go in via the archive API (`PUT /containers/{id}/archive`). |
| Lifetime | Auto-remove; killed at the wall-time deadline; labeled `micromax.sandbox=1` for orphan cleanup |
| Runtime | Optional `runtime: runsc` (gVisor) for stronger isolation when installed |

### Lifecycle of a run

1. Ensure the image is present (pull by **digest**, not tag).
2. Create the container with the profile above.
3. Upload code and files as a tar archive.
4. Start; attach to stdout/stderr (multiplexed stream), apply the output cap.
5. Wait with the wall-time deadline; on timeout, kill and remove.
6. Download `/out` as a tar, extract into `Result.Files` (with path sanitization — treat the tar as hostile).
7. Remove the container and report usage from stats.

### Packs added

| Pack | Image (pinned by digest) | Run |
|---|---|---|
| `java` | small JDK image | `javac` + `java` inside the container, tmpfs workspace |
| `go` | official golang image (or slim toolchain) | `go run` with module cache in tmpfs |
| `rust` | official rust image | `rustc` + run |
| `node` | node alpine | real Node.js |
| `bash` | alpine | `sh` |

### Sessions on containers

One container kept alive with a REPL process attached (stdin/stdout) — same sentinel framing as M3 where a REPL exists. Compiled languages use "stateless re-run with accumulated source" or are marked as sessions-unsupported. Documented per pack.

### Orphan cleanup

At startup and periodically, remove containers carrying our label that are older than their TTL, so crashes never leave containers running.

## Execution steps

1. Engine client: dial socket or pipe, version negotiation, ping, error mapping.
2. Image management: pull by digest, local presence check, progress streaming, size limit warnings.
3. Container create/start/attach/wait/kill/remove with the hardening profile.
4. Archive upload and download with tar sanitization (reject `..`, absolute paths, symlinks and hardlinks leaving the root, device files; enforce byte and file caps).
5. Implement output capture and cap, deadline enforcement, usage stats.
6. Implement the label-based orphan reaper.
7. Add packs: `bash`, `node`, `java`, `go`, `rust`. Pin image digests and record them.
8. Implement container-backed sessions where a REPL exists.
9. Optional gVisor runtime flag and detection.
10. Write the security doc: what Docker does and does not protect against, why the Docker socket is root-equivalent on the host, and rootless/Podman recommendation.

## Deliverables

`backend/docker/`, `packs/{bash,node,java,go,rust}`, orphan reaper, `docs/docker-security.md`.

## Tests (integration, run where Docker is available)

| Test | Expectation |
|---|---|
| Hello world in each pack | Correct output |
| Reach the internet / host / metadata IP (`169.254.169.254`) | Fails (network none) |
| Write to root FS | Read-only error |
| Fill `/work` | tmpfs quota error |
| Fork bomb | Stopped by `PidsLimit` |
| Memory bomb | OOM-killed, mapped to `ErrMemoryLimit` |
| Infinite loop | Killed at the deadline; container removed |
| Escape attempts (`mount`, `ptrace`, writing `/proc/sysrq-trigger`, raw sockets) | Denied by caps and seccomp |
| Hostile tar output (`../../etc/x`, symlink out, huge file) | Rejected and sanitized |
| Kill the host process mid-run | Orphan reaper removes the container later |
| Docker not installed | `ErrBackendUnavailable` with an actionable message |

CI: Linux runners with Docker; Windows and macOS best effort (Docker Desktop is not standard on hosted runners — document manual test results).

## Exit criteria

- [ ] All five packs run hello-world and a compile-error case correctly
- [ ] Every hardening test passes
- [ ] No container survives a test run (checked by label)
- [ ] Router correctly falls back (`PreferWasm`) only when allowed and never downgrades silently
- [ ] Security doc reviewed by you

## Risks

| Risk | Fallback |
|---|---|
| Windows named-pipe quirks | Isolate in a small file with build tags; test on Windows manually |
| Image pull slow or large | Pre-pull command in the CLI; cache; use slim images |
| Docker socket exposure concerns | Document; recommend rootless Docker/Podman; support a configurable socket path |
| Shared kernel (not a VM) | Offer gVisor option; recommend it for hostile multi-tenant use |

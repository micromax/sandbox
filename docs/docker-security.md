# Docker Backend Security Architecture & Hardening Guide

The Docker/Podman backend (`backend/docker`) allows `github.com/micromax/sandbox` to run heavy and compiled languages (Java, Go, Rust, C#, Node.js, Bash) inside containerized environments while maintaining defense-in-depth isolation.

---

## 1. Threat Model & Security Guarantees

Containers share the host operating system kernel (Linux cgroups, namespaces, and seccomp). Standard Docker defaults allow significant host access if not explicitly hardened.

`github.com/micromax/sandbox` enforces a non-configurable, locked-down security profile on **every** container:

| Security Control | Value Enforced | Threat Mitigated |
|---|---|---|
| **Root Filesystem** | `ReadonlyRootfs: true` | Prevents guest from modifying container binaries, installing system packages, or writing persistence hooks. |
| **Linux Capabilities** | `CapDrop: ["ALL"]` | Drops all POSIX capabilities (e.g. `CAP_NET_RAW`, `CAP_SYS_ADMIN`, `CAP_SYS_PTRACE`, `CAP_DAC_OVERRIDE`). Guest cannot mount, sniff, or debug. |
| **Privilege Escalation** | `no-new-privileges:true` | Prevents SUID/SGID binaries inside the image from acquiring higher privileges. |
| **Network Isolation** | `NetworkMode: "none"` | Disables all network interfaces except loopback. Completely blocks access to host LAN, internet, and cloud metadata endpoints (`169.254.169.254`). |
| **No Host Bind-Mounts** | `Never bind-mount host paths` | Code and files are copied via tar archive over the Docker REST API (`PUT /containers/{id}/archive`). The host filesystem is never mounted. |
| **Output Sanitization** | Strict Hostile Tar Scanner | Output files extracted from `/out` reject path traversals (`..`), absolute paths, and escaping symlinks. |
| **Resource Quotas** | Memory, NanoCPUs, PidsLimit | Hard caps on RAM (with swap disabled), CPU, and thread count to defeat fork bombs and memory exhaustion attacks. |
| **Ephemeral Writable Disk**| In-memory `tmpfs` at `/work` and `/out` | Writable scratch space lives purely in RAM up to `Limits.FSQuota`. Deleted automatically on exit. |
| **Orphan Auto-Cleanup** | `micromax.sandbox=1` Label | Containers are killed at the deadline and removed. A background reaper prunes stale or abandoned sandbox containers. |

---

## 2. Docker Daemon Root-Equivalence & Recommendations

> [!WARNING]
> Access to the Docker socket (`/var/run/docker.sock` or `\\.\pipe\docker_engine`) is equivalent to `root` access on the host machine. Any process with write access to the Docker socket can effectively control the host system.

### Recommended Production Deployments:

1. **Rootless Docker / Podman (Recommended for Multi-Tenant Hosts)**:
   - Run the Docker daemon or Podman daemon in **rootless mode** under an unprivileged user account.
   - If a container escape occurs, the attacker only has the permissions of the unprivileged host user, not host `root`.
2. **gVisor Runtime (`runsc`)**:
   - For hostile multi-tenant code execution, configure Docker with the gVisor runtime (`docker.WithRuntime("runsc")`).
   - gVisor runs an application kernel in userspace, intercepting system calls and eliminating shared kernel attack surface.
3. **Pure Go WebAssembly Backend**:
   - For JavaScript, Python, and lightweight tasks, prefer the **Wasm backend** (`sandbox.WithPolicy(sandbox.PreferWasm)`). The Wasm backend runs entirely in-process with zero external daemons and zero kernel exposure.

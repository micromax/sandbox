# Security Architecture & Threat Model

`github.com/micromax/sandbox` is designed to execute **untrusted, hostile, and AI-generated code** safely on host systems. This document outlines our threat model, security guarantees, isolation mechanisms, and known boundaries.

---

## 1. Threat Model & Assumptions

### Attacker Capabilities
We assume the code running inside the sandbox is completely untrusted and potentially malicious. The guest code may actively attempt to:
1. **Exfiltrate data**: Read local files (`/etc/passwd`, Windows registry, SSH keys), scan the local network, or access cloud instance metadata (`169.254.169.254`).
2. **Denial of Service (DoS)**: Consume all host CPU (infinite loops, fork bombs), allocate unbounded memory, fill up the host disk with huge files, or spam network sockets.
3. **Host privilege escalation / escape**: Exploit runtime or kernel vulnerabilities to execute arbitrary code with host user or root privileges.
4. **Data leakage between runs**: Leave files, background processes, or memory state behind that leaks into subsequent executions.

### Trust Boundary
* **Trusted**: The Go host application importing `github.com/micromax/sandbox`, host OS kernel, and host hardware.
* **Untrusted**: The guest code, guest runtime dependencies, and any files written by the guest into the sandbox.

---

## 2. Two-Tier Isolation Architecture

`sandbox` implements two distinct isolation backends tailored for different security and operational trade-offs:

```
                          ┌───────────────────────────┐
                          │    Go Host Application    │
                          └─────────────┬─────────────┘
                                        │
                         Router Policy (sandbox.Policy)
                                        │
               ┌────────────────────────┴────────────────────────┐
               ▼                                                 ▼
    ┌──────────────────────┐                          ┌──────────────────────┐
    │  Tier 1: Wasm Backend │                          │ Tier 2: Docker Backend│
    ├──────────────────────┤                          ├──────────────────────┤
    │ • Pure Go (wazero)   │                          │ • Hardened OCI       │
    │ • In-process isolate │                          │ • Readonly rootfs    │
    │ • Zero host daemon   │                          │ • Dropped ALL caps   │
    │ • JS, Python         │                          │ • Go, Rust, Java, etc│
    │ • Sub-100ms startup  │                          │ • Ephemeral tmpfs    │
    └──────────────────────┘                          └──────────────────────┘
```

---

## 3. Defense-in-Depth Security Matrix

| Security Layer | Tier 1: WebAssembly (`backend/wasm`) | Tier 2: Docker (`backend/docker`) |
|---|---|---|
| **Execution Boundary** | In-process WebAssembly virtual machine (WASI Preview 1 via wazero). No access to host memory or registers. | Linux kernel namespaces (PID, mount, UTS, IPC, network, cgroups). |
| **Filesystem Isolation** | In-memory Virtual Filesystem (`vfs.FS`). Guest only sees `/in` (read-only), `/work` (scratch), `/out` (collected). No access to host disk. | `ReadonlyRootfs: true`. Host filesystem is never bind-mounted. Work and output reside in ephemeral `tmpfs` mounts. |
| **Path Traversal Defenses** | Virtual filesystem path normalizer blocks `..`, NUL bytes, drive letters (`C:`), and absolute paths escaping the root. | Hostile tar extractor validates all files extracted from `/out`, rejecting path traversal attacks and escaping symlinks. |
| **Memory Hard Limits** | WebAssembly page cap (`wazero.WithMemoryLimitPages`). Allocations beyond the limit trap immediately. | Linux cgroups hard memory cap with swap disabled (`MemorySwap == Memory`). OOM killer terminates guest cleanly. |
| **CPU & Timeout Enforcement** | Wall-clock deadline (`WithCloseOnContextDone`). Wazero interrupts loops at loop back-edges. | Process context deadline (`SIGKILL` sent immediately on deadline expiry). |
| **Process Fork Bomb Protection** | Single-threaded execution. WASI has no fork/clone system calls. | `PidsLimit` enforced on the container (cgroup PID controller). |
| **Network (Default)** | Completely disabled. WASI Preview 1 has no socket interfaces. | `NetworkMode: "none"`. All interfaces disabled except loopback. |
| **Network (When Enabled)** | Host-function HTTP bridge. Strictly bounded by `netpolicy` allow-lists, SSRF filters, and byte caps. | Isolated bridge network with egress routed through an in-process filtering proxy. |
| **Serving Exposed Ports** | Host proxy binds to `127.0.0.1` and translates HTTP requests to in-process function calls. | Ephemeral loopback port mapping routed through the host proxy with rate limits and optional bearer auth. |
| **State Sanitization** | Ephemeral module instance per run. Zero state is retained across runs (except explicit REPL sessions). | Containers are removed on termination. Background reaper prunes orphaned containers. |

---

## 4. What is Protected vs. What is NOT Protected

### What is Protected
* **Host filesystem confidentiality and integrity**: Guest code cannot read host files or write outside designated output buffers.
* **Host availability**: Guest code cannot exhaust host RAM, disk, or lock up host CPU threads permanently.
* **Network perimeter**: Sandboxes cannot probe internal subnets, local development servers, or cloud instance metadata (SSRF).
* **Cross-tenant data privacy**: Independent executions run in isolated instances with independent virtual filesystems.

### What is NOT Protected (Explicit Boundaries)
* **Algorithmic Complexity within Timeouts**: A guest may consume 100% of a single CPU core up until the configured execution deadline.
* **Shared Kernel Exploits (Docker Backend)**: If a zero-day Linux kernel vulnerability allows escaping namespaces, a root container could theoretically compromise the host. For untrusted multi-tenant workloads using Docker, we recommend:
  - Running rootless Docker or Podman.
  - Configuring gVisor (`runsc`) as the container runtime.
* **Wasm Side-Channel Attacks**: Pure WebAssembly does not provide hardware-level cache timing / Spectre mitigations against microarchitectural side-channels. Do not co-locate hostile guests in the same process with highly sensitive cryptographic keys.

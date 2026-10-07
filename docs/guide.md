# The Complete Developer Guide to `micromax/sandbox`

`github.com/micromax/sandbox` is an embeddable Go library designed to run **untrusted, hostile, or AI-generated code** safely in isolation.

Whether you are building an AI code interpreter, an automated grading system, a plugin runner, or a multi-tenant backend, `sandbox` provides defense-in-depth security with:
* **Zero external dependencies** for common languages (Pure Go + WebAssembly via wazero, `CGO_ENABLED=0`).
* **Hardened container isolation** for heavy compiled languages (Docker/Podman).
* **Hard resource caps** on memory, wall-clock time, stdout/stderr, and disk quotas.
* **Strict virtual filesystem isolation** (`/in`, `/work`, `/out`) preventing host leaks.
* **Deny-by-default network policy** with SSRF and DNS rebinding protections.
* **Network serving** to expose guest services through a secured host reverse proxy.

---

## Table of Contents

1. [Installation](#1-installation)
2. [Quickstart (30 Seconds)](#2-quickstart-30-seconds)
3. [Architecture: Two-Tier Isolation](#3-architecture-two-tier-isolation)
4. [Language Packs & Capabilities](#4-language-packs--capabilities)
   - [Tier 1: Pure Wasm (Zero Docker)](#tier-1-pure-wasm-zero-docker)
   - [Tier 2: Docker (Universal Support)](#tier-2-docker-universal-support)
5. [Enforcing Resource Limits](#5-enforcing-resource-limits)
6. [Virtual Filesystem (VFS) & File I/O](#6-virtual-filesystem-vfs--file-io)
7. [Stateful REPL Sessions](#7-stateful-repl-sessions)
8. [Serving Network Services (`Serve`)](#8-serving-network-services-serve)
9. [Outbound Network Policies & SSRF Guards](#9-outbound-network-policies--ssrf-guards)
10. [Command-Line Interface (CLI)](#10-command-line-interface-cli)
11. [AI Agent & Model Context Protocol (MCP)](#11-ai-agent--model-context-protocol-mcp)
12. [Troubleshooting & Best Practices](#12-troubleshooting--best-practices)

---

## 1. Installation

### Add to your Go project:
```bash
go get github.com/micromax/sandbox
```

### Install the CLI tool:
```bash
go install github.com/micromax/sandbox/cmd/sandbox@latest
```

Verify your environment anytime with:
```bash
sandbox doctor
```

---

## 2. Quickstart (30 Seconds)

Running sandboxed code in Go requires three simple steps: create a backend, register language packs, and call `sb.Run()`.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
)

func main() {
	// 1. Initialize the pure-Go Wasm backend (zero CGO, zero Docker)
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatal(err)
	}

	// 2. Create the sandbox with the JavaScript language pack
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 3. Execute untrusted code safely
	ctx := context.Background()
	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: "js",
		Code: `console.log("Hello from inside the sandbox! 6 * 7 = " + (6 * 7));`,
	})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}

	fmt.Printf("Output:\n%s", res.Stdout)
	fmt.Printf("Duration: %v | Backend: %s\n", res.Usage.Wall, res.Backend)
}
```

---

## 3. Architecture: Two-Tier Isolation

`micromax/sandbox` employs a clean two-tier architecture so you are **never forced to run Docker** unless you explicitly need heavy compilers:

```
                            ┌────────────────────────┐
                            │  Go Host Application   │
                            └───────────┬────────────┘
                                        │
                            Routing Policy (sandbox.Policy)
                                        │
               ┌────────────────────────┴────────────────────────┐
               ▼                                                 ▼
    ┌──────────────────────┐                          ┌──────────────────────┐
    │ Tier 1: Wasm Backend │                          │Tier 2: Docker Backend│
    ├──────────────────────┤                          ├──────────────────────┤
    │ • 100% Pure Go       │                          │ • Hardened OCI       │
    │ • Zero Docker / CGO  │                          │ • Readonly rootfs    │
    │ • Sub-50ms startup   │                          │ • Dropped ALL caps   │
    │ • JS, TS, Python,    │                          │ • Java, Rust, Go,    │
    │   Lua, Raw WASI      │                          │   Node, Bash         │
    └──────────────────────┘                          └──────────────────────┘
```

You can choose your routing policy when initializing:
* `sandbox.WithPolicy(sandbox.WasmOnly)`: Strictly rejects any language that requires Docker.
* `sandbox.WithPolicy(sandbox.PreferWasm)`: Uses Wasm where available, falling back to Docker.
* `sandbox.WithPolicy(sandbox.DockerOnly)`: Runs all tasks in containers.

---

## 4. Language Packs & Capabilities

### Tier 1: Pure Wasm (Zero Docker)

All Tier 1 languages run 100% in-process via `wazero` with no external daemons, no CGO, and no container engine:

#### 1. JavaScript (`packs/js`)
* **Engine**: QuickJS-NG compiled to WASI Preview 1.
* **Embedded**: ~1.5 MB bundled directly inside the Go binary. Instant execution (<50ms).
* **Features**: Full ECMAScript 2023, REPL sessions, in-process HTTP handler bridge.

#### 2. TypeScript (`packs/ts`)
* **Engine**: Embedded QuickJS-NG + pure Go transpiler (type stripping & enum transform).
* **Delivery**: Zero downloads, zero Node.js, zero Docker. Instant execution (<50ms).
* **Features**: Full TypeScript types, interfaces, enums, generics stripped on the fly; REPL sessions and HTTP handler bridge supported.
* **Execution**:
  ```go
  res, err := sb.Run(ctx, sandbox.Spec{
      Lang: "ts",
      Code: `
          interface Task { id: number; title: string; }
          const t: Task = { id: 1, title: "Pure Wasm TypeScript" };
          console.log("Running task:", t.title);
      `,
  })
  ```

#### 3. Python (`packs/python`)
* **Engine**: Official CPython 3.13 WASI build.
* **On-Demand**: Downloaded automatically on first use into local cache and verified by SHA-256.
* **Features**: Full pure-Python standard library (`json`, `re`, `math`, `datetime`, `collections`, etc.), REPL sessions, WSGI handler bridge.

#### 4. Lua (`packs/lua`)
* **Engine**: Lua 5.4.6 compiled to WASI.
* **Embedded**: ~320 KB bundled directly into the Go binary. Blazing fast startup (<20ms).
* **Features**: Standard Lua math, string, table, and I/O libraries.

#### 5. Raw WASI (`packs/wasm`)
* **Runs any pre-compiled WASI binary** directly in pure Go without Docker.
* **Compiled Languages Enabled**:
  * **Go**: `GOOS=wasip1 GOARCH=wasm go build -o main.wasm main.go`
  * **Rust**: `cargo build --target wasm32-wasip1`
  * **Zig**: `zig build-exe -target wasm32-wasi main.zig`
  * **C/C++**: `clang --target=wasm32-wasi -o main.wasm main.c`
* **Execution**:
  ```go
  res, err := sb.Run(ctx, sandbox.Spec{
      Lang: "wasm",
      Files: map[string][]byte{"main.wasm": wasmBinaryBytes},
      Args:  []string{"arg1", "arg2"},
  })
  ```

---

### Tier 2: Docker (Universal Support)

For languages requiring heavy runtime toolchains, the Docker backend runs code in locked-down OCI containers:
* **Java (`packs/java`)**: OpenJDK 21 with on-the-fly compilation (`javac`).
* **Go (`packs/go`)**: Full Go compiler toolchain (`go run`).
* **Rust (`packs/rust`)**: Full Rust compiler (`rustc`).
* **Node.js (`packs/node`)**: Full Node.js runtime with npm/npx.
* **Bash (`packs/bash`)**: Standard POSIX shell and core utilities.

---

## 5. Enforcing Resource Limits

Untrusted code can enter infinite loops, attempt fork bombs, or allocate gigabytes of RAM. `sandbox` enforces hard caps configured field-by-field:

```go
spec := sandbox.Spec{
    Lang: "python",
    Code: hostileCode,
    Limits: &sandbox.Limits{
        Memory:    64 << 20,          // 64 MiB hard cap (allocations beyond this trap immediately)
        WallTime:  5 * time.Second,   // Wall-clock deadline (SIGKILL or Wasm interruption)
        MaxOutput: 1 << 20,           // 1 MiB combined stdout + stderr cap
        FSQuota:   16 << 20,          // 16 MiB virtual disk quota
        MaxFiles:  128,               // Maximum files + directories allowed
    },
}
```

### Handling Limit Violations
When a limit trips, `sb.Run()` returns a partial `Result` containing whatever output was captured before termination, paired with a sentinel error:

```go
res, err := sb.Run(ctx, spec)
if err != nil {
    switch {
    case errors.Is(err, sandbox.ErrTimeout):
        fmt.Println("Guest exceeded execution time limit")
    case errors.Is(err, sandbox.ErrMemoryLimit):
        fmt.Println("Guest exceeded memory allocation cap")
    case errors.Is(err, sandbox.ErrOutputLimit):
        fmt.Printf("Guest produced too much output (captured %d bytes)\n", len(res.Stdout))
    case errors.Is(err, sandbox.ErrFSQuota):
        fmt.Println("Guest exceeded disk quota")
    }
}
```

---

## 6. Virtual Filesystem (VFS) & File I/O

Guests **never touch the host filesystem**. Instead, `sandbox` mounts an in-memory virtual filesystem structured into three designated directories:

* `/in`: Read-only input files injected by the host via `Spec.Files`.
* `/work`: Writable scratch space for intermediate artifacts.
* `/out`: Writable output directory. Files created here are collected and returned in `Result.Files`.

```go
res, err := sb.Run(ctx, sandbox.Spec{
    Lang: "js",
    Files: map[string][]byte{
        "dataset.csv": []byte("name,score\nAlice,95\nBob,88"),
    },
    Code: `
        // Read injected input
        const raw = std.loadFile('/in/dataset.csv');
        
        // Write generated report to /out
        const out = std.open('/out/report.json', 'w');
        out.puts(JSON.stringify({ lines: raw.trim().split('\n').length }));
        out.close();
    `,
})

// Retrieve collected files:
reportJSON := res.Files["report.json"]
fmt.Printf("Generated report: %s\n", string(reportJSON))
```

Path traversal attacks (e.g. `../etc/passwd`, drive letters `C:`, NUL bytes) are rejected automatically at the API boundary.

---

## 7. Stateful REPL Sessions

For conversational AI agents and notebooks, `sandbox.NewSession` provides stateful multi-turn evaluation without restarting the guest process:

```go
sess, err := sb.NewSession(ctx, "python")
if err != nil {
    log.Fatal(err)
}
defer sess.Close()

// Turn 1: define state
res1, _ := sess.Eval(ctx, "x = [1, 2, 3]")

// Turn 2: mutate state
res2, _ := sess.Eval(ctx, "x.append(4)")

// Turn 3: read state
res3, _ := sess.Eval(ctx, "print('Sum:', sum(x))")
fmt.Println(string(res3.Stdout)) // Output: Sum: 10
```

---

## 8. Serving Network Services (`Serve`)

Milestone 5 allows running HTTP servers and web apps inside the sandbox, exposing them to the host **without granting the guest direct host network access**:

```go
svc, err := sb.Serve(ctx, spec, sandbox.ServeOpts{
    Ports: []sandbox.PortMap{{Guest: 8000}}, // Host port auto-assigned on 127.0.0.1
    Bind:  sandbox.Loopback,                 // Default: loopback only
    TTL:   10 * time.Minute,                 // Auto-shutdown timer
    Auth:  sandbox.BearerToken(""),          // Generates an unguessable access token
})
if err != nil {
    log.Fatal(err)
}
defer svc.Stop()

fmt.Printf("Service reachable at: %s\n", svc.URL())
fmt.Printf("Bearer token: %s\n", svc.Token())
```

* **Loopback by Default**: Prevents external LAN access unless explicitly enabled.
* **Bearer Token Auth**: The host proxy intercepts unauthenticated requests and responds with `401 Unauthorized` before traffic reaches the guest.
* **Slowloris & Body Limiters**: Enforces strict header read timeouts and body size caps.

---

## 9. Outbound Network Policies & SSRF Guards

By default, all sandboxes are completely air-gapped (`Spec.Net == nil`). To allow controlled egress (e.g. calling an external API):

```go
spec := sandbox.Spec{
    Lang: "python",
    Code: script,
    Net: &sandbox.NetPolicy{
        AllowHosts:   []string{"api.github.com", "*.pypi.org"},
        AllowPorts:   []int{443},
        MaxRequests:  50,
        MaxBytes:     10 << 20, // 10 MiB bandwidth cap
        AllowPrivate: false,    // Strictly blocks RFC1918, loopback, and metadata
    },
}
```

### Built-in Defenses
1. **SSRF Guard**: Blocks `127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, and cloud metadata (`169.254.169.254`).
2. **DNS Rebinding Prevention**: Resolves hostnames once on the host, validates the IP against the blocklist, and pins the connection to the validated IP.
3. **Audit Log (`Result.NetLog`)**:
   ```go
   for _, entry := range res.NetLog {
       fmt.Printf("[%s] %s %s -> %d (%d bytes, %v)\n",
           entry.Timestamp.Format(time.RFC3339),
           entry.Method, entry.URL, entry.StatusCode, entry.BytesTransferred, entry.Duration,
       )
   }
   ```

---

## 10. Command-Line Interface (CLI)

The `sandbox` CLI is built on the public Go API, providing a full terminal companion:

```bash
# Run a script with limits
sandbox run --lang python --timeout 5s --mem 64M script.py

# Infer language automatically from extension
sandbox run script.js
sandbox run logic.lua
sandbox run binary.wasm

# JSON output mode for scripting & pipelines
sandbox run --json script.py

# Start an interactive REPL
sandbox repl --lang python

# Serve a sandboxed web app
sandbox serve --lang js --port 8080 server.js

# Start Model Context Protocol (MCP) server for Claude / Cursor
sandbox mcp

# Health check and environment diagnostics
sandbox doctor

# List registered language packs
sandbox packs list
```

---

## 11. AI Agent & Model Context Protocol (MCP)

`micromax/sandbox` provides native support for the **Model Context Protocol (MCP)**, allowing AI assistants like **Claude Desktop**, **Cursor**, and custom LLM agents to use the sandbox as their secure code execution engine.

### Quick Setup with Claude Desktop

Add `sandbox` to `%APPDATA%\Claude\claude_desktop_config.json` (Windows) or `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS):

```json
{
  "mcpServers": {
    "sandbox": {
      "command": "sandbox",
      "args": ["mcp"]
    }
  }
}
```

Claude Desktop will automatically acquire 3 secure tools:
* `execute_code`: One-shot isolated execution for Python, TypeScript, JavaScript, Lua, Go, Rust, Java, and Bash.
* `eval_session`: Persistent multi-turn REPL execution that preserves variables across conversation turns.
* `list_languages`: Environment inspection.

For complete setup guides and OpenAI/Anthropic Go SDK examples, see [docs/mcp.md](file:///c:/Users/hp/sandbox/docs/mcp.md).

---

## 12. Troubleshooting & Best Practices

| Symptom | Cause | Solution |
|---|---|---|
| `docker daemon is not reachable` | Running a Tier 2 language (Java, Go, Rust, Bash) when Docker is not installed. | Either start Docker Desktop/Podman, or switch to Tier 1 languages (JavaScript, Python, Lua, or pre-compile to `.wasm`). |
| `sandbox: memory limit exceeded` | Guest exceeded `Limits.Memory` page cap. | Increase `Limits.Memory` (e.g. `128 << 20` for Python, `256 << 20` for JVM). |
| `sandbox: time limit exceeded` | Guest code took longer than `Limits.WallTime`. | Increase `Limits.WallTime` or optimize guest algorithms. |
| `network access denied` | Guest attempted network I/O with `Spec.Net == nil`. | Supply a configured `*sandbox.NetPolicy` with `AllowHosts`. |
| `SSRF violation` | Request targeted private IP or `169.254.169.254`. | Set `AllowPrivate: true` only if targeting internal microservices intentionally. |

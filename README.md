# micromax/sandbox

[![Go Reference](https://pkg.go.dev/badge/github.com/micromax/sandbox.svg)](https://pkg.go.dev/github.com/micromax/sandbox)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> Run untrusted or AI-generated code in strict isolation, as an embeddable Go library.

`github.com/micromax/sandbox` provides defense-in-depth execution environments with **zero host daemons required** for lightweight languages and **hardened container sandboxes** for heavy compilers.

---

## Key Features

* **Zero-Daemon Pure Go Backend (`wazero`)**: Run JavaScript, TypeScript, Python, Lua, and pre-compiled Wasm binaries with zero CGO (`CGO_ENABLED=0`) and no Docker required.
* **Universal Docker Tier (Opt-In)**: Hardened, read-only OCI containers with dropped capabilities for Java, Go, Rust, Node.js, and Bash.
* **Hard Resource Limits**: Strict caps on memory, wall-clock time, stdout/stderr byte limits, and virtual disk quotas.
* **Virtual Filesystem (VFS)**: Isolated in-memory filesystem (`/in`, `/work`, `/out`) that prevents host filesystem leaks and rejects path traversal attacks.
* **Stateful REPL Sessions**: Multi-turn conversational code execution for AI agents and notebooks.
* **Serving Network Ports (`Serve`)**: Expose guest web services through a secured host-side reverse proxy with rate limiting and bearer token authentication.
* **Deny-By-Default Outbound Networking**: Optional host allow-lists with SSRF guards and DNS rebinding prevention.
* **AI Agent & Model Context Protocol (MCP)**: Built-in `sandbox mcp` server for Claude Desktop, Cursor, and LLM agent function calling.
* **Unified CLI (`sandbox`)**: Terminal companion for running, testing, serving, and diagnosing sandboxes.

---

## Language Support Matrix

| Language | Identifier(s) | Default Backend | Cold Start | Memory Floor | Host Requirements |
|---|---|---|---|---|---|
| **JavaScript** | `js`, `javascript` | **Wasm** (QuickJS-NG) | ~50 ms | ~5 MB | **Zero Docker** (Embedded in binary) |
| **TypeScript** | `ts`, `typescript` | **Wasm** (QuickJS-NG) | ~50 ms | ~5 MB | **Zero Docker** (Embedded in binary) |
| **Python** | `python`, `py` | **Wasm** (CPython 3.13) | ~250 ms | ~25 MB | **Zero Docker** (Cached WASI bundle) |
| **Lua** | `lua`, `lua54` | **Wasm** (Lua 5.4.6) | ~20 ms | ~2 MB | **Zero Docker** (Embedded in binary) |
| **Raw WASI** | `wasm`, `wasi`, `wasip1` | **Wasm** (wazero) | ~5–15 ms | ~1 MB | **Zero Docker** (Pre-compiled Go/Rust/C) |
| **Node.js** | `node`, `nodejs` | **Docker** (`node:slim`) | ~600 ms | ~35 MB | Docker or Podman daemon |
| **Java** | `java` | **Docker** (`eclipse-temurin:21`) | ~1.2 s | ~128 MB | Docker or Podman daemon |
| **Go** | `go`, `golang` | **Docker** (`golang:alpine`) | ~1.0 s | ~64 MB | Docker or Podman daemon |
| **Rust** | `rust`, `rs` | **Docker** (`rust:alpine`) | ~1.5 s | ~128 MB | Docker or Podman daemon |
| **Bash** | `bash`, `sh` | **Docker** (`alpine:latest`) | ~400 ms | ~10 MB | Docker or Podman daemon |

---

## Quickstart

### 1. Installation

```bash
go get github.com/micromax/sandbox
```

### 2. Run Code in Pure WebAssembly (Zero Docker)

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
	// Initialize pure-Go Wasm backend
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatal(err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		log.Fatal(err)
	}

	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `console.log("6 * 7 = " + (6 * 7));`,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s", res.Stdout)
}
```

---

## CLI Tool

Install the standalone CLI:

```bash
go install github.com/micromax/sandbox/cmd/sandbox@latest
```

```bash
# Check your environment and installed engines
sandbox doctor

# Run a script with memory and time caps
sandbox run --timeout 5s --mem 64M script.py

# Start an interactive REPL
sandbox repl --lang python

# Serve a guest web server
sandbox serve --lang js --port 8080 app.js
```

---

## Detailed Documentation

* 📖 **[Developer Guide](docs/guide.md)** — Complete step-by-step manual covering VFS, REPL sessions, network serving, and limits.
* 🛡️ **[Security Architecture & Threat Model](docs/security.md)** — Defense-in-depth isolation guarantees, memory page limits, and container hardening.
* 🌐 **[Network Policy & SSRF Guide](docs/network.md)** — Outbound allow-lists, pinned DNS resolution, and metadata endpoint blocking.
* 📦 **[Language Packs Reference](docs/languages.md)** — Per-runtime capabilities, startup metrics, and standard library support.
* 🚀 **[Serving Network Services](docs/serve.md)** — Reverse proxy design, loopback-by-default binding, and bearer token auth.
* 📊 **[Performance Benchmarks](docs/benchmarks.md)** — Startup latency and memory consumption data.

---

## License

MIT — see [LICENSE](LICENSE).

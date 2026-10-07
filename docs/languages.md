# Supported Languages & Packs

`github.com/micromax/sandbox` provides pluggable language packs supporting both the **pure-Go WebAssembly backend** (zero host dependencies, zero Docker) and the **hardened Docker backend** (universal language support).

---

## Language Support Matrix

| Language | Identifier(s) | Default Backend | Cold Start | Memory Floor | Capabilities & Notes |
|---|---|---|---|---|---|
| **JavaScript** | `js`, `javascript` | **Wasm** (QuickJS-NG) | ~50 ms | ~5 MB | Embedded in binary (`go:embed`, ~1.5 MB). Pure ECMAScript 2023. Fast interactive REPL. **Zero Docker**. |
| **TypeScript** | `ts`, `typescript` | **Wasm** (QuickJS-NG + Transpiler) | ~50 ms | ~5 MB | Strips types/interfaces/enums, executes on embedded QuickJS. **Zero Docker / Zero Node.js**. |
| **Python** | `python`, `py`, `python3` | **Wasm** (CPython 3.13) | ~250–350 ms | ~25 MB | Official CPython WASI build. Pure Python stdlib (`json`, `math`, `re`, etc.). On-demand download. **Zero Docker**. |
| **Lua** | `lua`, `lua54`, `luawasi` | **Wasm** (Lua 5.4 WASI) | ~20 ms | ~2 MB | Embedded in binary (`go:embed`, ~320 KB). Fast, lightweight scripting. **Zero Docker**. |
| **Raw Wasm** | `wasm`, `wasi`, `wasip1` | **Wasm** (wazero) | ~5–15 ms | ~1 MB | Directly executes precompiled WASI binaries (from **Go**, **Rust**, **Zig**, **C**, etc.). **Zero Docker**. |
| **Node.js** | `node`, `nodejs` | **Docker** (`node:slim`) | ~600 ms | ~35 MB | Full Node.js runtime with npm/npx, ES modules, CommonJS, and async event loop. |
| **Java** | `java` | **Docker** (`eclipse-temurin:21-jdk-alpine`) | ~1.2 s | ~128 MB | Full OpenJDK 21. Supports on-the-fly compilation (`javac`) and execution (`java`). |
| **Go** | `go`, `golang` | **Docker** (`golang:alpine`) | ~1.0 s | ~64 MB | Full Go toolchain. Compiles and executes code using `go run main.go`. (Or use `wasm` pack with `GOOS=wasip1`). |
| **Rust** | `rust`, `rs` | **Docker** (`rust:alpine`) | ~1.5 s | ~128 MB | Full Rust compiler (`rustc`). Direct compilation. (Or use `wasm` pack with `wasm32-wasip1`). |
| **Bash** | `bash`, `sh` | **Docker** (`alpine:latest`) | ~400 ms | ~10 MB | Standard POSIX shell and GNU utilities (`sed`, `awk`, `grep`). |

---

## 1. WebAssembly Tier Details (Zero Docker / Pure Go)

### JavaScript (`js`)
- **Runtime**: [QuickJS-NG](https://github.com/quickjs-ng/quickjs) compiled to WASI preview1.
- **Delivery**: Embedded directly into the Go binary (`go:embed`, ~1.5 MB).
- **Offline Capable**: Yes, requires zero network downloads or external binaries.
- **Language Spec**: Full ECMAScript 2023 support (async/await, generators, BigInt, RegExp).
- **Serving / HTTP**: Supported via M5 Handler Bridge (`fetch(req)` style handler).

### TypeScript (`ts`, `typescript`)
- **Runtime**: Embedded QuickJS-NG + pure Go transpiler (AST type stripping).
- **Delivery**: Zero external downloads, re-uses embedded QuickJS WASI binary.
- **Offline Capable**: Yes, instant startup (<50ms).
- **Language Spec**: Strips interfaces, type aliases, generic type parameters, return types, variable types, and transforms enums to constants before running on QuickJS.
- **Use Cases**: Running TypeScript snippets, algorithm challenges, and tooling directly in Go with zero Node.js installation and zero Docker daemon.
- **Serving / HTTP**: Supported via M5 Handler Bridge (`fetch(req)` style handler).

### Python (`python`)
- **Runtime**: Official CPython (v3.13.9) compiled to WebAssembly with WASI SDK.
- **Delivery**: Downloaded on first use into the user cache directory (`os.UserCacheDir()`), verified with SHA-256 (`f974d681...`), and cached content-addressed.
- **Offline Capable**: Yes, after initial download or via pre-seeding (`sandbox packs install`).
- **Standard Modules**: Core stdlib: `json`, `math`, `re`, `datetime`, `collections`, `itertools`, `hashlib`, `urllib.parse`, `string`, `random`, `csv`, `io`.
- **Serving / HTTP**: Supported via M5 Handler Bridge (WSGI callable `app(environ, start_response)`).
- **Restrictions**: C extensions requiring host compilation (like native `numpy` or `torch`) are not supported in WASI tier-1 (use Docker backend).

### Lua (`lua`)
- **Runtime**: Official Lua 5.4 compiled to WebAssembly (WASI).
- **Delivery**: Embedded directly into the Go binary (`go:embed`, ~320 KB).
- **Offline Capable**: Yes, zero downloads and instant startup (<25ms).
- **Standard Modules**: Full standard Lua 5.4 library (`math`, `string`, `table`, `io`).
- **Use Cases**: Ephemeral algorithms, evaluation scripts, tool executions, and game logic without Docker.

### Raw Wasm (`wasm`, `wasi`, `wasip1`)
- **Runtime**: Wazero WASI Preview 1 engine.
- **Delivery**: Supplied directly in `Spec.Code` or `Spec.Files["main.wasm"]`.
- **Enables Zero-Docker for Compiled Languages**:
  - **Go**: Compile with `GOOS=wasip1 GOARCH=wasm go build -o main.wasm main.go`
  - **Rust**: Compile with `cargo build --target wasm32-wasip1`
  - **Zig**: Compile with `zig build-exe -target wasm32-wasi main.zig`
  - **C/C++**: Compile with `clang --target=wasm32-wasi -o main.wasm main.c`
- **Isolation**: Runs under the exact same hard limits: memory page caps, CPU deadlines, output limits, and `/in`, `/work`, `/out` virtual filesystem.

---

## 2. Docker Tier Details (Optional / Universal)

For heavy or runtime-intensive languages where compilation takes place inside the guest container:

### Java (`java`)
- **Compiler**: `javac`
- **Execution**: `java Main`
- **Recommended Memory Limit**: $\ge 256\text{ MiB}$ (JVM heap + metaspace allocation).

### Go (`go`)
- **Execution**: `go run main.go`
- **Recommended Memory Limit**: $\ge 128\text{ MiB}$ (Go compiler memory budget).

### Rust (`rust`)
- **Compiler**: `rustc main.rs -o /work/main && /work/main`
- **Recommended Memory Limit**: $\ge 256\text{ MiB}$ (LLVM compilation budget).

### Node.js (`node`)
- **Execution**: `node /in/main.js`
- **Recommended Memory Limit**: $\ge 64\text{ MiB}$.

### Bash (`bash`)
- **Execution**: `/bin/sh -c <script>`
- **Recommended Memory Limit**: $\ge 16\text{ MiB}$.

---

## Virtual Filesystem Mapping Across Backends

Regardless of backend, all language packs mount a consistent virtual filesystem:
* `/in`: Read-only input files and scripts provided in `Spec.Files`.
* `/work`: Writable scratch space for intermediate compilation artifacts.
* `/out`: Writable output directory. Files created here are collected and returned in `Result.Files`.

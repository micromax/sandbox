# Supported Languages (Wasm Backend)

This document details the capabilities, execution model, and restrictions for each language runtime supported under the pure-Go WebAssembly (`wasm`) backend.

---

## 1. JavaScript (`js`, `javascript`, `node`)

- **Runtime**: [QuickJS-NG](https://github.com/quickjs-ng/quickjs) compiled to WebAssembly (WASI preview1).
- **Delivery**: Embedded directly into the Go binary (`go:embed`, ~1.5 MB).
- **Offline Capable**: Yes, requires zero network downloads or external binaries.
- **Language Spec**: Full ECMAScript 2023 support (including async/await, generators, BigInt, RegExp).

### Standard Utilities Available
- `console.log`, `console.error`, `console.warn`
- Global `std` object (`--std` mode):
  - `std.open(path, mode)`: file operations on `/in`, `/work`, `/out`
  - `std.getenv(name)`: inspect environment variables granted in `Spec.Env`
  - `std.loadFile(path)`: read entire file as string
- Global `os` object:
  - System primitives bounded to the WASI virtual machine.

### What is Restricted / Not Available
- **No Node.js native C++ addons (`.node`)**: Only pure JavaScript.
- **No Host Sockets**: Raw TCP/UDP network sockets are disallowed by WASI preview1. Outbound network requests go through the host HTTP bridge (Milestone M7).
- **No Arbitrary Subprocesses**: Child process spawning is disabled by the WebAssembly boundary.

---

## 2. Python (`python`, `py`, `python3`)

- **Runtime**: Official CPython (v3.13.9) compiled to WebAssembly with WASI SDK.
- **Delivery**: Downloaded on first use into the user cache directory (`os.UserCacheDir()`), verified with SHA-256 (`f974d681668b8a51bf548474c9999630fe83a316155a4e937eaff57ea3ea4f39`), and cached content-addressed.
- **Offline Capable**: Yes, after initial download or via pre-seeding (`sandbox packs install`).
- **Language Spec**: Python 3.13.

### Standard Modules Available
- Core stdlib: `json`, `math`, `re`, `datetime`, `collections`, `itertools`, `hashlib`, `urllib.parse`, `string`, `random`, `csv`, `io`, and more.
- Virtual filesystem access to `/in` (read-only), `/work` (scratch), `/out` (collected).

### What is Restricted / Not Available
- **No C-extension native libraries requiring host compilation**: Packages like native `numpy` or `torch` with native `.so`/`.dll` binaries are not supported in WASI tier-1 (use Docker backend for native toolchains).
- **No Host Subprocesses**: `os.system`, `subprocess.Popen` are disabled or restricted by WASI.
- **No Direct Host Sockets**: Network is controlled by `Spec.Net` policy.

---

## Summary Matrix

| Language | Backend | Startup Time | Memory Footprint | Network | File I/O |
|---|---|---|---|---|---|
| **JavaScript** | Wasm (QuickJS) | ~50 ms (warm) | ~5-15 MB | Host HTTP bridge (M7) | `/in`, `/work`, `/out` |
| **Python** | Wasm (CPython) | ~250-400 ms (warm) | ~25-45 MB | Host HTTP bridge (M7) | `/in`, `/work`, `/out` |

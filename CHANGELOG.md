# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/) (pre-1.0: minor versions may change
the API).

## [Unreleased]

## [0.3.0] - 2026-10-11

### Added
- Persistent wazero compilation cache (`~/.cache/micromax-sandbox/wazero-cache`)
  with `WithCompilationCacheDir`, `WithInMemoryCompilationCache` and
  `WithCompilationCache` options, plus a benchmark suite.
- Real-time output streaming: `Spec.StdoutWriter`, `StderrWriter`, `OnStdout`
  and `OnStderr`; `Sandbox.SSEHandler()` serving `start`/`stdout`/`stderr`/`done`/`error`
  events over Server-Sent Events; live `notifications/message` from the MCP
  `execute_code` tool; `sandbox run` streams to the terminal.
- Guest package management: `packages.Store` (offline, content-addressed
  cache), `packages.ExtractWheel` for pure-Python wheels into `/in/site-packages`,
  `Spec.Packages`, `ErrPackageNotFound`, and the `sandbox packages list|add`
  and `sandbox run --pkg` commands.
- Multi-file projects: `sandbox.LoadProjectFiles`, `sandbox run --dir`, and the
  MCP `files` and `project_dir` arguments.
- Documentation: `docs/projects-and-packages.md`.

## [0.2.0] - 2026-10-07

### Added
- Model Context Protocol server (`sandbox mcp`) exposing `execute_code`,
  `eval_session` and `list_languages`, plus OpenAI and Anthropic tool schemas.
- Runnable multi-language example scripts.

## [0.1.0] - 2026-10-07

First release.

### Added
- Core library: `Sandbox`, `Spec`, `Limits` (zero never means unlimited),
  `Pack`, routing `Policy` (`PreferWasm`, `WasmOnly`, `DockerOnly`) that never
  silently downgrades isolation, sentinel errors, and the quota-limited
  in-memory virtual filesystem (`/in`, `/work`, `/out`).
- Wasm backend on wazero (no CGO, no daemon): JavaScript (QuickJS-NG),
  TypeScript, Python (CPython 3.13 WASI), Lua 5.4 and raw WASI modules.
- Stateful REPL sessions and the session `Manager` (caps, idle and lifetime reaping).
- Hardened Docker backend with digest-pinned images: Node.js, Java, Go, Rust, Bash.
- `Serve`: host-side reverse proxy for guest servers (loopback by default,
  bearer-token auth, connection and body limits), readiness probes.
- Outbound network policy with allow-lists, SSRF guards, DNS pinning and
  redirect validation.
- `sandbox` CLI (`run`, `repl`, `serve`, `doctor`, `packs`) and a
  cross-platform release workflow.
- Linux-in-Wasm spike findings (`docs/spikes/linux-in-wasm.md`).

[Unreleased]: https://github.com/micromax/sandbox/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/micromax/sandbox/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/micromax/sandbox/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/micromax/sandbox/releases/tag/v0.1.0

# Security Policy

`github.com/micromax/sandbox` exists to run untrusted code safely, so security
reports are taken seriously and handled privately.

## Reporting a vulnerability

**Please do not open a public issue for a vulnerability.**

Use GitHub's private reporting: on the repository page open the **Security** tab
and choose **Report a vulnerability**. Include:

- the affected version or commit (`sandbox version`) and OS/architecture;
- the backend involved (Wasm or Docker) and the language pack;
- a minimal reproduction, ideally a guest program plus the `Spec`/`Limits` used;
- what you expected to be blocked and what actually happened.

A report is most useful when it shows guest code doing something the
[security documentation](docs/security.md) says it cannot do, for example:

- reading or writing host files, or escaping `/in`, `/work`, `/out`;
- exceeding a configured limit (memory, wall time, output, filesystem quota);
- reaching a network destination the `NetPolicy` should deny (private ranges,
  cloud metadata, DNS rebinding, redirects);
- bypassing proxy authentication or limits in `Serve`;
- a crash or hang of the host process triggered by guest-controlled input,
  including malformed archives, tar output and JSON-RPC/MCP requests.

## Supported versions

Security fixes are made against the latest release and `main`. Older releases
are not patched; please upgrade.

## Scope and known boundaries

The following are documented, accepted boundaries rather than vulnerabilities
(see [docs/security.md](docs/security.md) and
[docs/docker-security.md](docs/docker-security.md)):

- a guest may use 100% of one CPU core until its wall-clock deadline;
- microarchitectural side channels (timing, speculative execution) are out of
  scope for the Wasm backend;
- the Docker backend shares the host kernel, and access to the Docker socket is
  equivalent to root. For hostile multi-tenant workloads use rootless
  Docker/Podman, a gVisor (`runsc`) runtime, or the Wasm backend;
- bugs in the underlying runtimes (wazero, Docker, the Linux kernel) should be
  reported to those projects, though we want to know if this library's usage
  makes them exploitable.

If you are unsure whether something is in scope, report it privately anyway.

## Hardening checklist for embedders

- Prefer the Wasm backend (`sandbox.WasmOnly`) for untrusted code when the language allows.
- Keep the default deny-by-default `Spec`: no `Net`, no `Env`, explicit `Limits`.
- If you expose `SSEHandler`, `Serve`, or the MCP server to others, put your own
  authentication, request-size limits and rate limiting in front of it.
- Bind `Serve` to loopback (the default) and use bearer-token auth.
- Keep dependencies current (`govulncheck ./...` runs in CI).

# sandbox

> `github.com/micromax/sandbox` — run untrusted or AI-generated code in isolation, as a Go library.

**Status: early development (milestone M1 of 7 — core API, limits and virtual filesystem).**
There is no real isolation backend yet; do not use this to run untrusted code until M2 (Wasm backend) lands.

## Goals

- Embeddable in any Go project (`go get`), no daemon required for the default backend.
- Many languages as pluggable packs; Wasm by default, Docker optional.
- Hard limits on memory, time, output and disk. Deny-by-default network and filesystem.
- REPL sessions and serving a guest port on the host.

See the full plan in [docs/plan](docs/plan/README.md).

## Safety rules the core already enforces

- No network unless a policy grants it (a policy is rejected until a backend can enforce it).
- Zero-value limits take safe defaults; "unlimited" must be requested explicitly.
- Injected file names are validated; `..`, drive letters and NUL bytes are rejected.
- Output is capped so a guest cannot exhaust host memory.
- If the routing policy's backends cannot run a language, `Run` fails instead of silently using weaker isolation.

## Example

```go
sb, _ := sandbox.New(
    sandbox.WithBackends(/* backend */),
    sandbox.WithPacks(/* language packs */),
)
res, err := sb.Run(ctx, sandbox.Spec{Lang: "python", Code: "print(6*7)"})
```

See [examples/hello](examples/hello/main.go) for a runnable demo of the current API.

## License

MIT — see [LICENSE](LICENSE).

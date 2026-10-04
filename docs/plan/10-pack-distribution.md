# 10 — Pack Distribution: bringing runtimes to every language

This is the central practical challenge: how do the language runtimes reach the user's project safely and with a small footprint?

## Goals

- A user who needs only Python does not pay for Java, and vice versa.
- `go get` works with no extra steps for small runtimes.
- Large runtimes are verified before use, with offline support.
- Adding a language never changes the core API.

## Pack definition

```go
type Pack struct {
    Name    string
    Version string
    Wasm    *WasmSpec      // optional
    Docker  *DockerSpec    // optional
    L2W     *L2WSpec       // optional, experimental
    Caps    Capabilities   // Sessions, Serve(handler), Serve(socket), Network, Compile...
}

type WasmSpec struct {
    Artifact  Artifact        // where the .wasm / stdlib comes from
    Entry     string
    Args      []string
    Mounts    []Mount         // e.g. stdlib read-only
    SessionDriver string      // REPL driver script, if any
}

type DockerSpec struct {
    ImageDigest string        // e.g. "python@sha256:..."
    Cmd         []string
    Workdir     string
}

type Artifact struct {
    Name   string
    URL    string             // optional, for download
    SHA256 string
    Size   int64
    Embedded []byte           // set when go:embed is used
}
```

`Caps` lets the router and docs know exactly what each pack supports before running anything.

## Distribution strategies

| Strategy | Use for | Details |
|---|---|---|
| `go:embed` | Small runtimes (QuickJS ~1 MB, Lua) | Works offline, single binary, zero setup |
| Download + cache | Large Wasm runtimes (CPython ~20-30 MB, Ruby, PHP) | Fetched on first use into the cache dir, **SHA-256 verified before every use** |
| Docker image by digest | Java, Go, Rust, C#, Node, bash | Docker pulls and caches; we pin the digest so tags can't be swapped |
| Linux-in-Wasm image (experimental) | Anything, if M6 passes | Download + cache, verified |

### Cache

- Location: `os.UserCacheDir()/micromax-sandbox/` (configurable via `WithCacheDir`).
- Content-addressed: files stored by their SHA-256.
- Verification: hash after download **and** on load (cheap relative to the run; skippable only with an explicit unsafe option).
- Concurrent-safe: download to a temp file, verify, atomic rename; file lock prevents duplicate downloads.
- wazero **compilation cache** lives in the same area, so runtimes compile once.
- Offline: `sandbox packs install python` pre-populates; `WithOfflineOnly()` forbids network fetches and errors clearly if missing.
- Size policy: refuse downloads whose size differs from the manifest.

### Module layout (Go modules)

```
github.com/micromax/sandbox                    core + wasm backend + docker backend
github.com/micromax/sandbox/packs/js           (embedded QuickJS)
github.com/micromax/sandbox/packs/python       (download + cache)
github.com/micromax/sandbox/packs/ruby
github.com/micromax/sandbox/packs/java         (docker)
github.com/micromax/sandbox/packs/go           (docker)
...
```

Each pack under `packs/` is its **own Go module** (own `go.mod`), so importing `packs/python` does not pull other languages' artifacts into `go.sum` or the binary. Packs depend only on the core module's small `Pack` types.

Usage in a user project:

```go
import (
    "github.com/micromax/sandbox"
    "github.com/micromax/sandbox/packs/js"
    "github.com/micromax/sandbox/packs/python"
)

sb := sandbox.New(sandbox.WithPacks(js.Pack(), python.Pack()))
```

### Manifest

`packs/MANIFEST.json` lists every artifact and image: name, version, URL, SHA-256 or digest, size, license, and upstream source. It is the audited record of everything we execute. CI checks the manifest against actual files and re-verifies hashes.

## Language matrix (target)

| Language | Primary | Delivery | Sessions | Serve | Phase |
|---|---|---|---|---|---|
| JavaScript | Wasm (QuickJS) | embed | yes | handler | M2 |
| Python | Wasm (CPython WASI) | download | yes | handler (WSGI) | M2 |
| Bash/sh | Docker | image | partial | n/a | M4 |
| Node.js | Docker | image | yes | any server | M4 |
| Java | Docker | image | partial | any server | M4 |
| Go | Docker | image | no (re-run) | any server | M4 |
| Rust | Docker | image | no | any server | M4 |
| Ruby, Lua, PHP, SQL | Wasm | embed/download | varies | handler | after v0.1 |
| Anything else | Docker, or L2W if M6 passes | image | varies | any | later |

## Building and updating runtimes

A `tools/` directory holds reproducible scripts for building or fetching each runtime:

1. `tools/fetch-quickjs.*` — download a pinned release, verify its hash, copy into the pack.
2. `tools/fetch-cpython-wasi.*` — same for CPython.
3. `tools/pin-image.*` — resolve an image tag to its digest and update the manifest.
4. A CI job runs weekly and opens a PR if upstream publishes a newer pinned version, with the hostile-guest suite running on the PR.

## Execution steps

1. Define `Pack`, `Artifact` and `Capabilities` in the core (M1).
2. Implement `artifact/` download, cache, lock, verify (M2).
3. Create the multi-module layout and verify cross-module `go get` works with `replace` directives in development (M2).
4. Implement `sandbox packs list|install|verify` (M7).
5. Add the manifest checker to CI (M2 onward).
6. Add each new language as a pack following a checklist:
   - Capabilities declared honestly
   - Hostile corpus entries for that language
   - Docs entry in `docs/languages.md`
   - Manifest entry with hash/digest and license

## Exit criteria

- [ ] Importing only `packs/js` does not add Python artifacts to the build
- [ ] A tampered cached artifact is detected and rejected
- [ ] Offline mode works after `packs install`
- [ ] Concurrent first-use downloads don't corrupt the cache
- [ ] Adding a new pack requires zero changes to the core

## Risks

| Risk | Fallback |
|---|---|
| Upstream URLs disappear | Mirror artifacts in our own GitHub release assets |
| Multi-module release friction | Scripted tagging; start with fewer modules and split later if needed |
| License obligations of bundled runtimes | Track in the manifest; ship a `NOTICE` file |

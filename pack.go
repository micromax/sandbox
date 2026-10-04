package sandbox

import (
	"fmt"
	"regexp"
	"strings"
)

// Pack describes one language and how to run it on each backend. Packs are
// plain data: adding a language never changes the core API.
type Pack struct {
	// Name is the canonical lower-case language name, e.g. "python".
	Name string
	// Aliases are alternative names, e.g. "py". Lower-case.
	Aliases []string
	// Version is the language/runtime version the pack pins.
	Version string
	// Wasm describes how to run the language on the Wasm backend.
	Wasm *WasmSpec
	// Docker describes how to run the language on the Docker backend.
	Docker *DockerSpec
	// Extra carries specs for out-of-tree or experimental backends, keyed
	// by backend name.
	Extra map[string]any
	// Caps declares what this pack supports so callers and the router know
	// before running anything.
	Caps Capabilities
}

// Capabilities declares what a pack can do.
type Capabilities struct {
	// Sessions: supports REPL-style stateful sessions.
	Sessions bool
	// ServeHandler: can serve through the host-bridged handler model.
	ServeHandler bool
	// ServeSocket: can run servers that open their own sockets.
	ServeSocket bool
	// Network: supports a network policy.
	Network bool
	// Compiles: has a compile step before running.
	Compiles bool
}

// Artifact is a runtime file (a .wasm module, a standard library archive...)
// pinned by hash.
type Artifact struct {
	// Name is a human-readable file name.
	Name string
	// URL is where to download it from; empty when Embedded is set.
	URL string
	// SHA256 is the lower-case hex digest every use is verified against.
	SHA256 string
	// Size is the expected size in bytes; 0 when unknown.
	Size int64
	// Embedded holds the bytes when the artifact ships inside the binary.
	Embedded []byte
}

// Mount makes an artifact available inside the guest, read-only.
type Mount struct {
	Artifact  Artifact
	GuestPath string
}

// WasmSpec describes running a language as a WebAssembly module.
type WasmSpec struct {
	// Module is the interpreter or runtime .wasm.
	Module Artifact
	// Args are the fixed arguments passed to the module.
	Args []string
	// Mounts are extra read-only files (for example a standard library).
	Mounts []Mount
	// SessionDriver is the REPL driver script used for sessions, if any.
	SessionDriver string
}

// DockerSpec describes running a language in a container.
type DockerSpec struct {
	// Image is the image reference pinned by digest, e.g. "python@sha256:...".
	Image string
	// Cmd is the command to run.
	Cmd []string
	// Workdir is the working directory inside the container.
	Workdir string
}

var (
	packNameRe = regexp.MustCompile(`^[a-z][a-z0-9_+.-]{0,31}$`)
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Validate checks that the pack is well formed.
func (p *Pack) Validate() error {
	if p == nil {
		return fmt.Errorf("%w: nil pack", ErrInvalidPack)
	}
	if !packNameRe.MatchString(p.Name) {
		return fmt.Errorf("%w: name %q must match %s", ErrInvalidPack, p.Name, packNameRe)
	}
	for _, a := range p.Aliases {
		if !packNameRe.MatchString(a) {
			return fmt.Errorf("%w: alias %q must match %s", ErrInvalidPack, a, packNameRe)
		}
	}
	if p.Wasm == nil && p.Docker == nil && len(p.Extra) == 0 {
		return fmt.Errorf("%w: %q defines no backend spec", ErrInvalidPack, p.Name)
	}
	if p.Wasm != nil {
		if err := p.Wasm.Module.validate(); err != nil {
			return fmt.Errorf("%w: %q wasm module: %v", ErrInvalidPack, p.Name, err)
		}
		for _, m := range p.Wasm.Mounts {
			if err := m.Artifact.validate(); err != nil {
				return fmt.Errorf("%w: %q mount %q: %v", ErrInvalidPack, p.Name, m.GuestPath, err)
			}
		}
	}
	if p.Docker != nil {
		if !strings.Contains(p.Docker.Image, "@sha256:") {
			return fmt.Errorf("%w: %q docker image %q must be pinned by digest (name@sha256:...)",
				ErrInvalidPack, p.Name, p.Docker.Image)
		}
		if len(p.Docker.Cmd) == 0 {
			return fmt.Errorf("%w: %q docker spec has no command", ErrInvalidPack, p.Name)
		}
	}
	return nil
}

// validate enforces that every artifact is either embedded or downloadable
// with a pinned hash, so nothing unverified can ever be executed.
func (a Artifact) validate() error {
	switch {
	case len(a.Embedded) > 0:
		if a.SHA256 != "" && !sha256Re.MatchString(a.SHA256) {
			return fmt.Errorf("SHA256 %q is not 64 lower-case hex characters", a.SHA256)
		}
		return nil
	case a.URL != "":
		if !sha256Re.MatchString(a.SHA256) {
			return fmt.Errorf("downloadable artifact %q needs a pinned 64-character SHA256", a.Name)
		}
		return nil
	default:
		return fmt.Errorf("artifact %q has neither embedded bytes nor a URL", a.Name)
	}
}

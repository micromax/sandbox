// Package wasm provides a generic WebAssembly/WASI binary pack.
//
// It allows running arbitrary pre-compiled WASI binaries (compiled from Go,
// Rust, Zig, C, TinyGo, etc.) directly on the Wasm backend with zero Docker dependencies.
package wasm

import (
	"github.com/micromax/sandbox"
)

// Pack returns the generic WebAssembly binary pack.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "wasm",
		Aliases: []string{"wasi", "wasip1"},
		Version: "preview1",
		Wasm: &sandbox.WasmSpec{
			Module: sandbox.Artifact{
				Name: "custom.wasm",
			},
		},
		Caps: sandbox.Capabilities{
			Sessions: false,
		},
	}
}

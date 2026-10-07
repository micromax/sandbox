// Package ts provides the TypeScript language pack for the Wasm backend.
//
// TypeScript code is transpiled directly to JavaScript (by stripping type
// annotations, interfaces, type aliases, and enums) and executed on the embedded
// QuickJS WebAssembly engine without requiring Node.js or Docker.
package ts

import (
	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/packs/js"
)

// Pack returns the TypeScript language pack configured for the Wasm backend.
func Pack() *sandbox.Pack {
	jsPack := js.Pack()
	return &sandbox.Pack{
		Name:    "ts",
		Aliases: []string{"typescript"},
		Version: js.Version,
		Wasm: &sandbox.WasmSpec{
			Module:        jsPack.Wasm.Module,
			Args:          []string{"qjs", "--std"},
			SessionDriver: jsPack.Wasm.SessionDriver,
		},
		Caps: sandbox.Capabilities{
			Sessions:     true,
			ServeHandler: true,
		},
	}
}

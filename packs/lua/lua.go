// Package lua provides the Lua 5.4 language pack using a WASI-compiled Lua runtime.
//
// The runtime is embedded directly into the binary (~320 KB),
// enabling offline execution with zero external dependencies and zero Docker.
package lua

import (
	_ "embed"

	"github.com/micromax/sandbox"
)

//go:embed lua.wasm
var luaWasm []byte

const (
	// Version is the pinned Lua release version.
	Version = "5.4.6"
	// SHA256 is the verified checksum of the embedded Lua WASI binary.
	SHA256 = "02754c9822caf5112e9a2ccaec3dd29076bf37b51d65cb886ae1606389370c84"
)

// Pack returns the Lua language pack configured for the Wasm backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "lua",
		Aliases: []string{"lua54", "luawasi"},
		Version: Version,
		Wasm: &sandbox.WasmSpec{
			Module: sandbox.Artifact{
				Name:     "lua.wasm",
				SHA256:   SHA256,
				Embedded: luaWasm,
				Size:     int64(len(luaWasm)),
			},
			Args: []string{"lua"},
		},
		Caps: sandbox.Capabilities{
			Sessions: false,
		},
	}
}

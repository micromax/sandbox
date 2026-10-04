// Package js provides the JavaScript language pack using QuickJS compiled to WebAssembly (WASI).
//
// The QuickJS runtime is embedded directly into the binary (~1.5 MB),
// enabling offline execution with zero external dependencies.
package js

import (
	_ "embed"

	"github.com/micromax/sandbox"
)

//go:embed qjs-wasi.wasm
var qjsWasm []byte

//go:embed driver.js
var jsDriver string

const (
	// Version is the pinned QuickJS-NG release version.
	Version = "0.17.0"
	// SHA256 is the verified checksum of the embedded QuickJS WASI binary.
	SHA256 = "42a732a676ec2d93488c19411e0fad283bf72658fdad746f089914b523c783b1"
)

// Pack returns the JavaScript language pack configured for the Wasm backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "js",
		Aliases: []string{"javascript", "node"},
		Version: Version,
		Wasm: &sandbox.WasmSpec{
			Module: sandbox.Artifact{
				Name:     "qjs-wasi.wasm",
				SHA256:   SHA256,
				Embedded: qjsWasm,
				Size:     int64(len(qjsWasm)),
			},
			Args:          []string{"qjs", "--std"},
			SessionDriver: jsDriver,
		},
		Caps: sandbox.Capabilities{
			Sessions:     true,
			ServeHandler: true,
		},
	}
}

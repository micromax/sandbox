// Package python provides the Python language pack using CPython compiled to WebAssembly (WASI).
//
// The CPython runtime and standard library are fetched on demand from pinned
// release assets and verified via SHA-256 before execution.
package python

import (
	_ "embed"

	"github.com/micromax/sandbox"
)

//go:embed driver.py
var pythonDriver string

const (
	// Version is the pinned CPython release version.
	Version = "3.13.9"

	// ZipURL is the official WASI SDK build of CPython released by Python core developers.
	ZipURL = "https://github.com/brettcannon/cpython-wasi-build/releases/download/v3.13.9/python-3.13.9-wasi_sdk-24.zip"

	// ZipSHA256 is the verified SHA-256 digest of the release zip archive.
	ZipSHA256 = "f974d681668b8a51bf548474c9999630fe83a316155a4e937eaff57ea3ea4f39"

	// ZipSize is the exact size in bytes of the release zip archive.
	ZipSize = 13698807
)

// Pack returns the Python language pack configured for the Wasm backend.
func Pack() *sandbox.Pack {
	archive := sandbox.Artifact{
		Name:   "cpython-wasi-bundle.zip",
		URL:    ZipURL,
		SHA256: ZipSHA256,
		Size:   ZipSize,
	}

	moduleArtifact := archive
	moduleArtifact.Name = "python.wasm"
	moduleArtifact.ArchiveEntry = "python.wasm"

	return &sandbox.Pack{
		Name:    "python",
		Aliases: []string{"py", "python3"},
		Version: Version,
		Wasm: &sandbox.WasmSpec{
			Module: moduleArtifact,
			Args:   []string{"python.wasm", "-B"},
			Mounts: []sandbox.Mount{
				{
					Artifact:  archive,
					GuestPath: "lib",
				},
			},
			SessionDriver: pythonDriver,
		},
		Caps: sandbox.Capabilities{
			Sessions:     true,
			ServeHandler: true,
		},
	}
}

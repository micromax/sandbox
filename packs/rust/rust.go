// Package rust provides the Rust language pack for the Docker backend.
package rust

import "github.com/micromax/sandbox"

const (
	// Version is the Rust toolchain version used.
	Version = "1.85"
	// Image is the pinned Rust container image digest.
	Image = "rust@sha256:e3eb333dfd5413346b0e9a456bf193c4db21d34c1b979a4ec7076a9fbcaee50a"
)

// Pack returns the Rust pack configured for the Docker backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "rust",
		Aliases: []string{"rs"},
		Version: Version,
		Docker: &sandbox.DockerSpec{
			Image:   Image,
			Cmd:     []string{"sh", "-c", "rustc /work/main.rs -o /work/main && /work/main"},
			Workdir: "/work",
		},
		Caps: sandbox.Capabilities{
			Sessions: false,
			Compiles: true,
			Network:  true,
		},
	}
}

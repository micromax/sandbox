// Package rust provides the Rust language pack for the Docker backend.
package rust

import "github.com/micromax/sandbox"

const (
	// Version is the Rust toolchain version used.
	Version = "1.85"
	// Image is the pinned Rust container image digest.
	Image = "rust:alpine@sha256:a96ea6d18d4062e38f16cfbadd8b4541d622f2527dd0a5eca1fb36d301da4e88"
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

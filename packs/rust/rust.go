// Package rust provides the Rust language pack for the Docker backend.
package rust

import "github.com/micromax/sandbox"

const (
	// Version is the Rust toolchain version used.
	Version = "1.85"
	// Image is the pinned Rust container image digest.
	Image = "rust:trixie@sha256:5d05167b28cef0fa3a6c781cd77949386848191f3382e82cf53bd1277a47a98f"
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

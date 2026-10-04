// Package golang provides the Go language pack for the Docker backend.
package golang

import "github.com/micromax/sandbox"

const (
	// Version is the Go toolchain version used.
	Version = "1.24"
	// Image is the pinned Golang alpine container image digest.
	Image = "golang@sha256:2d40d4fc278dad38be0777d5e2e88a2c6eb511089201524e9305ec0b37060372"
)

// Pack returns the Go pack configured for the Docker backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "go",
		Aliases: []string{"golang"},
		Version: Version,
		Docker: &sandbox.DockerSpec{
			Image:   Image,
			Cmd:     []string{"go", "run", "/work/main.go"},
			Workdir: "/work",
		},
		Caps: sandbox.Capabilities{
			Sessions: false,
			Compiles: true,
			Network:  true,
		},
	}
}

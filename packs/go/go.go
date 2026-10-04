// Package golang provides the Go language pack for the Docker backend.
package golang

import "github.com/micromax/sandbox"

const (
	// Version is the Go toolchain version used.
	Version = "1.24"
	// Image is the pinned Golang alpine container image digest.
	Image = "golang:1.24-alpine@sha256:8bee1901f1e530bfb4a7850aa7a479d17ae3a18beb6e09064ed54cfd245b7191"
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

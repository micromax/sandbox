// Package bash provides the Bash/Shell language pack for the Docker backend.
package bash

import "github.com/micromax/sandbox"

const (
	// Version is the Alpine version used.
	Version = "3.21"
	// Image is the pinned Alpine container image digest.
	Image = "alpine@sha256:56fa17d2a7e7f168a043a2712e63aed1f8543aeafdcee47c58dcffe38ed51099"
)

// Pack returns the Bash/Shell pack configured for the Docker backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "bash",
		Aliases: []string{"sh", "shell"},
		Version: Version,
		Docker: &sandbox.DockerSpec{
			Image:   Image,
			Cmd:     []string{"sh", "/work/script.sh"},
			Workdir: "/work",
		},
		Caps: sandbox.Capabilities{
			Sessions: false,
			Network:  true,
		},
	}
}

// Package node provides the Node.js language pack for the Docker backend.
package node

import "github.com/micromax/sandbox"

const (
	// Version is the Node.js version used.
	Version = "22"
	// Image is the pinned Node.js container image digest.
	Image = "node:22-alpine@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402"
)

// Pack returns the Node.js pack configured for the Docker backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "node",
		Aliases: []string{"nodejs"},
		Version: Version,
		Docker: &sandbox.DockerSpec{
			Image:   Image,
			Cmd:     []string{"node", "/work/index.js"},
			Workdir: "/work",
		},
		Caps: sandbox.Capabilities{
			Sessions:     false,
			ServeHandler: true,
			Network:      true,
		},
	}
}

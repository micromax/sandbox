// Package node provides the Node.js language pack for the Docker backend.
package node

import "github.com/micromax/sandbox"

const (
	// Version is the Node.js version used.
	Version = "22"
	// Image is the pinned Node.js container image digest.
	Image = "node@sha256:d541571217e1329c323f46f5647a98eb107e0c4b2d56d946571fa08de7b09594"
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

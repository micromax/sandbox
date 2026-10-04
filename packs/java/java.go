// Package java provides the Java language pack for the Docker backend.
package java

import "github.com/micromax/sandbox"

const (
	// Version is the Java JDK LTS version used.
	Version = "21"
	// Image is the pinned Eclipse Temurin OpenJDK container image digest.
	Image = "eclipse-temurin@sha256:f765c6e3c2d67aad0a588ebe7e932a23eaec37e8485097979c3d497a796e60fb"
)

// Pack returns the Java language pack configured for the Docker backend.
func Pack() *sandbox.Pack {
	return &sandbox.Pack{
		Name:    "java",
		Aliases: []string{"jvm", "jdk"},
		Version: Version,
		Docker: &sandbox.DockerSpec{
			Image:   Image,
			Cmd:     []string{"sh", "-c", "javac /work/Main.java && java -cp /work Main"},
			Workdir: "/work",
		},
		Caps: sandbox.Capabilities{
			Sessions: false,
			Compiles: true,
			Network:  true,
		},
	}
}

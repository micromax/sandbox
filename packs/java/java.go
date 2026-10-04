// Package java provides the Java language pack for the Docker backend.
package java

import "github.com/micromax/sandbox"

const (
	// Version is the Java JDK LTS version used.
	Version = "21"
	// Image is the pinned Eclipse Temurin OpenJDK 21 compiler container image digest.
	Image = "eclipse-temurin:21-jdk-alpine@sha256:0bfc69a4758a86710e5c474032d28400a8bd00874766f9e8b1642ac2fd293159"
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

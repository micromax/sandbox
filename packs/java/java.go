// Package java provides the Java language pack for the Docker backend.
package java

import "github.com/micromax/sandbox"

const (
	// Version is the Java JDK LTS version used.
	Version = "21"
	// Image is the pinned Eclipse Temurin OpenJDK container image digest.
	Image = "eclipse-temurin@sha256:69b0fa630eb9df28e3b3c3b0eb6190538fc1008d5162a046c8e39f706fa8251e"
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

package docker_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/docker"
	"github.com/micromax/sandbox/packs/bash"
	"github.com/micromax/sandbox/packs/go"
	"github.com/micromax/sandbox/packs/java"
	"github.com/micromax/sandbox/packs/js"
	"github.com/micromax/sandbox/packs/node"
	"github.com/micromax/sandbox/packs/rust"
)

func TestDockerPacksValidation(t *testing.T) {
	packs := []*sandbox.Pack{
		bash.Pack(),
		node.Pack(),
		java.Pack(),
		golang.Pack(),
		rust.Pack(),
	}

	for _, p := range packs {
		if err := p.Validate(); err != nil {
			t.Errorf("pack %s failed validation: %v", p.Name, err)
		}
		if p.Docker == nil {
			t.Errorf("pack %s has nil DockerSpec", p.Name)
		}
		if !strings.Contains(p.Docker.Image, "@sha256:") {
			t.Errorf("pack %s image %s not pinned by digest", p.Name, p.Docker.Image)
		}
	}
}

func TestDockerBackendUnavailableWhenDaemonDown(t *testing.T) {
	// Point to an invalid socket path
	dBackend, err := docker.New(docker.WithSocket("unix:///tmp/nonexistent-docker.sock"))
	if err != nil {
		t.Fatalf("docker.New should not error at creation: %v", err)
	}
	defer dBackend.Close()

	if dBackend.Name() != "docker" {
		t.Fatalf("expected name 'docker', got: %s", dBackend.Name())
	}

	bashPack := bash.Pack()
	if !dBackend.Supports(bashPack) {
		t.Fatal("expected dBackend to support bash pack")
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(dBackend),
		sandbox.WithPacks(bashPack),
		sandbox.WithPolicy(sandbox.DockerOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	_, err = sb.Run(context.Background(), sandbox.Spec{
		Lang: "bash",
		Code: `echo "hello"`,
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("expected ErrBackendUnavailable when daemon unreachable, got: %v", err)
	}
	if !strings.Contains(err.Error(), "docker daemon is not reachable") {
		t.Fatalf("expected actionable error message, got: %v", err)
	}
}

func TestRoutingPolicyPrefersWasmOverDocker(t *testing.T) {
	dBackend, err := docker.New(docker.WithSocket("unix:///tmp/nonexistent-docker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer dBackend.Close()

	// Register both fake wasm-capable and docker
	jsPack := js.Pack()
	sb, err := sandbox.New(
		sandbox.WithBackends(dBackend),
		sandbox.WithPacks(jsPack),
		sandbox.WithPolicy(sandbox.PreferWasm),
	)
	if err != nil {
		t.Fatal(err)
	}

	// js only has wasm spec; dBackend doesn't support it -> should fail with ErrBackendUnavailable
	// without crashing
	_, err = sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: "1+1",
	})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("expected ErrBackendUnavailable, got: %v", err)
	}
}

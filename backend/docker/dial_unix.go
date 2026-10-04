//go:build !windows

package docker

import (
	"context"
	"net"
	"os"
)

func defaultSocketCandidates() []string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return []string{h}
	}
	var candidates []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		podman := "unix://" + xdg + "/podman/podman.sock"
		candidates = append(candidates, podman)
	}
	candidates = append(candidates, "unix:///var/run/docker.sock")
	return candidates
}

func dialContext(ctx context.Context, proto, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, proto, addr)
}

//go:build windows

package docker

import (
	"context"
	"net"
	"os"
	"strings"

	winio "github.com/Microsoft/go-winio"
)

func defaultSocketCandidates() []string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return []string{h}
	}
	return []string{
		`npipe:////./pipe/docker_engine`,
		`npipe:////./pipe/dockerDesktopLinuxEngine`,
	}
}

func dialContext(ctx context.Context, proto, addr string) (net.Conn, error) {
	if proto == "npipe" || strings.HasPrefix(addr, `//./pipe/`) || strings.HasPrefix(addr, `\\.\pipe\`) {
		pipePath := addr
		if proto == "npipe" {
			pipePath = strings.TrimPrefix(addr, "npipe://")
		}
		pipePath = strings.ReplaceAll(pipePath, "/", `\`)
		return winio.DialPipeContext(ctx, pipePath)
	}
	var d net.Dialer
	return d.DialContext(ctx, proto, addr)
}

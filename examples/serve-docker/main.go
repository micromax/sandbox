// Command serve-docker demonstrates running a server in a hardened Docker container
// and exposing it safely to the host through the sandbox proxy.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/docker"
	"github.com/micromax/sandbox/packs/node"
	"github.com/micromax/sandbox/packs/python"
)

func main() {
	dBackend, err := docker.New()
	if err != nil {
		log.Fatalf("failed to initialize docker backend: %v", err)
	}
	defer dBackend.Close()

	sb, err := sandbox.New(
		sandbox.WithBackends(dBackend),
		sandbox.WithPacks(node.Pack(), python.Pack()),
		sandbox.WithPolicy(sandbox.DockerOnly),
	)
	if err != nil {
		log.Fatalf("failed to initialize sandbox: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// A simple Node.js HTTP server listening on port 3000
	nodeCode := `
const http = require('http');

const server = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
        message: 'Hello from Node.js server inside Docker!',
        url: req.url,
        pid: process.pid
    }));
});

server.listen(3000, '0.0.0.0', () => {
    console.log('Server running on port 3000');
});
`

	fmt.Println("Starting Node.js server in hardened container...")
	svc, err := sb.Serve(ctx, sandbox.Spec{
		Lang: "node",
		Code: nodeCode,
	}, sandbox.ServeOpts{
		Ports: []sandbox.PortMap{{Guest: 3000}},
		Bind:  sandbox.Loopback,
		Ready: sandbox.TCPReady(15 * time.Second),
		TTL:   5 * time.Minute,
	})
	if err != nil {
		log.Fatalf("failed to start serve container (make sure Docker daemon is running): %v", err)
	}
	defer svc.Stop()

	fmt.Printf("Service listening at %s\n", svc.URL())

	resp, err := http.Get(svc.URL() + "/test")
	if err != nil {
		log.Fatalf("GET request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response (%d):\n%s\n", resp.StatusCode, string(body))

	_ = svc.Stop()
	<-svc.Done()
	fmt.Println("Docker service stopped.")
}

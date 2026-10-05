// Command serve-js demonstrates exposing a JavaScript service via Wasm handler bridge.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
)

func main() {
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatalf("failed to initialize wasm backend: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		log.Fatalf("failed to initialize sandbox: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jsCode := `
function fetch(req) {
    if (req.path === "/health") {
        return { status: 200, body: "healthy" };
    }
    return {
        status: 200,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
            message: "Hello from sandboxed JavaScript!",
            time: new Date().toISOString(),
            method: req.method,
            path: req.path
        })
    };
}
`

	token := "secret-demo-token"
	fmt.Println("Starting Wasm JavaScript service with Bearer auth...")

	svc, err := sb.Serve(ctx, sandbox.Spec{
		Lang: "js",
		Code: jsCode,
	}, sandbox.ServeOpts{
		Ports: []sandbox.PortMap{{Guest: 8080}},
		Bind:  sandbox.Loopback,
		Auth:  sandbox.BearerToken(token),
		Ready: sandbox.HTTPReady("/health", 5*time.Second),
		TTL:   5 * time.Minute,
	})
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
	defer svc.Stop()

	fmt.Printf("Service listening at %s\n", svc.URL())
	fmt.Printf("Token: %s\n\n", svc.Token())

	// 1. Unauthorized request
	fmt.Println("1. Attempting request without token...")
	resp, err := http.Get(svc.URL() + "/")
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	fmt.Printf("   Response status: %d (expected 401)\n\n", resp.StatusCode)
	resp.Body.Close()

	// 2. Authorized request
	fmt.Println("2. Sending request with Bearer authorization...")
	req, _ := http.NewRequest(http.MethodGet, svc.URL()+"/api/data", nil)
	req.Header.Set("Authorization", "Bearer "+svc.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("   Response status: %d\n", resp.StatusCode)
	fmt.Printf("   Response body:\n%s\n\n", string(body))

	fmt.Println("Stopping service...")
	_ = svc.Stop()
	<-svc.Done()
	fmt.Println("Service stopped cleanly.")
}

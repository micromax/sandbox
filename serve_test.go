package sandbox_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
)

func TestServe_WasmJSHandler(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New: %v", err)
	}

	jsPack := js.Pack()
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(jsPack),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	jsCode := `
function fetch(req) {
    if (req.path === "/json") {
        return {
            status: 200,
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ message: "hello json", method: req.method })
        };
    }
    return {
        status: 200,
        headers: { "Content-Type": "text/plain" },
        body: "hello from wasm js service"
    };
}
`

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	svc, err := sb.Serve(ctx, sandbox.Spec{
		Lang: "js",
		Code: jsCode,
	}, sandbox.ServeOpts{
		Ports: []sandbox.PortMap{{Guest: 8080}},
		Bind:  sandbox.Loopback,
		Auth:  sandbox.BearerToken("test-token-123"),
		Ready: sandbox.HTTPReady("/", 5*time.Second),
	})
	if err != nil {
		t.Fatalf("sb.Serve: %v", err)
	}
	defer svc.Stop()

	if svc.URL() == "" {
		t.Fatal("expected non-empty svc.URL()")
	}
	if svc.Token() != "test-token-123" {
		t.Fatalf("expected token 'test-token-123', got %q", svc.Token())
	}

	// 1. Request without auth -> 401
	resp, err := http.Get(svc.URL() + "/")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}

	// 2. Request with valid auth -> 200
	req, _ := http.NewRequest(http.MethodGet, svc.URL()+"/", nil)
	req.Header.Set("Authorization", "Bearer "+svc.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello from wasm js service" {
		t.Fatalf("unexpected body: %q", string(body))
	}

	// 3. Request path /json -> 200 with JSON response
	req, _ = http.NewRequest(http.MethodPost, svc.URL()+"/json", strings.NewReader(`{"client":"test"}`))
	req.Header.Set("Authorization", "Bearer "+svc.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /json: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
	jsonBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(jsonBody), "hello json") || !strings.Contains(string(jsonBody), "POST") {
		t.Fatalf("unexpected json body: %q", string(jsonBody))
	}

	// 4. Stop service
	if err := svc.Stop(); err != nil {
		t.Fatalf("svc.Stop: %v", err)
	}

	select {
	case <-svc.Done():
		// Expected
	case <-time.After(3 * time.Second):
		t.Fatal("svc.Done() did not fire after Stop()")
	}
}

func TestServe_TTL(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New: %v", err)
	}

	jsPack := js.Pack()
	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(jsPack),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	svc, err := sb.Serve(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `function fetch(req) { return "ok"; }`,
	}, sandbox.ServeOpts{
		Ports: []sandbox.PortMap{{Guest: 8000}},
		TTL:   300 * time.Millisecond,
		Ready: sandbox.TCPReady(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("sb.Serve: %v", err)
	}
	defer svc.Stop()

	select {
	case <-svc.Done():
		if !errors.Is(svc.Err(), sandbox.ErrTimeout) {
			t.Fatalf("expected ErrTimeout upon TTL expiry, got: %v", svc.Err())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for TTL expiry")
	}
}

func TestServe_UnsupportedLanguage(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("wasm.New: %v", err)
	}

	// Pack with no serve capabilities
	dummyPack := &sandbox.Pack{
		Name:    "dummy",
		Version: "1.0",
		Wasm: &sandbox.WasmSpec{
			Module: sandbox.Artifact{
				Name:     "dummy.wasm",
				SHA256:   "4e50eb198895066922b047daec72566ecb97c02c676d63495f5ab480397500fa",
				Embedded: []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
			},
		},
		Caps: sandbox.Capabilities{
			ServeHandler: false,
		},
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(dummyPack),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	_, err = sb.Serve(context.Background(), sandbox.Spec{
		Lang: "dummy",
		Code: "xyz",
	}, sandbox.ServeOpts{})
	if !errors.Is(err, sandbox.ErrUnsupported) && !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("expected ErrUnsupported or ErrBackendUnavailable, got: %v", err)
	}
}

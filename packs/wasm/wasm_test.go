package wasm_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	backendwasm "github.com/micromax/sandbox/backend/wasm"
	packwasm "github.com/micromax/sandbox/packs/wasm"
)

func TestRawWasmPack(t *testing.T) {
	wBackend, err := backendwasm.New()
	if err != nil {
		t.Fatalf("failed to create wasm backend: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(packwasm.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the embedded lua.wasm as a test WASI binary
	luaPath := filepath.Join("..", "lua", "lua.wasm")
	wasmBytes, err := os.ReadFile(luaPath)
	if err != nil {
		t.Fatalf("reading test wasm binary: %v", err)
	}

	t.Run("execute via Files[main.wasm]", func(t *testing.T) {
		res, err := sb.Run(ctx, sandbox.Spec{
			Lang: "wasm",
			Files: map[string][]byte{
				"main.wasm": wasmBytes,
			},
			Args: []string{"-e", "print('hello from raw wasm execution!')"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d, stderr: %s", res.ExitCode, res.Stderr)
		}
		out := strings.TrimSpace(string(res.Stdout))
		if out != "hello from raw wasm execution!" {
			t.Errorf("got %q, want %q", out, "hello from raw wasm execution!")
		}
	})

	t.Run("execute via Code string prefix", func(t *testing.T) {
		res, err := sb.Run(ctx, sandbox.Spec{
			Lang: "wasm",
			Code: string(wasmBytes),
			Args: []string{"-e", "print(99 + 1)"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d, stderr: %s", res.ExitCode, res.Stderr)
		}
		out := strings.TrimSpace(string(res.Stdout))
		if out != "100" {
			t.Errorf("got %q, want %q", out, "100")
		}
	})
}

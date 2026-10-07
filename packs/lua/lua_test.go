package lua_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/lua"
)

func TestLuaPack(t *testing.T) {
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("failed to create wasm backend: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(lua.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("basic arithmetic", func(t *testing.T) {
		res, err := sb.Run(ctx, sandbox.Spec{
			Lang: "lua",
			Code: `print("result=" .. (6 * 7))`,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Fatalf("unexpected exit code: %d, stderr: %s", res.ExitCode, res.Stderr)
		}
		out := strings.TrimSpace(string(res.Stdout))
		if out != "result=42" {
			t.Errorf("got %q, want %q", out, "result=42")
		}
	})

	t.Run("fibonacci loop", func(t *testing.T) {
		code := `
function fib(n)
    if n <= 1 then return n end
    return fib(n-1) + fib(n-2)
end
print(fib(10))
`
		res, err := sb.Run(ctx, sandbox.Spec{
			Lang: "lua",
			Code: code,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := strings.TrimSpace(string(res.Stdout))
		if out != "55" {
			t.Errorf("got %q, want %q", out, "55")
		}
	})
}

package ts_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	backendwasm "github.com/micromax/sandbox/backend/wasm"
	packts "github.com/micromax/sandbox/packs/ts"
)

func TestTSPack(t *testing.T) {
	p := packts.Pack()
	if err := p.Validate(); err != nil {
		t.Fatalf("ts.Pack() failed validation: %v", err)
	}
	if p.Name != "ts" {
		t.Fatalf("unexpected name: %s", p.Name)
	}
	if len(p.Wasm.Module.Embedded) == 0 {
		t.Fatal("embedded wasm binary is empty")
	}
}

func TestTSExecution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wBackend, err := backendwasm.New()
	if err != nil {
		t.Fatalf("backendwasm.New: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(packts.Pack()),
	)
	if err != nil {
		t.Fatalf("sandbox.New: %v", err)
	}

	tsCode := `
interface Greeter {
	prefix: string;
}

enum Level {
	Low = 1,
	High = 2,
}

function greet(name: string, level: Level): string {
	return "Level " + level + ": Hello, " + name + "!";
}

console.log(greet("Antigravity", Level.High));
`

	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: "ts",
		Code: tsCode,
	})

	if err != nil {
		t.Fatalf("sb.Run failed: %v, stderr: %s", err, string(res.Stderr))
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code %d, stderr: %s", res.ExitCode, string(res.Stderr))
	}

	out := string(res.Stdout)
	expected := "Level 2: Hello, Antigravity!"
	if !strings.Contains(out, expected) {
		t.Fatalf("expected %q in output, got: %q", expected, out)
	}
}

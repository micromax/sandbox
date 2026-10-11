package benchmarks_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/js"
	"github.com/micromax/sandbox/packs/lua"
)

func BenchmarkQuickJSRun(b *testing.B) {
	wBackend, err := wasm.New()
	if err != nil {
		b.Fatalf("failed to create wasm backend: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		b.Fatalf("failed to create sandbox: %v", err)
	}

	ctx := context.Background()
	spec := sandbox.Spec{
		Lang: "js",
		Code: `const x = 1 + 2;`,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, err := sb.Run(ctx, spec)
		if err != nil {
			b.Fatalf("Run failed: %v", err)
		}
		if res.ExitCode != 0 {
			b.Fatalf("non-zero exit code: %d", res.ExitCode)
		}
	}
}

func BenchmarkLuaRun(b *testing.B) {
	wBackend, err := wasm.New()
	if err != nil {
		b.Fatalf("failed to create wasm backend: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(lua.Pack()),
	)
	if err != nil {
		b.Fatalf("failed to create sandbox: %v", err)
	}

	ctx := context.Background()
	spec := sandbox.Spec{
		Lang: "lua",
		Code: `local x = 1 + 2`,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, err := sb.Run(ctx, spec)
		if err != nil {
			b.Fatalf("Run failed: %v", err)
		}
		if res.ExitCode != 0 {
			b.Fatalf("non-zero exit code: %d", res.ExitCode)
		}
	}
}

func BenchmarkConcurrentQuickJS(b *testing.B) {
	wBackend, err := wasm.New()
	if err != nil {
		b.Fatalf("failed to create wasm backend: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		b.Fatalf("failed to create sandbox: %v", err)
	}

	ctx := context.Background()
	concurrency := 8

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for c := 0; c < concurrency; c++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				spec := sandbox.Spec{
					Lang: "js",
					Code: fmt.Sprintf("const id = %d; const res = id * 2;", id),
				}
				res, err := sb.Run(ctx, spec)
				if err != nil || res.ExitCode != 0 {
					b.Errorf("concurrent run failed: %v", err)
				}
			}(c)
		}
		wg.Wait()
	}
}

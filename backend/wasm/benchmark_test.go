package wasm_test

import (
	"context"
	"testing"

	"github.com/micromax/sandbox"
)

func BenchmarkJSRun(b *testing.B) {
	sb := newTestSandbox(&testing.T{})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sb.Run(ctx, sandbox.Spec{
			Lang: "js",
			Code: `const x = 20 + 22;`,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPythonRun(b *testing.B) {
	sb := newTestSandbox(&testing.T{})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := sb.Run(ctx, sandbox.Spec{
			Lang: "python",
			Code: `x = 20 + 22`,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Command run-js demonstrates running untrusted JavaScript inside the Wasm sandbox.
package main

import (
	"context"
	"fmt"
	"log"
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

	ctx := context.Background()

	// 1. Basic calculation
	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: "js",
		Code: `
console.log("Fibonacci numbers:");
function fib(n) { return n <= 1 ? n : fib(n - 1) + fib(n - 2); }
for (let i = 0; i < 10; i++) {
    console.log("fib(" + i + ") = " + fib(i));
}
`,
	})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}
	fmt.Printf("--- Output ---\n%s\n", res.Stdout)
	fmt.Printf("Duration: %v | Backend: %s\n", res.Usage.Wall, res.Backend)

	// 2. File generation into /out
	res, err = sb.Run(ctx, sandbox.Spec{
		Lang: "js",
		Code: `
const out = std.open('/out/summary.json', 'w');
out.puts(JSON.stringify({ status: "success", timestamp: Date.now() }));
out.close();
console.log("File generated in /out/summary.json");
`,
	})
	if err != nil {
		log.Fatalf("file output error: %v", err)
	}
	fmt.Printf("\nGenerated files: %v\n", len(res.Files))
	for name, content := range res.Files {
		fmt.Printf("  %s (%d bytes): %s\n", name, len(content), string(content))
	}

	// 3. Enforcing limits against infinite loop
	fmt.Printf("\nRunning infinite loop with 200ms limit...\n")
	_, err = sb.Run(ctx, sandbox.Spec{
		Lang: "js",
		Code: `while(true) {}`,
		Limits: &sandbox.Limits{
			WallTime: 200 * time.Millisecond,
		},
	})
	fmt.Printf("Sandbox protected host: %v\n", err)
}

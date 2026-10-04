// Command run-python demonstrates running untrusted Python inside the Wasm sandbox.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/python"
)

func main() {
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatalf("failed to initialize wasm backend: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(python.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		log.Fatalf("failed to initialize sandbox: %v", err)
	}

	ctx := context.Background()

	// 1. Python calculation and standard library
	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: "python",
		Code: `
import json, math

data = {
    "title": "Python WASI Sandbox",
    "pi": math.pi,
    "factors": [x for x in range(1, 20) if 20 % x == 0],
}
print(json.dumps(data, indent=2))
`,
	})
	if err != nil {
		log.Fatalf("python run error: %v, stderr: %s", err, res.Stderr)
	}
	fmt.Printf("--- Output ---\n%s\n", res.Stdout)
	fmt.Printf("Duration: %v | Backend: %s\n", res.Usage.Wall, res.Backend)

	// 2. Processing injected files and producing output files
	res, err = sb.Run(ctx, sandbox.Spec{
		Lang: "python",
		Code: `
with open('/in/data.txt', 'r') as f:
    words = f.read().split()

with open('/out/wordcount.txt', 'w') as f:
    f.write(f"Total words: {len(words)}\n")

print(f"Processed {len(words)} words successfully.")
`,
		Files: map[string][]byte{
			"data.txt": []byte("The quick brown fox jumps over the lazy dog in the sandbox"),
		},
	})
	if err != nil {
		log.Fatalf("python file processing error: %v, stderr: %s", err, res.Stderr)
	}
	fmt.Printf("\nGenerated files:\n")
	for name, content := range res.Files {
		fmt.Printf("  %s: %s", name, string(content))
	}

	// 3. Resource limit protection
	fmt.Printf("\nEnforcing 100ms timeout on runaway Python loop...\n")
	_, err = sb.Run(ctx, sandbox.Spec{
		Lang: "python",
		Code: `
while True:
    pass
`,
		Limits: &sandbox.Limits{
			WallTime: 100 * time.Millisecond,
		},
	})
	fmt.Printf("Sandbox protected host: %v\n", err)
}

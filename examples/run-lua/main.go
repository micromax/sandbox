// Command run-lua demonstrates running untrusted Lua inside the Wasm sandbox with zero Docker.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/lua"
)

func main() {
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatalf("failed to initialize wasm backend: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(lua.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		log.Fatalf("failed to initialize sandbox: %v", err)
	}

	ctx := context.Background()

	code := `
print("=== Lua Standard Libraries Demo ===")
local sum = 0
for i = 1, 100 do
    sum = sum + i
end
print("Sum of 1..100 = " .. sum)
print("Math.pi       = " .. math.pi)
print("Math.sqrt(144)= " .. math.sqrt(144))
`

	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: "lua",
		Code: code,
	})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}

	fmt.Printf("--- Output ---\n%s\n", res.Stdout)
	fmt.Printf("Duration: %v | Backend: %s\n", res.Usage.Wall, res.Backend)
}

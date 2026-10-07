// Command run-ts demonstrates running untrusted TypeScript inside the Wasm sandbox without Node.js or Docker.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packs/ts"
)

func main() {
	wBackend, err := wasm.New()
	if err != nil {
		log.Fatalf("failed to initialize wasm backend: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(ts.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		log.Fatalf("failed to initialize sandbox: %v", err)
	}

	ctx := context.Background()

	code := `
interface Product {
    id: number;
    name: string;
    price: number;
}

enum Category {
    Hardware = "HARDWARE",
    Software = "SOFTWARE",
}

function calculateTotal<T extends Product>(items: T[], discount: number = 0): number {
    const rawTotal = items.reduce((sum, item) => sum + item.price, 0);
    return rawTotal * (1 - discount);
}

const inventory: Product[] = [
    { id: 1, name: "WebAssembly Runtime", price: 100 },
    { id: 2, name: "Sandbox Security Guard", price: 50 },
];

console.log("Category:", Category.Software);
console.log("Total (10% off): $" + calculateTotal(inventory, 0.10));
`

	res, err := sb.Run(ctx, sandbox.Spec{
		Lang: "ts",
		Code: code,
	})
	if err != nil {
		log.Fatalf("execution error: %v", err)
	}

	fmt.Printf("--- Output ---\n%s\n", res.Stdout)
	fmt.Printf("Duration: %v | Backend: %s | ExitCode: %d\n", res.Usage.Wall, res.Backend, res.ExitCode)
}

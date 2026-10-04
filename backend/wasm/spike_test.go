package wasm_test

import (
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func minimalWasmBinary() []byte {
	// Minimal valid WASM binary exporting _start function:
	// (module
	//   (memory (export "memory") 1)
	//   (func (export "_start"))
	// )
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // \0asm
		0x01, 0x00, 0x00, 0x00, // version 1

		// Type section: 1 type () -> ()
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00,

		// Function section: 1 func with type 0
		0x03, 0x02, 0x01, 0x00,

		// Memory section: 1 memory, min 1 page, max not specified
		0x05, 0x03, 0x01, 0x00, 0x01,

		// Export section: 2 exports ("memory" -> mem 0, "_start" -> func 0)
		0x07, 0x13, 0x02,
		// "memory"
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		// "_start"
		0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x00,

		// Code section: 1 func body (0 locals, end)
		0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
	}
}

func TestWazeroMinimalExecution(t *testing.T) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	wasmBin := minimalWasmBinary()
	compiled, err := r.CompileModule(ctx, wasmBin)
	if err != nil {
		t.Fatalf("CompileModule error: %v", err)
	}

	config := wazero.NewModuleConfig().
		WithName("test-instance")

	mod, err := r.InstantiateModule(ctx, compiled, config)
	if err != nil {
		t.Fatalf("InstantiateModule error: %v", err)
	}
	defer mod.Close(ctx)

	t.Logf("Successfully instantiated and ran minimal WASI module!")
}

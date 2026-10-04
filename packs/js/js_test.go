package js_test

import (
	"testing"

	"github.com/micromax/sandbox/packs/js"
)

func TestJSPack(t *testing.T) {
	p := js.Pack()
	if err := p.Validate(); err != nil {
		t.Fatalf("js.Pack() failed validation: %v", err)
	}
	if p.Name != "js" {
		t.Fatalf("unexpected name: %s", p.Name)
	}
	if len(p.Wasm.Module.Embedded) == 0 {
		t.Fatal("embedded wasm binary is empty")
	}
}

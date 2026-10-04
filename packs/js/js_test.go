package js_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/micromax/sandbox/packs/js"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
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

func TestJSStdMethods(t *testing.T) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	defer r.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	p := js.Pack()
	cm, err := r.CompileModule(ctx, p.Wasm.Module.Embedded)
	if err != nil {
		t.Fatalf("CompileModule: %v", err)
	}

	stdin := bytes.NewBufferString("TEST_SENTINEL\n15\nconsole.log(99)\n")
	var stdout, stderr bytes.Buffer

	cfg := wazero.NewModuleConfig().
		WithArgs("qjs", "--std", "-e", `
var b64 = "aGVsbG8g8J+agCDkuJbnlYw="; // "hello 🚀 世界"
var bin = atob(b64);
var bytes = [];
for (var i = 0; i < bin.length; i++) bytes.push(bin.charCodeAt(i));
// decode utf-8
var str = "";
for (var i = 0; i < bytes.length;) {
    var c = bytes[i++];
    if (c < 0x80) str += String.fromCharCode(c);
    else if (c < 0xE0) str += String.fromCharCode(((c & 0x1F) << 6) | (bytes[i++] & 0x3F));
    else if (c < 0xF0) str += String.fromCharCode(((c & 0x0F) << 12) | ((bytes[i++] & 0x3F) << 6) | (bytes[i++] & 0x3F));
    else {
        var cp = ((c & 0x07) << 18) | ((bytes[i++] & 0x3F) << 12) | ((bytes[i++] & 0x3F) << 6) | (bytes[i++] & 0x3F);
        cp -= 0x10000;
        str += String.fromCharCode(0xD800 + (cp >> 10), 0xDC00 + (cp & 0x3FF));
    }
}
std.out.puts("DECODED:" + str + "\n");
`).
		WithStdin(stdin).
		WithStdout(&stdout).
		WithStderr(&stderr)

	mod, err := r.InstantiateModule(ctx, cm, cfg)
	if err != nil {
		t.Fatalf("InstantiateModule: %v (stderr: %s)", err, stderr.String())
	}
	_ = mod.Close(ctx)

	out := stdout.String()
	if !strings.Contains(out, "DECODED:hello 🚀 世界") {
		t.Fatalf("expected decoded string in output, got: %s", out)
	}
}

package sandbox

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDefaultLimitsAreValid(t *testing.T) {
	if err := DefaultLimits().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestZeroLimitsNeverMeanUnlimited(t *testing.T) {
	// A zero-value Limits is invalid on its own: it must be merged onto
	// defaults, never interpreted as "no limit".
	if err := (Limits{}).Validate(); err == nil {
		t.Fatal("zero Limits must not validate")
	}
	got, err := resolveLimits(DefaultLimits(), &Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got != DefaultLimits() {
		t.Fatalf("empty override changed limits: %+v", got)
	}
}

func TestMergeOnlyOverridesSetFields(t *testing.T) {
	base := DefaultLimits()
	got := mergeLimits(base, Limits{Memory: 64 << 20, WallTime: time.Second})
	want := base
	want.Memory = 64 << 20
	want.WallTime = time.Second
	if got != want {
		t.Fatalf("merge = %+v, want %+v", got, want)
	}
}

func TestExplicitUnlimitedValidates(t *testing.T) {
	l := DefaultLimits()
	l.Memory = Unlimited
	l.WallTime = UnlimitedTime
	l.CPUTime = UnlimitedTime
	l.MaxOutput = Unlimited
	l.FSQuota = Unlimited
	l.MaxFiles = UnlimitedCount
	l.MaxProcs = UnlimitedCount
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	// And the merge keeps the sentinels instead of treating them as unset.
	got := mergeLimits(DefaultLimits(), l)
	if got != l {
		t.Fatalf("merge lost sentinels: %+v", got)
	}
}

func TestValidateRejects(t *testing.T) {
	mut := func(f func(*Limits)) Limits { l := DefaultLimits(); f(&l); return l }
	cases := map[string]Limits{
		"tiny memory":       mut(func(l *Limits) { l.Memory = 1000 }),
		"negative wall":     mut(func(l *Limits) { l.WallTime = -2 }),
		"negative cpu":      mut(func(l *Limits) { l.CPUTime = -2 * time.Second }),
		"zero output":       mut(func(l *Limits) { l.MaxOutput = 0 }),
		"zero quota":        mut(func(l *Limits) { l.FSQuota = 0 }),
		"bad max files":     mut(func(l *Limits) { l.MaxFiles = -3 }),
		"bad max procs":     mut(func(l *Limits) { l.MaxProcs = -3 }),
		"zero wall (unset)": mut(func(l *Limits) { l.WallTime = 0 }),
	}
	for name, l := range cases {
		err := l.Validate()
		if !errors.Is(err, ErrInvalidLimits) {
			t.Errorf("%s: err = %v, want ErrInvalidLimits", name, err)
		}
	}
}

func TestPackValidate(t *testing.T) {
	sha := strings.Repeat("a", 64)
	wasmOK := &WasmSpec{Module: Artifact{Name: "m.wasm", URL: "https://example.com/m.wasm", SHA256: sha}}
	cases := []struct {
		name string
		pack *Pack
		ok   bool
	}{
		{"nil", nil, false},
		{"empty name", &Pack{Wasm: wasmOK}, false},
		{"upper-case name", &Pack{Name: "Python", Wasm: wasmOK}, false},
		{"bad alias", &Pack{Name: "python", Aliases: []string{"Py!"}, Wasm: wasmOK}, false},
		{"no backend spec", &Pack{Name: "python"}, false},
		{"valid wasm download", &Pack{Name: "python", Aliases: []string{"py"}, Wasm: wasmOK}, true},
		{"download without hash", &Pack{Name: "x", Wasm: &WasmSpec{Module: Artifact{Name: "m", URL: "https://e/m"}}}, false},
		{"download with short hash", &Pack{Name: "x", Wasm: &WasmSpec{Module: Artifact{Name: "m", URL: "https://e/m", SHA256: "abc"}}}, false},
		{"download with upper-case hash", &Pack{Name: "x", Wasm: &WasmSpec{Module: Artifact{Name: "m", URL: "https://e/m", SHA256: strings.ToUpper(sha)}}}, false},
		{"embedded", &Pack{Name: "js", Wasm: &WasmSpec{Module: Artifact{Name: "q", Embedded: []byte{0}}}}, true},
		{"artifact with nothing", &Pack{Name: "x", Wasm: &WasmSpec{Module: Artifact{Name: "q"}}}, false},
		{"bad mount", &Pack{Name: "x", Wasm: &WasmSpec{Module: Artifact{Embedded: []byte{0}}, Mounts: []Mount{{GuestPath: "/lib"}}}}, false},
		{"docker by digest", &Pack{Name: "go", Docker: &DockerSpec{Image: "golang@sha256:" + sha, Cmd: []string{"go"}}}, true},
		{"docker by tag", &Pack{Name: "go", Docker: &DockerSpec{Image: "golang:latest", Cmd: []string{"go"}}}, false},
		{"docker no cmd", &Pack{Name: "go", Docker: &DockerSpec{Image: "golang@sha256:" + sha}}, false},
		{"extra only", &Pack{Name: "z", Extra: map[string]any{"fake": true}}, true},
	}
	for _, c := range cases {
		err := c.pack.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if !c.ok && !errors.Is(err, ErrInvalidPack) {
			t.Errorf("%s: err = %v, want ErrInvalidPack", c.name, err)
		}
	}
}

func TestPolicyString(t *testing.T) {
	if got := PreferWasm.String(); got != "wasm > docker" {
		t.Fatalf("String = %q", got)
	}
	// NewPolicy must copy its input.
	in := []string{"a"}
	p := NewPolicy(in...)
	in[0] = "b"
	if p.Order[0] != "a" {
		t.Fatal("NewPolicy must copy names")
	}
}

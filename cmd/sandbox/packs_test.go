package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/micromax/sandbox"
)

func TestAllPacksAreValidAndRegisterable(t *testing.T) {
	// sandbox.New validates every pack and rejects duplicate names/aliases.
	if _, err := sandbox.New(sandbox.WithPacks(allPacks()...)); err != nil {
		t.Fatal(err)
	}
}

func TestInferLangReturnsRegisteredLanguages(t *testing.T) {
	registered := packLangs()
	for _, file := range []string{"a.js", "a.mjs", "a.ts", "a.py", "a.lua", "a.wasm", "a.java", "a.go", "a.rs", "a.sh", "a.bash", "A.PY"} {
		lang := inferLang(file)
		if lang == "" {
			t.Errorf("inferLang(%q) returned no language", file)
			continue
		}
		if !slices.Contains(registered, lang) {
			t.Errorf("inferLang(%q) = %q, which is not a registered pack %v", file, lang, registered)
		}
	}
}

func TestDescribePack(t *testing.T) {
	byName := map[string]*sandbox.Pack{}
	for _, p := range allPacks() {
		byName[p.Name] = p
	}

	if tier, rt := describePack(byName["js"]); !strings.HasPrefix(tier, "Tier 1") || !strings.HasPrefix(rt, "Wasm ") {
		t.Errorf("js: tier=%q runtime=%q", tier, rt)
	}
	tier, rt := describePack(byName["node"])
	if !strings.HasPrefix(tier, "Tier 2") || !strings.HasPrefix(rt, "Docker node:") {
		t.Errorf("node: tier=%q runtime=%q", tier, rt)
	}
	if strings.Contains(rt, "@sha256") {
		t.Errorf("runtime should not include the image digest: %q", rt)
	}
}

func TestPackNameFilters(t *testing.T) {
	wasm, docker := packNames(hasWasm), packNames(isDockerOnly)
	for _, n := range []string{"js", "ts", "python", "lua", "wasm"} {
		if !slices.Contains(wasm, n) {
			t.Errorf("%q missing from wasm packs %v", n, wasm)
		}
	}
	for _, n := range []string{"node", "java", "go", "rust", "bash"} {
		if !slices.Contains(docker, n) {
			t.Errorf("%q missing from docker-only packs %v", n, docker)
		}
	}
	if len(wasm)+len(docker) != len(allPacks()) {
		t.Errorf("filters do not partition the packs: %v / %v", wasm, docker)
	}
}

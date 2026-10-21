package main

import (
	"strings"

	"github.com/micromax/sandbox"
	packbash "github.com/micromax/sandbox/packs/bash"
	packgo "github.com/micromax/sandbox/packs/go"
	packjava "github.com/micromax/sandbox/packs/java"
	packjs "github.com/micromax/sandbox/packs/js"
	packlua "github.com/micromax/sandbox/packs/lua"
	packnode "github.com/micromax/sandbox/packs/node"
	packpython "github.com/micromax/sandbox/packs/python"
	packrust "github.com/micromax/sandbox/packs/rust"
	packts "github.com/micromax/sandbox/packs/ts"
	packwasm "github.com/micromax/sandbox/packs/wasm"
)

// allPacks is the single list of language packs the CLI registers. Everything
// user-visible about languages (sandbox packs, doctor, packages list) is derived
// from it, so adding a language means adding it here (and to inferLang).
func allPacks() []*sandbox.Pack {
	return []*sandbox.Pack{
		packjs.Pack(),
		packts.Pack(),
		packpython.Pack(),
		packlua.Pack(),
		packwasm.Pack(),
		packnode.Pack(),
		packjava.Pack(),
		packgo.Pack(),
		packrust.Pack(),
		packbash.Pack(),
	}
}

// describePack returns the isolation tier and a short runtime description.
func describePack(p *sandbox.Pack) (tier, runtime string) {
	switch {
	case p.Wasm != nil:
		return "Tier 1 (Zero Docker)", "Wasm " + p.Version
	case p.Docker != nil:
		image, _, _ := strings.Cut(p.Docker.Image, "@") // drop the digest
		return "Tier 2 (Universal)", "Docker " + image
	default:
		return "custom", p.Version
	}
}

func hasWasm(p *sandbox.Pack) bool      { return p.Wasm != nil }
func isDockerOnly(p *sandbox.Pack) bool { return p.Docker != nil && p.Wasm == nil }

// packNames returns the names of the packs matching keep, in registration order.
func packNames(keep func(*sandbox.Pack) bool) []string {
	var out []string
	for _, p := range allPacks() {
		if keep(p) {
			out = append(out, p.Name)
		}
	}
	return out
}

// packLangs returns every registered pack's canonical name.
func packLangs() []string {
	return packNames(func(*sandbox.Pack) bool { return true })
}

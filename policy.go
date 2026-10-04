package sandbox

import "strings"

// Policy decides which backends may run code, and in which order they are
// tried. Only backends named in Order are ever used, which is how the
// "never silently downgrade isolation" rule is enforced: if none of them can
// run a language, Run fails instead of picking something weaker.
type Policy struct {
	// Order lists backend names, most preferred first.
	Order []string
}

// Built-in policies.
var (
	// PreferWasm uses Wasm when the pack supports it, otherwise Docker (if
	// that backend is registered). This is the default.
	PreferWasm = Policy{Order: []string{"wasm", "docker"}}
	// WasmOnly never uses anything but the Wasm backend.
	WasmOnly = Policy{Order: []string{"wasm"}}
	// DockerOnly never uses anything but the Docker backend.
	DockerOnly = Policy{Order: []string{"docker"}}
)

// NewPolicy returns a custom policy from backend names in preference order.
func NewPolicy(names ...string) Policy {
	return Policy{Order: append([]string(nil), names...)}
}

func (p Policy) String() string { return strings.Join(p.Order, " > ") }

package sandbox

import (
	"io"

	"github.com/micromax/sandbox/vfs"
)

// errFSQuota aliases the VFS quota error so errors.Is works across layers.
var errFSQuota = vfs.ErrQuota

// Spec describes one execution.
type Spec struct {
	// Lang selects the language pack (case-insensitive; aliases allowed).
	Lang string
	// Code is the program source.
	Code string
	// Args are passed to the program.
	Args []string
	// Env is the complete environment visible to the guest. Nothing is
	// inherited from the host.
	Env map[string]string
	// Stdin is the program's standard input. Nil means empty.
	Stdin io.Reader
	// Files are injected read-only under /in. Keys are slash-separated
	// paths relative to /in and are validated by [vfs.Clean].
	Files map[string][]byte
	// Limits overrides the sandbox defaults field by field. Nil keeps them.
	Limits *Limits
	// Net is the network policy. Nil means no network access at all.
	Net *NetPolicy
}

// NetPolicy describes the outbound network access granted to a guest. The
// zero value of Spec.Net (nil) grants none.
//
// Enforcement is backend specific and arrives with the network milestone;
// until a backend declares support (see [NetworkCapable]), a non-nil policy
// makes Run fail with [ErrUnsupported] instead of being silently ignored.
type NetPolicy struct {
	// AllowHosts lists permitted host names; a leading "*." matches any
	// subdomain.
	AllowHosts []string
	// AllowPorts lists permitted destination ports. Empty means 80 and 443.
	AllowPorts []int
	// MaxRequests caps the number of outbound requests. Zero means the
	// backend default.
	MaxRequests int
	// MaxBytes caps total bytes received. Zero means the backend default.
	MaxBytes uint64
	// AllowPrivate permits private, loopback and link-local destinations.
	// Leave false unless you fully understand the SSRF implications.
	AllowPrivate bool
}

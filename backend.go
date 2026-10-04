package sandbox

import (
	"context"
	"io"

	"github.com/micromax/sandbox/vfs"
)

// Backend is an isolation mechanism: Wasm, Docker, and so on. Implementations
// live in sub-packages and are registered with [WithBackends].
type Backend interface {
	// Name is the stable identifier the routing policy refers to, e.g. "wasm".
	Name() string
	// Supports reports whether this backend can run the pack.
	Supports(p *Pack) bool
	// Run executes the request. See [Request] for the contract.
	Run(ctx context.Context, req *Request) (Outcome, error)
}

// NetworkCapable is implemented by backends that can enforce a [NetPolicy].
// A backend that does not implement it, or returns false, is never given a
// request with a network policy: the policy would otherwise be silently
// ignored.
type NetworkCapable interface {
	SupportsNetwork() bool
}

// Request is what the core hands to a backend. All limits are already
// validated and resolved.
//
// A backend must:
//   - write guest stdout and stderr only to Stdout and Stderr, which enforce
//     the output cap and return [ErrOutputLimit] when it is hit; stop the
//     guest and return that error when a write fails;
//   - honour ctx, which carries the wall-clock deadline;
//   - present FS to the guest as /in (read-only), /work and /out, and write
//     nothing to the host filesystem;
//   - never grant network access unless Spec.Net allows it;
//   - return limit violations as the matching Err* sentinel (wrapped is fine).
type Request struct {
	Pack   *Pack
	Spec   Spec
	Limits Limits
	// FS is the guest's virtual filesystem, pre-populated with /in, /work
	// and /out and the injected files.
	FS *vfs.FS
	// Stdin is never nil.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Outcome is what a backend reports about a completed run.
type Outcome struct {
	// ExitCode is the guest exit status. Non-zero is not an error.
	ExitCode int
	// PeakMemory is the peak guest memory in bytes, or 0 if unknown.
	PeakMemory uint64
}

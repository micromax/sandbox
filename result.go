package sandbox

import (
	"errors"
	"time"
)

// Sentinel errors, for use with [errors.Is].
//
// A non-zero exit code from guest code is not a Go error; it is reported in
// [Result.ExitCode].
var (
	// ErrTimeout means the wall-clock limit expired.
	ErrTimeout = errors.New("sandbox: time limit exceeded")
	// ErrMemoryLimit means the guest exceeded its memory limit.
	ErrMemoryLimit = errors.New("sandbox: memory limit exceeded")
	// ErrOutputLimit means the guest produced more output than allowed.
	ErrOutputLimit = errors.New("sandbox: output limit exceeded")
	// ErrFSQuota means the virtual filesystem quota (bytes or file count)
	// was exceeded. It is the same value as [vfs.ErrQuota].
	ErrFSQuota = errFSQuota
	// ErrSessionKilled means a session was destroyed, for example by a
	// timeout, and its state is gone.
	ErrSessionKilled = errors.New("sandbox: session killed")
	// ErrBackendUnavailable means no backend permitted by the routing
	// policy can run the requested language.
	ErrBackendUnavailable = errors.New("sandbox: no permitted backend available")
	// ErrUnsupported means the chosen backend cannot do what was asked.
	ErrUnsupported = errors.New("sandbox: operation not supported")
	// ErrNetworkDenied means the guest tried to use the network and the
	// policy forbids it.
	ErrNetworkDenied = errors.New("sandbox: network access denied")
	// ErrUnknownLanguage means no registered pack matches the language.
	ErrUnknownLanguage = errors.New("sandbox: unknown language")
	// ErrInvalidSpec means the run specification is malformed.
	ErrInvalidSpec = errors.New("sandbox: invalid spec")
	// ErrInvalidLimits means a Limits value is out of range.
	ErrInvalidLimits = errors.New("sandbox: invalid limits")
	// ErrInvalidPack means a pack definition is malformed.
	ErrInvalidPack = errors.New("sandbox: invalid pack")
)

// isLimitError reports whether err is a resource-limit violation. For those
// Run returns a partial Result alongside the error.
func isLimitError(err error) bool {
	return errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrMemoryLimit) ||
		errors.Is(err, ErrOutputLimit) ||
		errors.Is(err, ErrFSQuota)
}

// Result is what an execution produced.
//
// When Run fails because a resource limit was hit (see [ErrTimeout],
// [ErrMemoryLimit], [ErrOutputLimit], [ErrFSQuota]) the returned Result is
// still non-nil and holds whatever was captured before the limit tripped.
type Result struct {
	// Stdout and Stderr hold the captured output, together capped by
	// Limits.MaxOutput.
	Stdout, Stderr []byte
	// Truncated is true when output was cut short by Limits.MaxOutput.
	Truncated bool
	// ExitCode is the guest exit status.
	ExitCode int
	// Files holds the files the guest left under /out, keyed by path
	// relative to /out.
	Files map[string][]byte
	// Usage reports the resources consumed.
	Usage Usage
	// Backend is the name of the backend that ran the code.
	Backend string
}

// Usage reports resources consumed by one execution.
type Usage struct {
	// Wall is the elapsed time.
	Wall time.Duration
	// PeakMemory is the peak guest memory in bytes, if the backend reports it.
	PeakMemory uint64
	// FSBytes is the virtual filesystem size at the end of the run.
	FSBytes uint64
	// FSFiles is the number of files and directories at the end of the run.
	FSFiles int
}

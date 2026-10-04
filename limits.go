package sandbox

import (
	"fmt"
	"math"
	"time"
)

// Unlimited and friends disable a limit explicitly. A zero value in a
// [Limits] field never means unlimited; it means "use the default".
const (
	// Unlimited disables a byte-valued limit (Memory, MaxOutput, FSQuota).
	Unlimited uint64 = math.MaxUint64
	// UnlimitedTime disables a duration-valued limit (WallTime, CPUTime).
	UnlimitedTime time.Duration = -1
	// UnlimitedCount disables a count-valued limit (MaxFiles, MaxProcs).
	UnlimitedCount = -1
)

// Limits bounds the resources one execution may use.
//
// Zero fields take the value from [DefaultLimits] (or the sandbox-wide
// defaults set with [WithDefaultLimits]). Whether and how precisely a limit is
// enforced depends on the backend; each backend documents its guarantees.
type Limits struct {
	// Memory is the maximum guest memory in bytes. Default 128 MiB.
	Memory uint64
	// WallTime is the maximum elapsed time. Default 10 s.
	WallTime time.Duration
	// CPUTime is the maximum CPU time, best effort. Default 10 s.
	CPUTime time.Duration
	// MaxOutput is the combined stdout+stderr byte cap. Default 1 MiB.
	MaxOutput uint64
	// FSQuota is the byte quota of the virtual filesystem. Default 16 MiB.
	FSQuota uint64
	// MaxFiles caps files plus directories in the virtual filesystem,
	// including the three base directories in, work and out. Default 256.
	MaxFiles int
	// MaxProcs caps processes/threads where the backend has them. Default 64.
	MaxProcs int
}

const minMemory = 1 << 20 // 1 MiB

// DefaultLimits returns the built-in safe defaults.
func DefaultLimits() Limits {
	return Limits{
		Memory:    128 << 20,
		WallTime:  10 * time.Second,
		CPUTime:   10 * time.Second,
		MaxOutput: 1 << 20,
		FSQuota:   16 << 20,
		MaxFiles:  256,
		MaxProcs:  64,
	}
}

// mergeLimits returns base with every non-zero field of o applied on top.
func mergeLimits(base, o Limits) Limits {
	if o.Memory != 0 {
		base.Memory = o.Memory
	}
	if o.WallTime != 0 {
		base.WallTime = o.WallTime
	}
	if o.CPUTime != 0 {
		base.CPUTime = o.CPUTime
	}
	if o.MaxOutput != 0 {
		base.MaxOutput = o.MaxOutput
	}
	if o.FSQuota != 0 {
		base.FSQuota = o.FSQuota
	}
	if o.MaxFiles != 0 {
		base.MaxFiles = o.MaxFiles
	}
	if o.MaxProcs != 0 {
		base.MaxProcs = o.MaxProcs
	}
	return base
}

// Validate reports whether a fully resolved Limits value is usable. It is
// called on the result of merging, so zero values are rejected here: they
// would mean a default was missing, never "unlimited".
func (l Limits) Validate() error {
	if l.Memory != Unlimited && l.Memory < minMemory {
		return fmt.Errorf("%w: Memory %d is below the %d byte minimum", ErrInvalidLimits, l.Memory, minMemory)
	}
	if l.WallTime != UnlimitedTime && l.WallTime <= 0 {
		return fmt.Errorf("%w: WallTime must be positive or UnlimitedTime", ErrInvalidLimits)
	}
	if l.CPUTime != UnlimitedTime && l.CPUTime <= 0 {
		return fmt.Errorf("%w: CPUTime must be positive or UnlimitedTime", ErrInvalidLimits)
	}
	if l.MaxOutput == 0 {
		return fmt.Errorf("%w: MaxOutput must be positive", ErrInvalidLimits)
	}
	if l.FSQuota == 0 {
		return fmt.Errorf("%w: FSQuota must be positive", ErrInvalidLimits)
	}
	if l.MaxFiles != UnlimitedCount && l.MaxFiles <= 0 {
		return fmt.Errorf("%w: MaxFiles must be positive or UnlimitedCount", ErrInvalidLimits)
	}
	if l.MaxProcs != UnlimitedCount && l.MaxProcs <= 0 {
		return fmt.Errorf("%w: MaxProcs must be positive or UnlimitedCount", ErrInvalidLimits)
	}
	return nil
}

// resolveLimits applies the per-run override on top of the sandbox defaults
// and validates the outcome.
func resolveLimits(base Limits, override *Limits) (Limits, error) {
	l := base
	if override != nil {
		l = mergeLimits(base, *override)
	}
	if err := l.Validate(); err != nil {
		return Limits{}, err
	}
	return l, nil
}

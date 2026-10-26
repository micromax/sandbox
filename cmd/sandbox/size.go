package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// sizeUnits maps an upper-case suffix to its multiplier. All units are binary
// (1K = 1024 bytes), matching how --mem has always treated "M".
var sizeUnits = map[string]uint64{
	"":    1,
	"B":   1,
	"K":   1 << 10,
	"KB":  1 << 10,
	"KIB": 1 << 10,
	"M":   1 << 20,
	"MB":  1 << 20,
	"MIB": 1 << 20,
	"G":   1 << 30,
	"GB":  1 << 30,
	"GIB": 1 << 30,
}

// parseSize converts a human-friendly size such as "64M", "512KiB", "1g" or a
// bare byte count into bytes. Unknown suffixes, signs, fractions, zero and
// values that overflow uint64 are rejected rather than ignored.
func parseSize(s string) (uint64, error) {
	in := strings.TrimSpace(s)
	if in == "" {
		return 0, fmt.Errorf("empty size")
	}

	i := 0
	for i < len(in) && in[i] >= '0' && in[i] <= '9' {
		i++
	}
	digits, suffix := in[:i], strings.ToUpper(strings.TrimSpace(in[i:]))
	if digits == "" {
		return 0, fmt.Errorf("%q must start with a number", s)
	}
	mult, ok := sizeUnits[suffix]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q in %q (use K, M or G, e.g. 64M)", in[i:], s)
	}

	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is too large", s)
	}
	if n == 0 {
		return 0, fmt.Errorf("%q must be greater than zero", s)
	}
	if n > math.MaxUint64/mult {
		return 0, fmt.Errorf("%q is too large", s)
	}
	return n * mult, nil
}

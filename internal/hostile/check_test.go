package hostile_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/internal/hostile"
)

func res(stdout string, exit int) *sandbox.Result {
	return &sandbox.Result{Stdout: []byte(stdout), ExitCode: exit}
}

// The harness must be able to fail, otherwise a green corpus proves nothing.
func TestCheckDetectsViolations(t *testing.T) {
	blocked := hostile.Case{Mode: hostile.Blocked, Errs: []error{sandbox.ErrTimeout}}
	contained := hostile.Case{Mode: hostile.Contained, Errs: []error{sandbox.ErrTimeout}}

	tests := []struct {
		name    string
		c       hostile.Case
		res     *sandbox.Result
		err     error
		elapsed time.Duration
		want    string // substring of the expected problem; "" means no problem
	}{
		{"blocked and stopped by allowed error", blocked, res("", 0), sandbox.ErrTimeout, time.Second, ""},
		{"wrapped allowed error is accepted", blocked, res("", 0), fmt.Errorf("x: %w", sandbox.ErrTimeout), time.Second, ""},
		{"blocked attack completes cleanly", blocked, res("", 0), nil, time.Second, "completed cleanly"},
		{"blocked attack exits non-zero but not allowed", blocked, res("", 1), nil, time.Second, "completed cleanly"},
		{"non-zero exit accepted when allowed", func() hostile.Case { c := blocked; c.AllowNonZeroExit = true; return c }(), res("", 1), nil, time.Second, ""},
		{"unexpected error", blocked, res("", 0), errors.New("boom"), time.Second, "unexpected error"},
		{"wrong sentinel", blocked, res("", 0), sandbox.ErrOutputLimit, time.Second, "unexpected error"},
		{"marker in stdout", contained, res("x "+hostile.Marker, 0), nil, time.Second, hostile.Marker},
		{"forbidden substring", func() hostile.Case { c := contained; c.StdoutNot = []string{"secret"}; return c }(), res("the secret", 0), nil, time.Second, "forbidden"},
		{"required substring missing", func() hostile.Case { c := contained; c.StdoutHas = []string{"denied"}; return c }(), res("ok", 0), nil, time.Second, "lacks"},
		{"exact stdout mismatch", func() hostile.Case { c := contained; c.StdoutEq = []byte("a"); return c }(), res("b", 0), nil, time.Second, "want"},
		{"over time bound", func() hostile.Case { c := contained; c.Max = time.Second; return c }(), res("", 0), nil, 2 * time.Second, "bound"},
		{"contained clean run is fine", contained, res("fine", 0), nil, time.Second, ""},
		{"nil result with expectations", func() hostile.Case { c := contained; c.StdoutHas = []string{"x"}; return c }(), nil, sandbox.ErrTimeout, time.Second, "no result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(tt.c.Check(tt.res, tt.err, tt.elapsed), "\n")
			switch {
			case tt.want == "" && got != "":
				t.Fatalf("unexpected problems: %s", got)
			case tt.want != "" && !strings.Contains(got, tt.want):
				t.Fatalf("problems = %q, want one containing %q", got, tt.want)
			}
		})
	}
}

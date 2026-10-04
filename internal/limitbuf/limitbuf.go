// Package limitbuf provides output capture with a hard byte budget.
//
// A guest program can print forever. Without a cap that would exhaust host
// memory, so every captured stream is written through a [Writer] that stops
// accepting bytes once the shared [Group] budget is spent.
package limitbuf

import (
	"bytes"
	"sync"
)

// Group is a shared byte budget for several writers (typically stdout and
// stderr). The cap applies to the sum of everything written.
type Group struct {
	mu        sync.Mutex
	max       uint64
	used      uint64
	truncated bool
	errFull   error
}

// NewGroup returns a Group allowing at most max bytes in total. Once the
// budget is exhausted, writes return errFull.
func NewGroup(max uint64, errFull error) *Group {
	return &Group{max: max, errFull: errFull}
}

// NewWriter returns a new capturing writer drawing from the group's budget.
func (g *Group) NewWriter() *Writer { return &Writer{g: g} }

// Truncated reports whether any write was cut short by the budget.
func (g *Group) Truncated() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.truncated
}

// Used returns the number of bytes stored so far across all writers.
func (g *Group) Used() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used
}

// Writer captures bytes in memory, subject to its Group's budget.
type Writer struct {
	g   *Group
	buf bytes.Buffer
}

// Write stores as much of p as the budget allows. When p does not fit
// entirely it stores the part that does, marks the group truncated and
// returns the group's full-error, as io.Writer requires for short writes.
func (w *Writer) Write(p []byte) (int, error) {
	g := w.g
	g.mu.Lock()
	defer g.mu.Unlock()

	remaining := g.max - g.used
	n := len(p)
	if uint64(n) > remaining {
		n = int(remaining)
		g.truncated = true
	}
	w.buf.Write(p[:n])
	g.used += uint64(n)
	if n < len(p) {
		return n, g.errFull
	}
	return n, nil
}

// Bytes returns a copy of everything captured by this writer.
func (w *Writer) Bytes() []byte {
	w.g.mu.Lock()
	defer w.g.mu.Unlock()
	return bytes.Clone(w.buf.Bytes())
}

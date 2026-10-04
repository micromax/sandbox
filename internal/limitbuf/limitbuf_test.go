package limitbuf

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

var errFull = errors.New("full")

func TestWithinBudget(t *testing.T) {
	g := NewGroup(10, errFull)
	w := g.NewWriter()
	n, err := w.Write([]byte("hello"))
	if n != 5 || err != nil {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if g.Truncated() {
		t.Fatal("should not be truncated")
	}
	if string(w.Bytes()) != "hello" {
		t.Fatalf("Bytes = %q", w.Bytes())
	}
}

func TestExactlyAtBudgetIsNotTruncated(t *testing.T) {
	g := NewGroup(5, errFull)
	w := g.NewWriter()
	if _, err := w.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if g.Truncated() {
		t.Fatal("exact fit must not be truncated")
	}
}

func TestTruncationStoresExactlyMax(t *testing.T) {
	const max = 100
	g := NewGroup(max, errFull)
	w := g.NewWriter()
	big := []byte(strings.Repeat("x", max*10))
	n, err := w.Write(big)
	if n != max || !errors.Is(err, errFull) {
		t.Fatalf("Write = %d, %v; want %d, errFull", n, err, max)
	}
	if !g.Truncated() {
		t.Fatal("expected truncated")
	}
	if len(w.Bytes()) != max {
		t.Fatalf("stored %d bytes, want %d", len(w.Bytes()), max)
	}
	// Further writes store nothing.
	if n, err := w.Write([]byte("more")); n != 0 || !errors.Is(err, errFull) {
		t.Fatalf("after full: %d, %v", n, err)
	}
}

func TestBudgetIsShared(t *testing.T) {
	g := NewGroup(10, errFull)
	a, b := g.NewWriter(), g.NewWriter()
	if _, err := a.Write([]byte("123456")); err != nil {
		t.Fatal(err)
	}
	n, err := b.Write([]byte("abcdefgh"))
	if n != 4 || !errors.Is(err, errFull) {
		t.Fatalf("b.Write = %d, %v", n, err)
	}
	if g.Used() != 10 {
		t.Fatalf("Used = %d", g.Used())
	}
}

func TestBytesReturnsCopy(t *testing.T) {
	g := NewGroup(10, errFull)
	w := g.NewWriter()
	w.Write([]byte("abc"))
	b := w.Bytes()
	b[0] = 'Z'
	if string(w.Bytes()) != "abc" {
		t.Fatal("Bytes must return a copy")
	}
}

func TestConcurrentWritesNeverExceedBudget(t *testing.T) {
	const max = 1000
	g := NewGroup(max, errFull)
	var wg sync.WaitGroup
	writers := make([]*Writer, 8)
	for i := range writers {
		writers[i] = g.NewWriter()
		wg.Add(1)
		go func(w *Writer) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				w.Write([]byte("0123456789"))
			}
		}(writers[i])
	}
	wg.Wait()
	total := 0
	for _, w := range writers {
		total += len(w.Bytes())
	}
	if total != max {
		t.Fatalf("total stored = %d, want %d", total, max)
	}
}

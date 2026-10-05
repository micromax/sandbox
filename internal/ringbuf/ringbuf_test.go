package ringbuf

import (
	"bytes"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	rb := New(10)

	// Write less than capacity
	_, _ = rb.Write([]byte("hello"))
	if string(rb.Bytes()) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(rb.Bytes()))
	}

	// Write more, wrapping around
	_, _ = rb.Write([]byte(" world!"))
	// Total written: "hello world!" (12 bytes). Capacity is 10.
	// Last 10 bytes: "llo world!"
	if string(rb.Bytes()) != "llo world!" {
		t.Fatalf("expected 'llo world!', got %q", string(rb.Bytes()))
	}

	// Write a single chunk larger than capacity
	_, _ = rb.Write([]byte("0123456789abcdef"))
	// Last 10 bytes: "6789abcdef"
	if string(rb.Bytes()) != "6789abcdef" {
		t.Fatalf("expected '6789abcdef', got %q", string(rb.Bytes()))
	}

	r := rb.Reader()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r)
	if buf.String() != "6789abcdef" {
		t.Fatalf("reader expected '6789abcdef', got %q", buf.String())
	}
}

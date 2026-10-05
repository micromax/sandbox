package ringbuf

import (
	"bytes"
	"io"
	"sync"
)

// RingBuffer is a thread-safe, fixed-capacity circular buffer implementing io.Writer.
// Oldest bytes are discarded when capacity is exceeded.
type RingBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
	head int
	tail int
	full bool
}

// New creates a RingBuffer with the given maximum byte capacity.
func New(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 64 * 1024 // 64 KB default
	}
	return &RingBuffer{
		buf:  make([]byte, capacity),
		size: capacity,
	}
}

// Write appends p to the circular buffer.
func (r *RingBuffer) Write(p []byte) (n int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n = len(p)
	if n == 0 {
		return 0, nil
	}

	// If incoming slice is larger than capacity, keep only the trailing `size` bytes
	if n >= r.size {
		p = p[n-r.size:]
		copy(r.buf, p)
		r.head = 0
		r.tail = 0
		r.full = true
		return n, nil
	}

	for _, b := range p {
		r.buf[r.tail] = b
		r.tail = (r.tail + 1) % r.size
		if r.full {
			r.head = (r.head + 1) % r.size
		} else if r.tail == r.head {
			r.full = true
		}
	}
	return n, nil
}

// Bytes returns a contiguous copy of the current buffered contents in chronological order.
func (r *RingBuffer) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.full && r.tail >= r.head {
		out := make([]byte, r.tail-r.head)
		copy(out, r.buf[r.head:r.tail])
		return out
	}

	out := make([]byte, r.size)
	n1 := copy(out, r.buf[r.head:])
	copy(out[n1:], r.buf[:r.tail])
	return out
}

// String returns the buffer contents as a string.
func (r *RingBuffer) String() string {
	return string(r.Bytes())
}

// Reader returns an io.Reader reading from the current snapshot of buffer contents.
func (r *RingBuffer) Reader() io.Reader {
	return bytes.NewReader(r.Bytes())
}

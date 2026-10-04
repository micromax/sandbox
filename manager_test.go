package sandbox_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/micromax/sandbox"
)

func TestManagerLifecycleAndCap(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	m := sandbox.NewManager(sb, sandbox.ManagerOpts{
		MaxSessions: 2,
		Idle:        5 * time.Minute,
	})
	defer m.Shutdown(ctx)

	s1, err := m.Create(ctx, "echo")
	if err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	s2, err := m.Create(ctx, "echo")
	if err != nil {
		t.Fatalf("Create s2: %v", err)
	}

	if m.Count() != 2 {
		t.Fatalf("expected count 2, got: %d", m.Count())
	}

	// 3rd session exceeds cap
	_, err = m.Create(ctx, "echo")
	if !errors.Is(err, sandbox.ErrTooManySessions) {
		t.Fatalf("expected ErrTooManySessions, got: %v", err)
	}

	// Lookup via Get
	got, ok := m.Get(s1.ID())
	if !ok || got.ID() != s1.ID() {
		t.Fatalf("failed to retrieve s1 by ID")
	}

	// Close s1 releases slot
	if err := m.Close(s1.ID()); err != nil {
		t.Fatalf("Close s1: %v", err)
	}
	if m.Count() != 1 {
		t.Fatalf("expected count 1 after close, got: %d", m.Count())
	}

	// Now can create s3
	s3, err := m.Create(ctx, "echo")
	if err != nil {
		t.Fatalf("Create s3: %v", err)
	}
	defer s3.Close()
	defer s2.Close()
}

func TestManagerIdleReaping(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	m := sandbox.NewManager(sb, sandbox.ManagerOpts{
		MaxSessions:  10,
		Idle:         40 * time.Millisecond,
		ReapInterval: 10 * time.Millisecond,
	})
	defer m.Shutdown(ctx)

	s, err := m.Create(ctx, "echo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if m.Count() != 1 {
		t.Fatalf("expected 1 session, got: %d", m.Count())
	}

	// Wait for idle reaper to clean it up
	time.Sleep(100 * time.Millisecond)

	if m.Count() != 0 {
		t.Fatalf("expected 0 sessions after idle timeout, got: %d", m.Count())
	}

	// Session was closed by reaper
	_, err = s.Eval(ctx, "hello")
	if !errors.Is(err, sandbox.ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed on reaped session, got: %v", err)
	}
}

func TestManagerConcurrentSessions(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	m := sandbox.NewManager(sb, sandbox.ManagerOpts{
		MaxSessions: 20,
		Idle:        5 * time.Minute,
	})
	defer m.Shutdown(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s, err := m.Create(ctx, "echo")
			if err != nil {
				t.Errorf("Create error: %v", err)
				return
			}
			defer s.Close()

			res, err := s.Eval(ctx, "eval-test")
			if err != nil {
				t.Errorf("Eval error: %v", err)
				return
			}
			if string(res.Stdout) != "eval-test" {
				t.Errorf("unexpected output: %s", res.Stdout)
			}
		}(i)
	}
	wg.Wait()
}

func TestManagerShutdown(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	m := sandbox.NewManager(sb, sandbox.ManagerOpts{
		MaxSessions: 10,
		Idle:        5 * time.Minute,
	})

	s, err := m.Create(ctx, "echo")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if m.Count() != 0 {
		t.Fatalf("expected 0 active sessions after shutdown, got: %d", m.Count())
	}

	// Operations on shut down manager fail
	_, err = m.Create(ctx, "echo")
	if !errors.Is(err, sandbox.ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed, got: %v", err)
	}

	_, err = s.Eval(ctx, "echo test")
	if !errors.Is(err, sandbox.ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed on session after shutdown, got: %v", err)
	}
}

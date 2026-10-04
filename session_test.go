package sandbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
)

func TestSessionLifecycle(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "echo")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	if !s.Alive() {
		t.Fatal("expected session to be alive")
	}
	if s.ID() == "" {
		t.Fatal("expected non-empty session ID")
	}
	if s.Lang() != "echo" {
		t.Fatalf("unexpected lang: %s", s.Lang())
	}

	// 1. Persistence across evals
	res, err := s.Eval(ctx, "set message=hello-world")
	if err != nil {
		t.Fatalf("Eval set: %v", err)
	}
	if res.Restarted {
		t.Fatal("expected Restarted to be false")
	}

	res, err = s.Eval(ctx, "get message")
	if err != nil {
		t.Fatalf("Eval get: %v", err)
	}
	if string(res.Stdout) != "hello-world" {
		t.Fatalf("expected 'hello-world', got: %s", res.Stdout)
	}

	// 2. Reset clears state
	if err := s.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	res, err = s.Eval(ctx, "get message")
	if err != nil {
		t.Fatalf("Eval get after reset: %v", err)
	}
	if string(res.Stdout) != "" {
		t.Fatalf("expected empty stdout after reset, got: %s", res.Stdout)
	}

	// 3. Close closes session
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if s.Alive() {
		t.Fatal("expected session to be dead after Close")
	}

	_, err = s.Eval(ctx, "echo test")
	if !errors.Is(err, sandbox.ErrSessionClosed) {
		t.Fatalf("expected ErrSessionClosed, got: %v", err)
	}
}

func TestSessionTimeoutAndKill(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "spin")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	evalCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	_, err = s.Eval(evalCtx, "spin")
	if !errors.Is(err, sandbox.ErrSessionKilled) {
		t.Fatalf("expected ErrSessionKilled on spin timeout, got: %v", err)
	}
	if !errors.Is(err, sandbox.ErrTimeout) {
		t.Fatalf("expected wrapped ErrTimeout, got: %v", err)
	}
	if s.Alive() {
		t.Fatal("expected killed session to report Alive() == false")
	}

	// Subsequent eval on dead session returns ErrSessionKilled
	_, err = s.Eval(ctx, "echo test")
	if !errors.Is(err, sandbox.ErrSessionKilled) {
		t.Fatalf("expected ErrSessionKilled on subsequent eval, got: %v", err)
	}
}

func TestSessionAutoRestart(t *testing.T) {
	sb := newSB(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "echo", sandbox.WithAutoRestart(true))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	// Kill session
	_, err = s.Eval(ctx, "kill")
	if !errors.Is(err, sandbox.ErrSessionKilled) {
		t.Fatalf("expected ErrSessionKilled, got: %v", err)
	}

	// Next eval auto-restarts the session
	res, err := s.Eval(ctx, "hello after restart")
	if err != nil {
		t.Fatalf("Eval after restart: %v", err)
	}
	if !res.Restarted {
		t.Fatal("expected res.Restarted == true on first eval after restart")
	}
	if !strings.Contains(string(res.Stdout), "hello after restart") {
		t.Fatalf("unexpected stdout: %s", res.Stdout)
	}

	// Next eval runs normally without Restarted flag
	res, err = s.Eval(ctx, "second eval")
	if err != nil {
		t.Fatalf("second eval: %v", err)
	}
	if res.Restarted {
		t.Fatal("expected res.Restarted == false on subsequent eval")
	}
}

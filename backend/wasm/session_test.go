package wasm_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
)

func TestJSSessionStatePersistence(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "js")
	if err != nil {
		t.Fatalf("NewSession js: %v", err)
	}
	defer s.Close()

	if !s.Alive() {
		t.Fatal("expected js session to be alive")
	}

	// 1. Variable persistence
	_, err = s.Eval(ctx, "var x = 21;")
	if err != nil {
		t.Fatalf("eval 1 error: %v", err)
	}

	res, err := s.Eval(ctx, "console.log(x * 2);")
	if err != nil {
		t.Fatalf("eval 2 error: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "42") {
		t.Fatalf("expected '42' in stdout, got: %s", res.Stdout)
	}

	// 2. Function persistence
	_, err = s.Eval(ctx, "function greet(name) { return 'Hello, ' + name + '!'; }")
	if err != nil {
		t.Fatalf("eval 3 error: %v", err)
	}

	res, err = s.Eval(ctx, "console.log(greet('Wasm'));")
	if err != nil {
		t.Fatalf("eval 4 error: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "Hello, Wasm!") {
		t.Fatalf("expected 'Hello, Wasm!', got: %s", res.Stdout)
	}
}

func TestJSSessionExceptionPreserved(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "js")
	if err != nil {
		t.Fatalf("NewSession js: %v", err)
	}
	defer s.Close()

	_, err = s.Eval(ctx, "var count = 100;")
	if err != nil {
		t.Fatalf("eval 1: %v", err)
	}

	// Throw error in eval 2
	res, err := s.Eval(ctx, "throw new Error('kaboom');")
	if err != nil {
		t.Fatalf("eval 2 expected no Go error (exit code reported in result), got: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code on exception")
	}
	if !strings.Contains(string(res.Stderr), "kaboom") {
		t.Fatalf("expected 'kaboom' in stderr, got: %s", res.Stderr)
	}

	// Session must still be alive and count must still be 100
	if !s.Alive() {
		t.Fatal("expected session to remain alive after exception")
	}

	res, err = s.Eval(ctx, "console.log(count + 23);")
	if err != nil {
		t.Fatalf("eval 3: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "123") {
		t.Fatalf("expected '123', got: %s", res.Stdout)
	}
}

func TestJSSessionTimeoutKillsSession(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "js", sandbox.WithSessionLimits(sandbox.Limits{
		WallTime: 100 * time.Millisecond,
	}))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	_, err = s.Eval(ctx, "while(true) {}")
	if !errors.Is(err, sandbox.ErrSessionKilled) {
		t.Fatalf("expected ErrSessionKilled on timeout, got: %v", err)
	}
	if !errors.Is(err, sandbox.ErrTimeout) {
		t.Fatalf("expected wrapped ErrTimeout, got: %v", err)
	}
	if s.Alive() {
		t.Fatal("expected session to be dead")
	}

	// Subsequent eval must fail with ErrSessionKilled
	_, err = s.Eval(ctx, "console.log(1+1);")
	if !errors.Is(err, sandbox.ErrSessionKilled) {
		t.Fatalf("expected ErrSessionKilled on dead session, got: %v", err)
	}
}

func TestPythonSessionStatePersistence(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "python")
	if err != nil {
		t.Fatalf("NewSession python: %v", err)
	}
	defer s.Close()

	// 1. Variable persistence
	_, err = s.Eval(ctx, "x = 21")
	if err != nil {
		t.Fatalf("eval 1: %v", err)
	}

	res, err := s.Eval(ctx, "print(x * 2)")
	if err != nil {
		t.Fatalf("eval 2: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "42") {
		t.Fatalf("expected '42' in stdout, got: %s", res.Stdout)
	}

	// 2. Function persistence
	_, err = s.Eval(ctx, "def multiply(a, b):\n    return a * b")
	if err != nil {
		t.Fatalf("eval 3: %v", err)
	}

	res, err = s.Eval(ctx, "print(multiply(6, 7))")
	if err != nil {
		t.Fatalf("eval 4: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "42") {
		t.Fatalf("expected '42', got: %s", res.Stdout)
	}
}

func TestPythonSessionExceptionPreserved(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "python")
	if err != nil {
		t.Fatalf("NewSession python: %v", err)
	}
	defer s.Close()

	_, err = s.Eval(ctx, "val = 'persisted'")
	if err != nil {
		t.Fatalf("eval 1: %v", err)
	}

	// Raise exception
	res, err := s.Eval(ctx, "raise ValueError('session test error')")
	if err != nil {
		t.Fatalf("unexpected Go error on exception: %v", err)
	}
	if res.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code on python exception")
	}
	if !strings.Contains(string(res.Stderr), "session test error") {
		t.Fatalf("expected traceback in stderr, got: %s", res.Stderr)
	}

	// Session is still alive and val is preserved
	if !s.Alive() {
		t.Fatal("expected session to remain alive after python exception")
	}

	res, err = s.Eval(ctx, "print(val.upper())")
	if err != nil {
		t.Fatalf("eval 3: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "PERSISTED") {
		t.Fatalf("expected 'PERSISTED', got: %s", res.Stdout)
	}
}

func TestGuestPrintsSentinelText(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s, err := sb.NewSession(ctx, "js")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	fakeSentinels := `console.log("__SENTINEL__\n__SB_EOF__\nREADY\nEXIT:0");`
	res, err := s.Eval(ctx, fakeSentinels)
	if err != nil {
		t.Fatalf("Eval error: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "__SENTINEL__") {
		t.Fatalf("expected fake sentinel in stdout, got: %s", res.Stdout)
	}

	// Verify next eval works cleanly
	res, err = s.Eval(ctx, "console.log('clean');")
	if err != nil {
		t.Fatalf("Eval clean error: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "clean") {
		t.Fatalf("expected clean output, got: %s", res.Stdout)
	}
}

func TestTwoSessionsDoNotShareState(t *testing.T) {
	sb := newTestSandbox(t)
	ctx := context.Background()

	s1, err := sb.NewSession(ctx, "js")
	if err != nil {
		t.Fatalf("NewSession 1: %v", err)
	}
	defer s1.Close()

	s2, err := sb.NewSession(ctx, "js")
	if err != nil {
		t.Fatalf("NewSession 2: %v", err)
	}
	defer s2.Close()

	// In s1, set secret
	_, err = s1.Eval(ctx, "var secret = 's1_secret';")
	if err != nil {
		t.Fatalf("s1 set: %v", err)
	}

	// In s2, secret must be undefined
	res, err := s2.Eval(ctx, "console.log(typeof secret);")
	if err != nil {
		t.Fatalf("s2 get: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "undefined") {
		t.Fatalf("expected 'undefined' in s2, got: %s", res.Stdout)
	}
}

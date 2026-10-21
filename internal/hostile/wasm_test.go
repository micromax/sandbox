package hostile_test

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"sync/atomic"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	backendwasm "github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/internal/hostile"
	packjs "github.com/micromax/sandbox/packs/js"
	packlua "github.com/micromax/sandbox/packs/lua"
	packpython "github.com/micromax/sandbox/packs/python"
)

// newFixture creates real host-side canaries: if a guest can read or touch
// them, the sandbox leaked.
func newFixture(t *testing.T) *hostile.Fixture {
	t.Helper()
	dir := t.TempDir()
	fx := &hostile.Fixture{
		CanaryEnvName: "HOSTILE_CANARY_SECRET",
		CanarySecret:  "canary-3f9a1c7e5b2d4086",
		CanaryPath:    filepath.Join(dir, "canary.txt"),
		WritePath:     filepath.Join(dir, "pwned.txt"),
		LoopbackHost:  "127.0.0.1",
	}
	t.Setenv(fx.CanaryEnvName, fx.CanarySecret)
	if err := os.WriteFile(fx.CanaryPath, []byte(fx.CanarySecret), 0o600); err != nil {
		t.Fatal(err)
	}

	// A host-loopback listener the guest must never reach.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	fx.LoopbackPort = ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			fx.RecordLoopbackHit()
			c.Close()
		}
	}()
	return fx
}

func TestWasmHostileGuests(t *testing.T) {
	fx := newFixture(t)

	wb, err := backendwasm.New()
	if err != nil {
		t.Fatal(err)
	}
	sb, err := sandbox.New(
		sandbox.WithBackends(wb),
		sandbox.WithPacks(packjs.Pack(), packlua.Pack(), packpython.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Warm the compilation cache and unpack the Python stdlib once, so the timing
	// bounds below measure the attack and not first-run setup.
	for _, lang := range []string{"js", "lua", "python"} {
		code := "print(1)"
		if lang == "js" {
			code = "console.log(1)"
		}
		if _, err := sb.Run(t.Context(), sandbox.Spec{Lang: lang, Code: code}); err != nil {
			t.Fatalf("warm-up %s: %v", lang, err)
		}
	}

	// Bound concurrency: dozens of simultaneous guests (and the race detector)
	// would turn the timing assertions into a measure of machine load.
	slots := make(chan struct{}, 4)

	for _, c := range hostile.Cases(fx) {
		if raceEnabled && c.Known == "" { // known-issue cases keep their strict bound
			c.Max *= 3
		}
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()

			start := time.Now()
			res, err := sb.Run(t.Context(), sandbox.Spec{
				Lang:   c.Lang,
				Code:   c.Code,
				Env:    c.Env,
				Files:  c.Files,
				Limits: c.Limits,
			})
			problems := c.Check(res, err, time.Since(start))
			if c.After != nil {
				c.After(t, fx)
			}

			switch {
			case c.Known != "" && len(problems) == 0:
				t.Errorf("marked Known but now passes; remove the Known marker: %s", c.Known)
			case c.Known != "":
				t.Skipf("known issue: %s\n  observed: %v", c.Known, problems)
			default:
				for _, p := range problems {
					t.Error(p)
				}
			}
		})
	}
}

// Mutation check on the real backend: if the guest is granted the canary secret
// (i.e. a leak actually happens), the corpus must notice.
func TestCorpusDetectsARealLeak(t *testing.T) {
	fx := newFixture(t)
	wb, err := backendwasm.New()
	if err != nil {
		t.Fatal(err)
	}
	sb, err := sandbox.New(sandbox.WithBackends(wb), sandbox.WithPacks(packpython.Pack()), sandbox.WithPolicy(sandbox.WasmOnly))
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range hostile.Cases(fx) {
		if c.Name != "leak/python-host-environment" {
			continue
		}
		start := time.Now()
		res, err := sb.Run(t.Context(), sandbox.Spec{
			Lang: c.Lang,
			Code: c.Code,
			Env:  map[string]string{fx.CanaryEnvName: fx.CanarySecret}, // the "leak"
		})
		if problems := c.Check(res, err, time.Since(start)); len(problems) == 0 {
			t.Fatal("corpus did not detect the canary secret reaching the guest")
		}
		return
	}
	t.Fatal("leak/python-host-environment case not found")
}

// Limits.Memory caps the guest's linear memory, but a deep-recursing guest also
// grows the wasm call stack, which lives in the host process. This measures how
// far host memory grows past the guest limit.
func TestHostMemoryStaysNearGuestLimit(t *testing.T) {
	const (
		guestLimit = 64 << 20
		allowance  = 2 * guestLimit // generous: runtime, compiled code and buffers
	)
	const known = "measured: a deep-recursion Python guest with Memory=64MiB grows the host process by hundreds of MiB (call stack is outside Limits.Memory)"

	wb, err := backendwasm.New()
	if err != nil {
		t.Fatal(err)
	}
	sb, err := sandbox.New(sandbox.WithBackends(wb), sandbox.WithPacks(packpython.Pack()), sandbox.WithPolicy(sandbox.WasmOnly))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sb.Run(t.Context(), sandbox.Spec{Lang: "python", Code: "print(1)"}); err != nil { // warm-up
		t.Fatal(err)
	}

	// Live heap objects plus goroutine stacks. Unlike total mapped memory this
	// shrinks after a GC, so the baseline does not depend on which tests ran first.
	total := func() uint64 {
		s := []metrics.Sample{
			{Name: "/memory/classes/heap/objects:bytes"},
			{Name: "/memory/classes/heap/stacks:bytes"},
		}
		metrics.Read(s)
		return s[0].Value.Uint64() + s[1].Value.Uint64()
	}
	runtime.GC()
	base := total()

	var peak atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-time.After(25 * time.Millisecond):
				if v := total(); v > peak.Load() {
					peak.Store(v)
				}
			}
		}
	}()

	_, _ = sb.Run(t.Context(), sandbox.Spec{
		Lang: "python",
		Code: "import sys\nsys.setrecursionlimit(10**9)\ndef f(n): return f(n + 1) + 1\nf(0)",
		// Long wall limit: the recursion should end on its own at a fixed depth,
		// so the peak does not depend on how fast this machine is.
		Limits: &sandbox.Limits{Memory: guestLimit, WallTime: 90 * time.Second},
	})
	close(stop)
	<-done

	growth := int64(peak.Load()) - int64(base)
	t.Logf("host memory grew by %d MiB (guest limit %d MiB, allowance %d MiB)", growth>>20, guestLimit>>20, allowance>>20)
	if growth > allowance {
		t.Skipf("known issue: %s\n  observed growth: %d MiB", known, growth>>20)
	}
	t.Errorf("marked known but host growth %d MiB is now within %d MiB; remove the Known marker", growth>>20, allowance>>20)
}

package sandbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/internal/fake"
	"github.com/micromax/sandbox/vfs"
)

func newSB(t *testing.T, opts ...sandbox.Option) *sandbox.Sandbox {
	t.Helper()
	all := []sandbox.Option{
		sandbox.WithBackends(fake.Backend{}),
		sandbox.WithPacks(fake.Packs()...),
		sandbox.WithPolicy(sandbox.NewPolicy("fake")),
	}
	sb, err := sandbox.New(append(all, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return sb
}

func TestRunEcho(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Code: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Stdout) != "hello" || res.ExitCode != 0 || res.Backend != "fake" {
		t.Fatalf("result = %+v", res)
	}
	if res.Usage.Wall < 0 { // clock granularity on Windows can read 0
		t.Fatalf("Usage.Wall = %v", res.Usage.Wall)
	}
}

func TestLanguageLookupIsCaseInsensitive(t *testing.T) {
	sb := newSB(t)
	if _, err := sb.Run(context.Background(), sandbox.Spec{Lang: "  ECHO ", Code: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestAliases(t *testing.T) {
	p := fake.Pack("echo")
	p.Aliases = []string{"say"}
	sb, err := sandbox.New(
		sandbox.WithBackends(fake.Backend{}),
		sandbox.WithPacks(p),
		sandbox.WithPolicy(sandbox.NewPolicy("fake")),
	)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "say", Code: "hi"})
	if err != nil || string(res.Stdout) != "hi" {
		t.Fatalf("alias run = %v, %v", res, err)
	}
	if got := sb.Languages(); len(got) != 1 || got[0] != "echo" {
		t.Fatalf("Languages = %v", got)
	}
}

func TestNonZeroExitIsNotAnError(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "fail", Code: "boom"})
	if err != nil {
		t.Fatalf("non-zero exit must not be an error, got %v", err)
	}
	if res.ExitCode != 3 || string(res.Stderr) != "boom" {
		t.Fatalf("result = %+v", res)
	}
}

func TestStdin(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "cat", Stdin: strings.NewReader("abc")})
	if err != nil || string(res.Stdout) != "abc" {
		t.Fatalf("stdin run = %v, %v", res, err)
	}
	// Nil stdin is empty, not a panic.
	res, err = sb.Run(context.Background(), sandbox.Spec{Lang: "cat"})
	if err != nil || len(res.Stdout) != 0 {
		t.Fatalf("nil stdin run = %v, %v", res, err)
	}
}

func TestOutputFilesCollected(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "writer", Code: "data"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Files["result.txt"]) != "data" || len(res.Files) != 1 {
		t.Fatalf("Files = %v", res.Files)
	}
	if res.Usage.FSBytes != 4 {
		t.Fatalf("FSBytes = %d", res.Usage.FSBytes)
	}
}

func TestInjectedFilesAreNormalised(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang:  "files",
		Files: map[string][]byte{"a.txt": []byte("A"), `dir\b.txt`: []byte("B"), "/c.txt": []byte("C")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(res.Stdout), "a.txt\nc.txt\ndir/b.txt\n"; got != want {
		t.Fatalf("listing = %q, want %q", got, want)
	}
}

func TestHostileInjectedFileNamesRejected(t *testing.T) {
	sb := newSB(t)
	for _, name := range []string{"../escape", `..\escape`, "a/../../b", `C:\x`, "nul\x00", ".", ""} {
		_, err := sb.Run(context.Background(), sandbox.Spec{
			Lang: "echo", Files: map[string][]byte{name: []byte("x")},
		})
		if !errors.Is(err, sandbox.ErrInvalidSpec) {
			t.Errorf("file name %q: err = %v, want ErrInvalidSpec", name, err)
		}
	}
}

func TestEnvIsExactlyWhatWasGiven(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "env", Env: map[string]string{"B": "2", "A": "1"},
	})
	if err != nil || string(res.Stdout) != "A=1\nB=2\n" {
		t.Fatalf("env run = %q, %v", res.Stdout, err)
	}
	res, _ = sb.Run(context.Background(), sandbox.Spec{Lang: "env"})
	if len(res.Stdout) != 0 {
		t.Fatalf("empty Env leaked %q", res.Stdout)
	}
}

func TestInvalidEnvRejected(t *testing.T) {
	sb := newSB(t)
	for _, env := range []map[string]string{{"": "x"}, {"A=B": "x"}, {"A": "x\x00"}, {"A\x00": "x"}} {
		if _, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Env: env}); !errors.Is(err, sandbox.ErrInvalidSpec) {
			t.Errorf("env %q: err = %v", env, err)
		}
	}
}

func TestMissingLangRejected(t *testing.T) {
	sb := newSB(t)
	if _, err := sb.Run(context.Background(), sandbox.Spec{}); !errors.Is(err, sandbox.ErrInvalidSpec) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownLanguage(t *testing.T) {
	sb := newSB(t)
	_, err := sb.Run(context.Background(), sandbox.Spec{Lang: "cobol"})
	if !errors.Is(err, sandbox.ErrUnknownLanguage) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "echo") {
		t.Fatalf("error should list registered languages: %v", err)
	}
}

func TestTimeoutReturnsPartialResultAndErrTimeout(t *testing.T) {
	sb := newSB(t)
	start := time.Now()
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "spin", Limits: &sandbox.Limits{WallTime: 50 * time.Millisecond},
	})
	if !errors.Is(err, sandbox.ErrTimeout) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want ErrTimeout wrapping DeadlineExceeded", err)
	}
	if res == nil {
		t.Fatal("limit errors must return a partial Result")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("deadline was not enforced promptly")
	}
}

func TestCallerCancellationIsNotATimeout(t *testing.T) {
	sb := newSB(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	res, err := sb.Run(ctx, sandbox.Spec{Lang: "spin"})
	if !errors.Is(err, context.Canceled) || errors.Is(err, sandbox.ErrTimeout) {
		t.Fatalf("err = %v, want context.Canceled and not ErrTimeout", err)
	}
	if res != nil {
		t.Fatal("non-limit errors must return a nil Result")
	}
}

func TestCallerDeadlineIsNotMislabelled(t *testing.T) {
	sb := newSB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := sb.Run(ctx, sandbox.Spec{Lang: "spin", Limits: &sandbox.Limits{WallTime: time.Hour}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, sandbox.ErrTimeout) {
		t.Fatal("caller's own deadline must not be reported as the sandbox time limit")
	}
}

func TestOutputLimit(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "flood", Limits: &sandbox.Limits{MaxOutput: 4096},
	})
	if !errors.Is(err, sandbox.ErrOutputLimit) {
		t.Fatalf("err = %v, want ErrOutputLimit", err)
	}
	if res == nil || len(res.Stdout) != 4096 || !res.Truncated {
		t.Fatalf("partial result wrong: %+v", res)
	}
}

func TestFSQuotaOnInjection(t *testing.T) {
	sb := newSB(t)
	_, err := sb.Run(context.Background(), sandbox.Spec{
		Lang:   "echo",
		Files:  map[string][]byte{"big": make([]byte, 2048)},
		Limits: &sandbox.Limits{FSQuota: 1024},
	})
	if !errors.Is(err, sandbox.ErrFSQuota) || !errors.Is(err, vfs.ErrQuota) {
		t.Fatalf("err = %v, want ErrFSQuota", err)
	}
}

func TestFSQuotaInsideGuest(t *testing.T) {
	sb := newSB(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "writer", Code: strings.Repeat("x", 2000),
		Limits: &sandbox.Limits{FSQuota: 1024},
	})
	if !errors.Is(err, sandbox.ErrFSQuota) {
		t.Fatalf("err = %v, want ErrFSQuota", err)
	}
	if res == nil || len(res.Files) != 0 {
		t.Fatalf("failed write must leave no file: %+v", res)
	}
}

func TestMaxFilesCountsBaseDirectories(t *testing.T) {
	sb := newSB(t)
	// in, work, out = 3 nodes; 2 files would make 5.
	_, err := sb.Run(context.Background(), sandbox.Spec{
		Lang:   "echo",
		Files:  map[string][]byte{"a": nil, "b": nil},
		Limits: &sandbox.Limits{MaxFiles: 4},
	})
	if !errors.Is(err, sandbox.ErrFSQuota) {
		t.Fatalf("err = %v, want ErrFSQuota (file count)", err)
	}
}

func TestNeverDowngradesIsolation(t *testing.T) {
	// Only the fake backend is registered but the policy permits only wasm.
	sb, err := sandbox.New(
		sandbox.WithBackends(fake.Backend{}),
		sandbox.WithPacks(fake.Packs()...),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sb.Run(context.Background(), sandbox.Spec{Lang: "echo"})
	if !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("err = %v, want ErrBackendUnavailable", err)
	}
}

func TestDefaultPolicyIsPreferWasm(t *testing.T) {
	sb, err := sandbox.New(sandbox.WithBackends(fake.Backend{}), sandbox.WithPacks(fake.Packs()...))
	if err != nil {
		t.Fatal(err)
	}
	// "fake" is not in PreferWasm's order, so it must not be used.
	if _, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo"}); !errors.Is(err, sandbox.ErrBackendUnavailable) {
		t.Fatalf("err = %v, want ErrBackendUnavailable", err)
	}
}

func TestPolicyOrderPrefersEarlierBackend(t *testing.T) {
	sb, err := sandbox.New(
		sandbox.WithBackends(second{}, fake.Backend{}),
		sandbox.WithPacks(fake.Packs()...),
		sandbox.WithPolicy(sandbox.NewPolicy("fake", "second")),
	)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Code: "x"})
	if err != nil || res.Backend != "fake" {
		t.Fatalf("backend = %v, err %v", res, err)
	}
}

func TestPolicyFallsThroughToNextSupportingBackend(t *testing.T) {
	// "second" supports nothing, so routing must skip it and use "fake".
	sb, err := sandbox.New(
		sandbox.WithBackends(second{}, fake.Backend{}),
		sandbox.WithPacks(fake.Packs()...),
		sandbox.WithPolicy(sandbox.NewPolicy("second", "fake")),
	)
	if err != nil {
		t.Fatal(err)
	}
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Code: "x"})
	if err != nil || res.Backend != "fake" {
		t.Fatalf("backend = %v, err %v", res, err)
	}
}

func TestNetworkPolicyNeverSilentlyIgnored(t *testing.T) {
	sb := newSB(t)
	_, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Net: &sandbox.NetPolicy{AllowHosts: []string{"example.com"}}})
	if !errors.Is(err, sandbox.ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}

func TestBackendErrorReturnsNilResult(t *testing.T) {
	sb := newSB(t)
	sb2, _ := sandbox.New(
		sandbox.WithBackends(fake.Backend{}),
		sandbox.WithPacks(fake.Pack("mystery")),
		sandbox.WithPolicy(sandbox.NewPolicy("fake")),
	)
	_ = sb
	res, err := sb2.Run(context.Background(), sandbox.Spec{Lang: "mystery"})
	if err == nil || res != nil {
		t.Fatalf("res = %v, err = %v; want nil result and an error", res, err)
	}
}

func TestPerRunLimitsMergeOntoDefaults(t *testing.T) {
	sb := newSB(t, sandbox.WithDefaultLimits(sandbox.Limits{MaxOutput: 10}))
	res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Code: strings.Repeat("a", 100)})
	if !errors.Is(err, sandbox.ErrOutputLimit) {
		t.Fatalf("err = %v, want ErrOutputLimit", err)
	}
	if res == nil || len(res.Stdout) != 10 {
		t.Fatalf("sandbox default MaxOutput not applied: %+v", res)
	}
}

func TestNewValidation(t *testing.T) {
	t.Run("duplicate pack", func(t *testing.T) {
		_, err := sandbox.New(sandbox.WithPacks(fake.Pack("echo"), fake.Pack("echo")))
		if !errors.Is(err, sandbox.ErrInvalidPack) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("alias collides", func(t *testing.T) {
		p := fake.Pack("other")
		p.Aliases = []string{"echo"}
		_, err := sandbox.New(sandbox.WithPacks(fake.Pack("echo"), p))
		if !errors.Is(err, sandbox.ErrInvalidPack) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("duplicate backend", func(t *testing.T) {
		if _, err := sandbox.New(sandbox.WithBackends(fake.Backend{}, fake.Backend{})); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("nil backend", func(t *testing.T) {
		if _, err := sandbox.New(sandbox.WithBackends(nil)); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("bad default limits", func(t *testing.T) {
		_, err := sandbox.New(sandbox.WithDefaultLimits(sandbox.Limits{Memory: 100}))
		if !errors.Is(err, sandbox.ErrInvalidLimits) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestInvalidPerRunLimits(t *testing.T) {
	sb := newSB(t)
	for _, l := range []sandbox.Limits{
		{Memory: 100},
		{WallTime: -5 * time.Second},
		{MaxFiles: -7},
	} {
		l := l
		if _, err := sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Limits: &l}); !errors.Is(err, sandbox.ErrInvalidLimits) {
			t.Errorf("limits %+v: err = %v", l, err)
		}
	}
}

func TestUnlimitedMustBeExplicit(t *testing.T) {
	sb := newSB(t)
	// Explicit unlimited output lets a large write through.
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "echo", Code: strings.Repeat("a", 2<<20),
		Limits: &sandbox.Limits{MaxOutput: sandbox.Unlimited},
	})
	if err != nil || len(res.Stdout) != 2<<20 {
		t.Fatalf("explicit unlimited failed: %v", err)
	}
	// The default does not.
	res, err = sb.Run(context.Background(), sandbox.Spec{Lang: "echo", Code: strings.Repeat("a", 2<<20)})
	if !errors.Is(err, sandbox.ErrOutputLimit) {
		t.Fatalf("default must cap output, err = %v", err)
	}
	_ = res
}

func TestConcurrentRunsAreIndependent(t *testing.T) {
	sb := newSB(t)
	errs := make(chan error, 50)
	for i := 0; i < 50; i++ {
		go func() {
			res, err := sb.Run(context.Background(), sandbox.Spec{Lang: "writer", Code: "mine"})
			if err == nil && string(res.Files["result.txt"]) != "mine" {
				err = errors.New("cross-talk between runs")
			}
			errs <- err
		}()
	}
	for i := 0; i < 50; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// second is a backend that supports nothing.
type second struct{}

func (second) Name() string                { return "second" }
func (second) Supports(*sandbox.Pack) bool { return false }
func (second) Run(context.Context, *sandbox.Request) (sandbox.Outcome, error) {
	return sandbox.Outcome{}, errors.New("second must never run")
}

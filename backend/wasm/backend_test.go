package wasm_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/backend/wasm"
	"github.com/micromax/sandbox/packages"
	"github.com/micromax/sandbox/packs/js"
	"github.com/micromax/sandbox/packs/python"
)

func newTestSandbox(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	wBackend, err := wasm.New()
	if err != nil {
		t.Fatalf("failed to create wasm backend: %v", err)
	}

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(js.Pack(), python.Pack()),
		sandbox.WithPolicy(sandbox.WasmOnly),
	)
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	return sb
}

func TestJSRun(t *testing.T) {
	sb := newTestSandbox(t)

	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `console.log("HELLO " + (20 + 22));`,
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "HELLO 42") {
		t.Fatalf("expected 'HELLO 42', got: %s", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got: %d", res.ExitCode)
	}
	if res.Backend != "wasm" {
		t.Fatalf("expected backend 'wasm', got: %s", res.Backend)
	}
}

func TestJSFilesInOut(t *testing.T) {
	sb := newTestSandbox(t)

	code := `
const f = std.open('/in/hello.txt', 'r');
const msg = f.readAsString();
f.close();

const out = std.open('/out/result.txt', 'w');
out.puts(msg.toUpperCase());
out.close();
console.log('done');
`

	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: code,
		Files: map[string][]byte{
			"hello.txt": []byte("welcome inside sandbox"),
		},
	})
	if err != nil {
		t.Fatalf("Run error: %v, stderr: %s", err, res.Stderr)
	}

	t.Logf("stdout: %s", res.Stdout)
	t.Logf("stderr: %s", res.Stderr)
	if string(res.Files["result.txt"]) != "WELCOME INSIDE SANDBOX" {
		t.Fatalf("expected output file, got: %q", string(res.Files["result.txt"]))
	}
}

func TestJSTimeout(t *testing.T) {
	sb := newTestSandbox(t)

	start := time.Now()
	_, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `while(true) {}`,
		Limits: &sandbox.Limits{
			WallTime: 100 * time.Millisecond,
		},
	})

	if !errors.Is(err, sandbox.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took too long to terminate: %v", elapsed)
	}
}

func TestJSOutputLimit(t *testing.T) {
	sb := newTestSandbox(t)

	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `while(true) { console.log("output flood line"); }`,
		Limits: &sandbox.Limits{
			MaxOutput: 4096,
		},
	})

	if !errors.Is(err, sandbox.ErrOutputLimit) {
		t.Fatalf("expected ErrOutputLimit, got: %v", err)
	}
	if res == nil || len(res.Stdout) != 4096 || !res.Truncated {
		t.Fatalf("partial result incorrect: %+v", res)
	}
}

func TestJSDiskQuota(t *testing.T) {
	sb := newTestSandbox(t)

	code := `
const f = std.open('/work/large.txt', 'w');
for (let i = 0; i < 2000; i++) {
    f.puts("012345678901234567890123456789\n");
}
f.close();
`
	_, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: code,
		Limits: &sandbox.Limits{
			FSQuota: 1024, // 1 KB quota, code writes ~60 KB
		},
	})

	if !errors.Is(err, sandbox.ErrFSQuota) {
		t.Fatalf("expected ErrFSQuota, got: %v", err)
	}
}

func TestJSEnvironmentIsolation(t *testing.T) {
	sb := newTestSandbox(t)

	code := `
console.log("SECRET=" + std.getenv("SECRET_KEY"));
console.log("HOST_PATH=" + std.getenv("PATH"));
`
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: code,
		Env: map[string]string{
			"SECRET_KEY": "sandbox-secret",
		},
	})
	if err != nil {
		t.Fatalf("Run error: %v, stderr: %s", err, res.Stderr)
	}

	stdout := string(res.Stdout)
	if !strings.Contains(stdout, "SECRET=sandbox-secret") {
		t.Fatalf("expected SECRET=sandbox-secret, got: %s", stdout)
	}
	if strings.Contains(stdout, "HOST_PATH=") && !strings.Contains(stdout, "HOST_PATH=undefined") && !strings.Contains(stdout, "HOST_PATH=null") && !strings.Contains(stdout, "HOST_PATH=\n") {
		t.Fatalf("host PATH leaked: %s", stdout)
	}
}

func TestPythonRun(t *testing.T) {
	sb := newTestSandbox(t)

	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "python",
		Code: `
import json
data = {"answer": 6 * 7, "framework": "micromax-sandbox"}
print(json.dumps(data))
`,
	})
	if err != nil {
		t.Fatalf("Run error: %v, stderr: %s", err, res.Stderr)
	}
	t.Logf("stdout: %s", res.Stdout)
	t.Logf("stderr: %s", res.Stderr)
	if !strings.Contains(string(res.Stdout), `"answer": 42`) {
		t.Fatalf("expected answer 42, got: %s", res.Stdout)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got: %d", res.ExitCode)
	}
}

func TestPythonFilesInOut(t *testing.T) {
	sb := newTestSandbox(t)

	code := `
with open('/in/input.txt', 'r') as f:
    text = f.read()

with open('/out/output.txt', 'w') as f:
    f.write(text.replace("world", "sandbox"))

print("python complete")
`

	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "python",
		Code: code,
		Files: map[string][]byte{
			"input.txt": []byte("hello world from test"),
		},
	})
	if err != nil {
		t.Fatalf("Run error: %v, stderr: %s", err, res.Stderr)
	}

	if string(res.Files["output.txt"]) != "hello sandbox from test" {
		t.Fatalf("unexpected output file: %q", string(res.Files["output.txt"]))
	}
}

func TestPythonTimeout(t *testing.T) {
	sb := newTestSandbox(t)

	start := time.Now()
	_, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "python",
		Code: `
while True:
    pass
`,
		Limits: &sandbox.Limits{
			WallTime: 100 * time.Millisecond,
		},
	})

	if !errors.Is(err, sandbox.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestPythonMemoryBomb(t *testing.T) {
	sb := newTestSandbox(t)

	// Attempts to allocate massive memory under tight memory limit (8 MiB)
	code := `
chunks = []
for _ in range(1000):
    chunks.append(bytearray(10 * 1024 * 1024))
`
	_, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "python",
		Code: code,
		Limits: &sandbox.Limits{
			Memory: 8 * 1024 * 1024, // 8 MiB limit
		},
	})

	if err == nil {
		t.Fatal("expected memory limit violation or MemoryError")
	}
	t.Logf("Memory limit error returned: %v", err)
}

func TestPythonSubprocessDenied(t *testing.T) {
	sb := newTestSandbox(t)

	code := `
import os
try:
    os.system("echo hacked")
    print("FAILED_TO_RESTRICT")
except Exception as e:
    print("RESTRICTED:", e)
`
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "python",
		Code: code,
	})
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	// Under WASI, os.system either raises AttributeError or fails without executing host binaries
	if strings.Contains(string(res.Stdout), "hacked") {
		t.Fatal("host command executed inside sandbox!")
	}
}

func TestConcurrentSandboxes(t *testing.T) {
	sb := newTestSandbox(t)

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			res, err := sb.Run(context.Background(), sandbox.Spec{
				Lang: "js",
				Code: `console.log("instance-" + (10 + 5));`,
			})
			if err != nil {
				errs <- err
				return
			}
			if !bytes.Contains(res.Stdout, []byte("instance-15")) {
				errs <- errors.New("unexpected stdout in concurrent instance")
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent run error: %v", err)
	}
}

func TestWasmNetworkPolicy(t *testing.T) {
	sb := newTestSandbox(t)
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `console.log("network policy active");`,
		Net: &sandbox.NetPolicy{
			AllowHosts: []string{"api.example.com"},
			AllowPorts: []int{443},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error with network policy: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "network policy active") {
		t.Errorf("got %q, want network policy active", string(res.Stdout))
	}
}

func TestWasmCompilationCacheOptions(t *testing.T) {
	// Test in-memory compilation cache option
	bMem, err := wasm.New(wasm.WithInMemoryCompilationCache())
	if err != nil {
		t.Fatalf("wasm.New with in-memory cache failed: %v", err)
	}
	defer bMem.Close(context.Background())

	sbMem, err := sandbox.New(
		sandbox.WithBackends(bMem),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}

	res, err := sbMem.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `console.log("in-memory cache ok");`,
	})
	if err != nil {
		t.Fatalf("sbMem.Run failed: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "in-memory cache ok") {
		t.Errorf("got %q, want 'in-memory cache ok'", string(res.Stdout))
	}

	// Test persistent directory compilation cache option
	tempCacheDir := t.TempDir()
	bDir, err := wasm.New(wasm.WithCompilationCacheDir(tempCacheDir))
	if err != nil {
		t.Fatalf("wasm.New with cache dir failed: %v", err)
	}
	defer bDir.Close(context.Background())

	sbDir, err := sandbox.New(
		sandbox.WithBackends(bDir),
		sandbox.WithPacks(js.Pack()),
	)
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}

	// First execution writes to cache directory
	res1, err := sbDir.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `console.log("disk cache run 1");`,
	})
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	if !strings.Contains(string(res1.Stdout), "disk cache run 1") {
		t.Errorf("got %q, want 'disk cache run 1'", string(res1.Stdout))
	}

	// Second execution hits the cache
	res2, err := sbDir.Run(context.Background(), sandbox.Spec{
		Lang: "js",
		Code: `console.log("disk cache run 2");`,
	})
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if !strings.Contains(string(res2.Stdout), "disk cache run 2") {
		t.Errorf("got %q, want 'disk cache run 2'", string(res2.Stdout))
	}
}

func TestPythonGuestPackages(t *testing.T) {
	// 1. Create a fake pure-python wheel in memory
	var wheelBuf bytes.Buffer
	zw := zip.NewWriter(&wheelBuf)
	f, err := zw.Create("greet/__init__.py")
	if err != nil {
		t.Fatalf("creating zip file: %v", err)
	}
	_, _ = f.Write([]byte("def say_hi(name):\n    return f'Hello, {name} from package!'\n"))
	_ = zw.Close()

	// 2. Setup custom package store
	pkgDir := t.TempDir()
	store, err := packages.NewStore(pkgDir)
	if err != nil {
		t.Fatalf("packages.NewStore failed: %v", err)
	}
	if err := store.Add("python", "greet", "greet-0.1.0-py3-none-any.whl", wheelBuf.Bytes()); err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}

	wBackend, err := wasm.New(wasm.WithPackageStore(store))
	if err != nil {
		t.Fatalf("wasm.New failed: %v", err)
	}
	defer wBackend.Close(context.Background())

	sb, err := sandbox.New(
		sandbox.WithBackends(wBackend),
		sandbox.WithPacks(python.Pack()),
	)
	if err != nil {
		t.Fatalf("sandbox.New failed: %v", err)
	}

	// 3. Run python code that imports the package
	res, err := sb.Run(context.Background(), sandbox.Spec{
		Lang: "python",
		Code: `
import greet
print(greet.say_hi("Antigravity"))
`,
		Packages: []string{"greet"},
	})
	if err != nil {
		t.Fatalf("sb.Run with package failed: %v", err)
	}
	if !strings.Contains(string(res.Stdout), "Hello, Antigravity from package!") {
		t.Errorf("expected greet output, got: %s", string(res.Stdout))
	}

	// 4. Test missing package returns ErrPackageNotFound
	_, missingErr := sb.Run(context.Background(), sandbox.Spec{
		Lang:     "python",
		Code:     `import nonexistent`,
		Packages: []string{"nonexistent"},
	})
	if !errors.Is(missingErr, sandbox.ErrPackageNotFound) {
		t.Errorf("expected ErrPackageNotFound, got: %v", missingErr)
	}
}



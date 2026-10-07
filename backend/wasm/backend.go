// Package wasm implements the WebAssembly isolation backend using wazero.
//
// All executions run in WebAssembly sandboxes with zero CGO and zero external
// daemons. Memory, wall-clock time, disk quotas, and stdout/stderr outputs are
// strictly bounded.
package wasm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/artifact"
	"github.com/micromax/sandbox/netpolicy"
	"github.com/micromax/sandbox/packs/ts"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Backend implements [sandbox.Backend] via wazero.
type Backend struct {
	store      *artifact.Store
	compCache  wazero.CompilationCache
	mu         sync.RWMutex
	mountPaths map[string]string // cached host paths for unpacked directory mounts
}

// Option configures the Wasm backend.
type Option func(*Backend)

// WithArtifactStore configures a custom artifact store.
func WithArtifactStore(store *artifact.Store) Option {
	return func(b *Backend) {
		b.store = store
	}
}

// New creates a new Wasm backend.
func New(opts ...Option) (*Backend, error) {
	store, err := artifact.NewStore()
	if err != nil {
		return nil, fmt.Errorf("wasm: creating artifact store: %w", err)
	}

	b := &Backend{
		store:      store,
		compCache:  wazero.NewCompilationCache(),
		mountPaths: make(map[string]string),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b, nil
}

// Close frees the compilation cache.
func (b *Backend) Close(ctx context.Context) error {
	return b.compCache.Close(ctx)
}

// Name implements [sandbox.Backend].
func (b *Backend) Name() string {
	return "wasm"
}

// Supports implements [sandbox.Backend].
func (b *Backend) Supports(p *sandbox.Pack) bool {
	return p != nil && p.Wasm != nil
}

// SupportsNetwork implements [sandbox.NetworkCapable].
func (b *Backend) SupportsNetwork() bool {
	return true
}

// Run executes the request in an isolated WebAssembly sandbox.
func (b *Backend) Run(ctx context.Context, req *sandbox.Request) (sandbox.Outcome, error) {
	if req.Pack == nil || req.Pack.Wasm == nil {
		return sandbox.Outcome{}, fmt.Errorf("%w: pack %q has no wasm spec", sandbox.ErrUnsupported, req.Pack.Name)
	}

	wasmSpec := req.Pack.Wasm

	// 1. Fetch / verify wasm module binary
	var moduleData []byte
	isRawWasm := req.Pack.Name == "wasm" || req.Pack.Name == "wasi" || req.Pack.Name == "wasip1"
	if isRawWasm {
		if raw, ok := req.Spec.Files["main.wasm"]; ok && len(raw) > 0 {
			moduleData = raw
		} else if raw, ok := req.FS.Snapshot("in")["main.wasm"]; ok && len(raw) > 0 {
			moduleData = raw
		} else if strings.HasPrefix(req.Spec.Code, "\x00asm") {
			moduleData = []byte(req.Spec.Code)
		} else if wasmSpec.Module.Name != "" && wasmSpec.Module.Name != "custom.wasm" {
			var err error
			moduleData, err = b.store.Load(ctx, wasmSpec.Module)
			if err != nil {
				return sandbox.Outcome{}, fmt.Errorf("loading wasm module %s: %w", wasmSpec.Module.Name, err)
			}
		} else {
			return sandbox.Outcome{}, fmt.Errorf("%w: wasm pack requires WebAssembly bytecode in Code or Files[\"main.wasm\"]", sandbox.ErrInvalidSpec)
		}
	} else {
		var err error
		moduleData, err = b.store.Load(ctx, wasmSpec.Module)
		if err != nil {
			return sandbox.Outcome{}, fmt.Errorf("loading wasm module %s: %w", wasmSpec.Module.Name, err)
		}
	}

	// 2. Prepare temporary directory structure for this run
	runDir, err := os.MkdirTemp("", "sb-wasm-*")
	if err != nil {
		return sandbox.Outcome{}, fmt.Errorf("creating run workspace: %w", err)
	}
	defer os.RemoveAll(runDir)

	inDir := filepath.Join(runDir, "in")
	workDir := filepath.Join(runDir, "work")
	outDir := filepath.Join(runDir, "out")

	if err := os.MkdirAll(inDir, 0o755); err != nil {
		return sandbox.Outcome{}, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return sandbox.Outcome{}, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return sandbox.Outcome{}, err
	}

	// Copy injected files into inDir
	inFiles := req.FS.Snapshot("in")
	for relPath, content := range inFiles {
		target := filepath.Join(inDir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return sandbox.Outcome{}, err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return sandbox.Outcome{}, err
		}
	}

	// Prepare arguments and script file
	args := make([]string, 0, len(wasmSpec.Args)+len(req.Spec.Args)+2)
	if isRawWasm {
		args = append(args, "main.wasm")
		if len(req.Spec.Args) > 0 {
			args = append(args, req.Spec.Args...)
		}
	} else {
		args = append(args, wasmSpec.Args...)
		if len(req.Spec.Args) > 0 {
			args = append(args, req.Spec.Args...)
		} else if req.Spec.Code != "" {
			switch req.Pack.Name {
			case "js", "javascript":
				codePath := filepath.Join(workDir, "__main__.js")
				if err := os.WriteFile(codePath, []byte(req.Spec.Code), 0o644); err != nil {
					return sandbox.Outcome{}, err
				}
				args = append(args, "/work/__main__.js")
			case "ts", "typescript":
				jsCode := ts.Transpile(req.Spec.Code)
				codePath := filepath.Join(workDir, "__main__.js")
				if err := os.WriteFile(codePath, []byte(jsCode), 0o644); err != nil {
					return sandbox.Outcome{}, err
				}
				args = append(args, "/work/__main__.js")
			case "python", "py":
				codePath := filepath.Join(workDir, "__main__.py")
				if err := os.WriteFile(codePath, []byte(req.Spec.Code), 0o644); err != nil {
					return sandbox.Outcome{}, err
				}
				args = append(args, "-B", "/work/__main__.py")
			case "lua", "lua54", "luawasi":
				codePath := filepath.Join(workDir, "__main__.lua")
				if err := os.WriteFile(codePath, []byte(req.Spec.Code), 0o644); err != nil {
					return sandbox.Outcome{}, err
				}
				args = append(args, "/work/__main__.lua")
			default:
				codePath := filepath.Join(workDir, "main")
				if err := os.WriteFile(codePath, []byte(req.Spec.Code), 0o644); err != nil {
					return sandbox.Outcome{}, err
				}
				args = append(args, "/work/main")
			}
		}
	}

	// 3. Configure filesystem mounts
	// Mounting runDir at the root ("") enables both absolute paths (/in, /work, /out)
	// and relative paths (in, work, out) to resolve seamlessly.
	fsConfig := wazero.NewFSConfig().
		WithDirMount(runDir, "")

	// Additional read-only mounts (e.g. stdlib)
	for _, m := range wasmSpec.Mounts {
		hostPath, err := b.resolveMount(ctx, m)
		if err != nil {
			return sandbox.Outcome{}, fmt.Errorf("resolving mount %s: %w", m.GuestPath, err)
		}
		cleanGuest := strings.Trim(filepath.ToSlash(m.GuestPath), "/")
		// If the unpacked directory contains the target guest folder (e.g. unpacked/lib),
		// mount that folder directly to avoid nesting (e.g. /lib/lib/python3.13).
		nested := filepath.Join(hostPath, cleanGuest)
		if fi, err := os.Stat(nested); err == nil && fi.IsDir() {
			hostPath = nested
		}
		fsConfig = fsConfig.WithReadOnlyDirMount(hostPath, cleanGuest)
	}

	// Wrap context with cancel so if output limit is exceeded, execution terminates immediately
	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()

	wrappedStdout := &cancelingWriter{Writer: req.Stdout, onLimit: cancelExec}
	wrappedStderr := &cancelingWriter{Writer: req.Stderr, onLimit: cancelExec}

	// 4. Configure wazero runtime with memory and deadline limits
	rConfig := wazero.NewRuntimeConfig().
		WithCompilationCache(b.compCache).
		WithCloseOnContextDone(true)

	if req.Limits.Memory != sandbox.Unlimited {
		pages := uint32(req.Limits.Memory / 65536)
		if pages == 0 {
			pages = 1
		}
		if pages > 65536 {
			pages = 65536
		}
		rConfig = rConfig.WithMemoryLimitPages(pages)
	}

	runtime := wazero.NewRuntimeWithConfig(execCtx, rConfig)
	defer runtime.Close(execCtx)

	wasi_snapshot_preview1.MustInstantiate(execCtx, runtime)
	unstableBuilder := runtime.NewHostModuleBuilder("wasi_unstable")
	wasi_snapshot_preview1.NewFunctionExporter().ExportFunctions(unstableBuilder)
	if _, err := unstableBuilder.Instantiate(execCtx); err != nil {
		return sandbox.Outcome{}, fmt.Errorf("instantiating wasi_unstable: %w", err)
	}

	var netLogs []sandbox.NetLogEntry
	var netMu sync.Mutex

	if req.Spec.Net != nil {
		netPolicy := req.Spec.Net
		matcher := netpolicy.NewHostMatcher(netPolicy.AllowHosts)
		limiter := netpolicy.NewRequestLimiter(netPolicy.MaxRequests, netPolicy.MaxBytes, 0)
		rv := netpolicy.NewRedirectValidator(netPolicy.AllowPrivate, netPolicy.AllowHosts)
		netClient := netpolicy.NewHTTPClient(matcher, netPolicy.AllowPorts, netPolicy.AllowPrivate, limiter, rv)

		netBuilder := runtime.NewHostModuleBuilder("sandbox_net")
		netBuilder.NewFunctionBuilder().
			WithFunc(func(ctx context.Context, m api.Module, mPtr, mLen, uPtr, uLen, bPtr, bLen, respPtr, respMax, writtenPtr uint32) uint32 {
				mem := m.Memory()
				mBytes, _ := mem.Read(mPtr, mLen)
				uBytes, _ := mem.Read(uPtr, uLen)
				bBytes, _ := mem.Read(bPtr, bLen)

				method := string(mBytes)
				reqURL := string(uBytes)

				httpReq, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(bBytes))
				if err != nil {
					netMu.Lock()
					netLogs = append(netLogs, sandbox.NetLogEntry{
						Timestamp: time.Now(),
						Method:    method,
						URL:       reqURL,
						Error:     err.Error(),
					})
					netMu.Unlock()
					return 500
				}

				start := time.Now()
				resp, err := netClient.Do(httpReq)
				duration := time.Since(start)
				if err != nil {
					netMu.Lock()
					netLogs = append(netLogs, sandbox.NetLogEntry{
						Timestamp: start,
						Method:    method,
						URL:       reqURL,
						Duration:  duration,
						Error:     err.Error(),
					})
					netMu.Unlock()
					return 502
				}
				defer resp.Body.Close()

				body, _ := io.ReadAll(resp.Body)
				toWrite := len(body)
				if uint32(toWrite) > respMax {
					toWrite = int(respMax)
				}
				mem.Write(respPtr, body[:toWrite])
				mem.WriteUint32Le(writtenPtr, uint32(toWrite))

				netMu.Lock()
				netLogs = append(netLogs, sandbox.NetLogEntry{
					Timestamp:        start,
					Method:           method,
					URL:              reqURL,
					StatusCode:       resp.StatusCode,
					BytesTransferred: int64(toWrite),
					Duration:         duration,
				})
				netMu.Unlock()

				return uint32(resp.StatusCode)
			}).
			Export("http_request")

		if _, err := netBuilder.Instantiate(execCtx); err != nil {
			return sandbox.Outcome{}, fmt.Errorf("instantiating sandbox_net host module: %w", err)
		}
	}

	compiled, err := runtime.CompileModule(execCtx, moduleData)
	if err != nil {
		return sandbox.Outcome{}, fmt.Errorf("compiling wasm module: %w", err)
	}

	// Module config
	modName := fmt.Sprintf("wasm-%s", req.Pack.Name)
	modConfig := wazero.NewModuleConfig().
		WithName(modName).
		WithArgs(args...).
		WithFSConfig(fsConfig).
		WithStdin(req.Stdin).
		WithStdout(wrappedStdout).
		WithStderr(wrappedStderr)

	// Set environment variables
	for k, v := range req.Spec.Env {
		modConfig = modConfig.WithEnv(k, v)
	}
	// Common language defaults
	if req.Pack.Name == "python" || req.Pack.Name == "py" {
		modConfig = modConfig.WithEnv("PYTHONHOME", "/")
		modConfig = modConfig.WithEnv("PYTHONPATH", "/lib/python3.13")
	}

	// 5. Instantiate and execute module
	mod, instErr := runtime.InstantiateModule(ctx, compiled, modConfig)
	if mod != nil {
		_ = mod.Close(ctx)
	}

	// 6. Map errors
	outcome := sandbox.Outcome{
		NetLog: netLogs,
	}
	var runErr error

	if instErr != nil {
		runErr = b.mapError(ctx, instErr, &outcome)
	}

	// 7. Verify disk quota and collect output files
	quotaErr := b.checkQuotaAndCollect(runDir, req)
	if runErr == nil && quotaErr != nil {
		runErr = quotaErr
	}

	return outcome, runErr
}

// cancelingWriter notifies onLimit when an output write fails due to limitbuf budget exhaustion.
type cancelingWriter struct {
	io.Writer
	onLimit context.CancelFunc
}

func (cw *cancelingWriter) Write(p []byte) (int, error) {
	n, err := cw.Writer.Write(p)
	if err != nil && errors.Is(err, sandbox.ErrOutputLimit) {
		cw.onLimit()
	}
	return n, err
}

func (b *Backend) mapError(ctx context.Context, err error, outcome *sandbox.Outcome) error {
	if errors.Is(err, sandbox.ErrOutputLimit) {
		return sandbox.ErrOutputLimit
	}
	// If context was canceled due to output limit reached
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
		return sandbox.ErrTimeout
	}

	// Check exit code
	var exitErr *sys.ExitError
	if errors.As(err, &exitErr) {
		code := int(exitErr.ExitCode())
		// If context deadline was exceeded when exit error was raised
		if ctx.Err() == context.DeadlineExceeded {
			return sandbox.ErrTimeout
		}
		outcome.ExitCode = code
		return nil
	}

	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "memory limit") ||
		strings.Contains(errStr, "grow memory") ||
		strings.Contains(errStr, "out of memory") ||
		strings.Contains(errStr, "failed to grow memory") {
		return sandbox.ErrMemoryLimit
	}
	if strings.Contains(errStr, "context deadline exceeded") {
		return sandbox.ErrTimeout
	}
	if strings.Contains(errStr, "context canceled") {
		return context.Canceled
	}

	return err
}

func (b *Backend) checkQuotaAndCollect(runDir string, req *sandbox.Request) error {
	var totalBytes uint64
	var totalFiles int

	outDir := filepath.Join(runDir, "out")
	workDir := filepath.Join(runDir, "work")

	// Scan work and out dirs for total usage
	for _, dir := range []string{workDir, outDir} {
		_ = filepath.Walk(dir, func(p string, info fs.FileInfo, err error) error {
			if err != nil || p == dir {
				return nil
			}
			totalFiles++
			if !info.IsDir() {
				totalBytes += uint64(info.Size())
			}
			return nil
		})
	}

	// Injected files in 'in' also count towards total workspace nodes
	inSnap := req.FS.Snapshot("in")
	totalFiles += len(inSnap)

	if req.Limits.FSQuota != sandbox.Unlimited && totalBytes > req.Limits.FSQuota {
		return sandbox.ErrFSQuota
	}
	if req.Limits.MaxFiles != sandbox.UnlimitedCount && totalFiles > req.Limits.MaxFiles {
		return sandbox.ErrFSQuota
	}

	// Collect output files from outDir into req.FS
	err := filepath.Walk(outDir, func(p string, info fs.FileInfo, err error) error {
		if err != nil || p == outDir || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(outDir, p)
		if err != nil {
			return nil
		}
		cleanRel := filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return req.FS.WriteFile("out/"+cleanRel, data)
	})
	if err != nil {
		return fmt.Errorf("collecting output files: %w", err)
	}

	return nil
}

func (b *Backend) resolveMount(ctx context.Context, m sandbox.Mount) (string, error) {
	b.mu.RLock()
	existing, ok := b.mountPaths[m.Artifact.SHA256]
	b.mu.RUnlock()
	if ok {
		return existing, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if existing, ok := b.mountPaths[m.Artifact.SHA256]; ok {
		return existing, nil
	}

	data, err := b.store.Load(ctx, m.Artifact)
	if err != nil {
		return "", err
	}

	// Unpack artifact directory (e.g. zip archive of stdlib)
	destDir := filepath.Join(b.store.Dir(), "unpacked", m.Artifact.SHA256)
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := unpackToDir(data, destDir); err != nil {
			return "", err
		}
	}

	b.mountPaths[m.Artifact.SHA256] = destDir
	return destDir, nil
}

func unpackToDir(data []byte, dest string) error {
	_ = os.MkdirAll(dest, 0o755)
	// If it is a zip archive, extract it
	if len(data) > 4 && string(data[:4]) == "PK\x03\x04" {
		return extractZipBytes(data, dest)
	}
	// Otherwise save as single file
	return os.WriteFile(filepath.Join(dest, "artifact.bin"), data, 0o644)
}

func extractZipBytes(data []byte, dest string) error {
	tmpFile, err := os.CreateTemp("", "zip-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return err
	}
	_ = tmpFile.Close()

	return extractZipFile(tmpName, dest)
}

func extractZipFile(zipPath, dest string) error {
	r, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	fi, err := r.Stat()
	if err != nil {
		return err
	}

	zr, err := newZipReader(r, fi.Size())
	if err != nil {
		return err
	}

	for _, f := range zr.File {
		outPath := filepath.Join(dest, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(outPath, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		outFile, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

package wasm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/micromax/sandbox"
	"github.com/micromax/sandbox/vfs"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var _ sandbox.SessionBackend = (*Backend)(nil)

// NewSession implements [sandbox.SessionBackend].
func (b *Backend) NewSession(ctx context.Context, req *sandbox.SessionRequest) (sandbox.Session, error) {
	if req.Pack == nil || req.Pack.Wasm == nil {
		return nil, fmt.Errorf("%w: pack %q has no wasm spec", sandbox.ErrUnsupported, req.Pack.Name)
	}
	if req.Pack.Wasm.SessionDriver == "" {
		return nil, fmt.Errorf("%w: pack %q has no session driver", sandbox.ErrUnsupported, req.Pack.Name)
	}

	moduleData, err := b.store.Load(ctx, req.Pack.Wasm.Module)
	if err != nil {
		return nil, fmt.Errorf("wasm: loading module for session %s: %w", req.Pack.Wasm.Module.Name, err)
	}

	sess := &wasmSession{
		backend:     b,
		req:         req,
		moduleData:  moduleData,
		id:          req.Config.ID,
		lang:        req.Pack.Name,
		limits:      req.Config.Limits,
		autoRestart: req.Config.AutoRestart,
		maxLifetime: req.Config.MaxLifetime,
		createdAt:   time.Now(),
	}

	if err := sess.start(ctx); err != nil {
		_ = sess.Close()
		return nil, err
	}

	return sess, nil
}

type wasmSession struct {
	backend     *Backend
	req         *sandbox.SessionRequest
	moduleData  []byte
	id          string
	lang        string
	limits      sandbox.Limits
	autoRestart bool
	maxLifetime time.Duration
	createdAt   time.Time

	evalMu sync.Mutex // serializes evals on this session

	mu          sync.Mutex
	closed      bool
	dead        bool
	runtime     wazero.Runtime
	sessCtx     context.Context
	cancelSess  context.CancelFunc
	runDir      string
	fsys        *vfs.FS
	stdinWriter *io.PipeWriter
	stdoutPipe  *io.PipeReader
	stderrPipe  *io.PipeReader
	doneCh      chan struct{}
	lastErr     error
}

func (s *wasmSession) ID() string   { return s.id }
func (s *wasmSession) Lang() string { return s.lang }

func (s *wasmSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && !s.dead
}

func (s *wasmSession) start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.startLocked(ctx)
}

func (s *wasmSession) startLocked(ctx context.Context) error {
	runDir, err := os.MkdirTemp("", "sb-sess-*")
	if err != nil {
		return fmt.Errorf("wasm: creating session workspace: %w", err)
	}
	s.runDir = runDir

	inDir := filepath.Join(runDir, "in")
	workDir := filepath.Join(runDir, "work")
	outDir := filepath.Join(runDir, "out")

	if err := os.MkdirAll(inDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	// Write driver script
	wasmSpec := s.req.Pack.Wasm
	var driverFileName string
	var args []string

	switch s.req.Pack.Name {
	case "js", "javascript":
		driverFileName = "driver.js"
		args = []string{"qjs", "--std", "/work/driver.js"}
	case "python", "py", "python3":
		driverFileName = "driver.py"
		args = []string{"python.wasm", "-B", "/work/driver.py"}
	default:
		driverFileName = "driver"
		args = append([]string{}, wasmSpec.Args...)
		args = append(args, "/work/driver")
	}

	driverPath := filepath.Join(workDir, driverFileName)
	if err := os.WriteFile(driverPath, []byte(wasmSpec.SessionDriver), 0o644); err != nil {
		return fmt.Errorf("writing driver script: %w", err)
	}

	fsConfig := wazero.NewFSConfig().WithDirMount(runDir, "")
	for _, m := range wasmSpec.Mounts {
		hostPath, err := s.backend.resolveMount(ctx, m)
		if err != nil {
			return fmt.Errorf("resolving mount %s: %w", m.GuestPath, err)
		}
		cleanGuest := strings.Trim(filepath.ToSlash(m.GuestPath), "/")
		nested := filepath.Join(hostPath, cleanGuest)
		if fi, err := os.Stat(nested); err == nil && fi.IsDir() {
			hostPath = nested
		}
		fsConfig = fsConfig.WithReadOnlyDirMount(hostPath, cleanGuest)
	}

	// Configure context and runtime
	sessCtx, cancelSess := context.WithCancel(context.Background())
	s.sessCtx = sessCtx
	s.cancelSess = cancelSess

	rConfig := wazero.NewRuntimeConfig().
		WithCompilationCache(s.backend.compCache).
		WithCloseOnContextDone(true)

	if s.limits.Memory != sandbox.Unlimited {
		pages := uint32(s.limits.Memory / 65536)
		if pages == 0 {
			pages = 1
		}
		if pages > 65536 {
			pages = 65536
		}
		rConfig = rConfig.WithMemoryLimitPages(pages)
	}

	runtime := wazero.NewRuntimeWithConfig(sessCtx, rConfig)
	s.runtime = runtime
	wasi_snapshot_preview1.MustInstantiate(sessCtx, runtime)

	compiled, err := runtime.CompileModule(sessCtx, s.moduleData)
	if err != nil {
		runtime.Close(sessCtx)
		return fmt.Errorf("compiling wasm module for session: %w", err)
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()

	s.stdinWriter = stdinW
	s.stdoutPipe = stdoutR
	s.stderrPipe = stderrR

	modConfig := wazero.NewModuleConfig().
		WithName(fmt.Sprintf("sess-%s-%d", s.id, time.Now().UnixNano())).
		WithArgs(args...).
		WithFSConfig(fsConfig).
		WithStdin(stdinR).
		WithStdout(stdoutW).
		WithStderr(stderrW)

	for k, v := range s.req.Config.Env {
		modConfig = modConfig.WithEnv(k, v)
	}
	if s.req.Pack.Name == "python" || s.req.Pack.Name == "py" || s.req.Pack.Name == "python3" {
		modConfig = modConfig.WithEnv("PYTHONHOME", "/")
		modConfig = modConfig.WithEnv("PYTHONPATH", "/lib/python3.13")
	}

	doneCh := make(chan struct{})
	s.doneCh = doneCh
	s.dead = false

	go func() {
		_, runErr := runtime.InstantiateModule(sessCtx, compiled, modConfig)
		_ = stdoutW.Close()
		_ = stderrW.Close()

		s.mu.Lock()
		s.dead = true
		s.lastErr = runErr
		s.mu.Unlock()

		close(doneCh)
	}()

	// Wait for READY signal from driver
	readyCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 6) // "READY\n"
		_, err := io.ReadFull(stdoutR, buf)
		if err != nil {
			readyCh <- fmt.Errorf("driver failed to signal ready: %w", err)
			return
		}
		if string(buf) != "READY\n" {
			readyCh <- fmt.Errorf("unexpected ready signal: %q", string(buf))
			return
		}
		readyCh <- nil
	}()

	select {
	case err := <-readyCh:
		if err != nil {
			cancelSess()
			<-doneCh
			return err
		}
		return nil
	case <-ctx.Done():
		cancelSess()
		<-doneCh
		return ctx.Err()
	case <-doneCh:
		cancelSess()
		s.mu.Lock()
		lastErr := s.lastErr
		s.mu.Unlock()
		return fmt.Errorf("session interpreter exited prematurely: %v", lastErr)
	}
}

func (s *wasmSession) Eval(ctx context.Context, code string) (*sandbox.Result, error) {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, sandbox.ErrSessionClosed
	}

	restarted := false
	if s.dead {
		if !s.autoRestart {
			s.mu.Unlock()
			return nil, sandbox.ErrSessionKilled
		}
		// Clean up old runtime before restart
		s.cleanupRuntimeLocked()
		if err := s.startLocked(ctx); err != nil {
			s.mu.Unlock()
			return nil, fmt.Errorf("restarting session: %w", err)
		}
		restarted = true
	}

	if s.maxLifetime > 0 && time.Since(s.createdAt) > s.maxLifetime {
		s.mu.Unlock()
		_ = s.Close()
		return nil, fmt.Errorf("%w: session max lifetime reached", sandbox.ErrSessionClosed)
	}

	stdinW := s.stdinWriter
	stdoutR := s.stdoutPipe
	stderrR := s.stderrPipe
	cancelSess := s.cancelSess
	s.mu.Unlock()

	// Wall-clock deadline for this Eval
	evalWallTime := s.limits.WallTime
	if evalWallTime <= 0 || evalWallTime == sandbox.UnlimitedTime {
		evalWallTime = 10 * time.Second
	}
	evalCtx, cancelEval := context.WithTimeout(ctx, evalWallTime)
	defer cancelEval()

	// Generate 32-byte crypto token for output framing
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		tokenBytes = []byte(fmt.Sprintf("%d", time.Now().UnixNano()))
	}
	token := hex.EncodeToString(tokenBytes)
	b64Code := base64.StdEncoding.EncodeToString([]byte(code))

	maxOutput := s.limits.MaxOutput
	if maxOutput == 0 {
		maxOutput = 1 << 20 // 1 MiB default
	}

	start := time.Now()

	type outResult struct {
		data   []byte
		status int
		err    error
	}

	stdoutCh := make(chan outResult, 1)
	stderrCh := make(chan outResult, 1)

	// Reader for stdout
	go func() {
		data, status, err := readStdoutUntilSentinel(stdoutR, token, maxOutput, cancelSess)
		stdoutCh <- outResult{data: data, status: status, err: err}
	}()

	// Reader for stderr
	go func() {
		data, err := readStderrUntilSentinel(stderrR, token, maxOutput, cancelSess)
		stderrCh <- outResult{data: data, err: err}
	}()

	// Send eval payload to guest driver
	msg := token + "\n" + b64Code + "\n"
	if _, err := io.WriteString(stdinW, msg); err != nil {
		s.mu.Lock()
		s.dead = true
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: failed to write to guest stdin: %v", sandbox.ErrSessionKilled, err)
	}

	var stdoutRes, stderrRes outResult
	var outErr error

	for i := 0; i < 2; i++ {
		select {
		case res := <-stdoutCh:
			stdoutRes = res
			if res.err != nil && outErr == nil {
				outErr = res.err
			}
		case res := <-stderrCh:
			stderrRes = res
			if res.err != nil && outErr == nil {
				outErr = res.err
			}
		case <-evalCtx.Done():
			// Timeout or cancellation kills the session
			cancelSess()
			s.mu.Lock()
			s.dead = true
			s.mu.Unlock()

			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%w: %w", sandbox.ErrSessionKilled, sandbox.ErrTimeout)
		}
	}

	wall := time.Since(start)

	if outErr != nil {
		if errors.Is(outErr, sandbox.ErrOutputLimit) {
			cancelSess()
			s.mu.Lock()
			s.dead = true
			s.mu.Unlock()
			return &sandbox.Result{
				Stdout:    stdoutRes.data,
				Stderr:    stderrRes.data,
				Truncated: true,
				ExitCode:  stdoutRes.status,
				Restarted: restarted,
				Backend:   "wasm",
				Usage:     sandbox.Usage{Wall: wall},
			}, sandbox.ErrOutputLimit
		}

		s.mu.Lock()
		s.dead = true
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: %v", sandbox.ErrSessionKilled, outErr)
	}

	return &sandbox.Result{
		Stdout:    stdoutRes.data,
		Stderr:    stderrRes.data,
		ExitCode:  stdoutRes.status,
		Restarted: restarted,
		Backend:   "wasm",
		Usage:     sandbox.Usage{Wall: wall},
	}, nil
}

func (s *wasmSession) Reset(ctx context.Context) error {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return sandbox.ErrSessionClosed
	}

	s.cleanupRuntimeLocked()
	return s.startLocked(ctx)
}

func (s *wasmSession) Close() error {
	s.evalMu.Lock()
	defer s.evalMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.dead = true
	cancelSess := s.cancelSess
	stdinW := s.stdinWriter
	stdoutR := s.stdoutPipe
	stderrR := s.stderrPipe
	doneCh := s.doneCh
	runtime := s.runtime
	runDir := s.runDir
	s.runDir = ""
	s.mu.Unlock()

	if cancelSess != nil {
		cancelSess()
	}
	if stdinW != nil {
		_ = stdinW.Close()
	}
	if stdoutR != nil {
		_ = stdoutR.Close()
	}
	if stderrR != nil {
		_ = stderrR.Close()
	}
	if doneCh != nil {
		<-doneCh
	}
	if runtime != nil {
		_ = runtime.Close(context.Background())
	}
	if runDir != "" {
		_ = os.RemoveAll(runDir)
	}
	return nil
}

func (s *wasmSession) cleanupRuntimeLocked() {
	if s.cancelSess != nil {
		s.cancelSess()
	}
	if s.stdinWriter != nil {
		_ = s.stdinWriter.Close()
	}
	if s.stdoutPipe != nil {
		_ = s.stdoutPipe.Close()
	}
	if s.stderrPipe != nil {
		_ = s.stderrPipe.Close()
	}
	doneCh := s.doneCh
	runtime := s.runtime
	runDir := s.runDir
	s.runDir = ""

	s.mu.Unlock()
	if doneCh != nil {
		<-doneCh
	}
	if runtime != nil {
		_ = runtime.Close(context.Background())
	}
	if runDir != "" {
		_ = os.RemoveAll(runDir)
	}
	s.mu.Lock()
}

// readStdoutUntilSentinel reads stdout bytes until the sentinel marker:
// "\n" + token + " EXIT:" + status + "\n"
func readStdoutUntilSentinel(r io.Reader, token string, maxOutput uint64, cancel context.CancelFunc) ([]byte, int, error) {
	markerPrefix := "\n" + token + " EXIT:"
	markerPrefixBytes := []byte(markerPrefix)

	var buf bytes.Buffer
	chunk := make([]byte, 4096)

	for {
		n, err := r.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			if maxOutput != sandbox.Unlimited && uint64(buf.Len()) > maxOutput {
				cancel()
				return buf.Bytes()[:maxOutput], 0, sandbox.ErrOutputLimit
			}

			// Check for sentinel marker
			b := buf.Bytes()
			idx := bytes.Index(b, markerPrefixBytes)
			if idx >= 0 {
				after := b[idx+len(markerPrefixBytes):]
				nlIdx := bytes.IndexByte(after, '\n')
				if nlIdx >= 0 {
					statusStr := strings.TrimSpace(string(after[:nlIdx]))
					status, _ := strconv.Atoi(statusStr)
					payload := b[:idx]
					return payload, status, nil
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf.Bytes(), 1, fmt.Errorf("unexpected EOF before stdout sentinel: %w", io.ErrUnexpectedEOF)
			}
			return buf.Bytes(), 1, err
		}
	}
}

// readStderrUntilSentinel reads stderr bytes until the sentinel marker:
// "\n" + token + "\n"
func readStderrUntilSentinel(r io.Reader, token string, maxOutput uint64, cancel context.CancelFunc) ([]byte, error) {
	marker := "\n" + token + "\n"
	markerBytes := []byte(marker)

	var buf bytes.Buffer
	chunk := make([]byte, 4096)

	for {
		n, err := r.Read(chunk)
		if n > 0 {
			buf.Write(chunk[:n])
			if maxOutput != sandbox.Unlimited && uint64(buf.Len()) > maxOutput {
				cancel()
				return buf.Bytes()[:maxOutput], sandbox.ErrOutputLimit
			}

			b := buf.Bytes()
			idx := bytes.Index(b, markerBytes)
			if idx >= 0 {
				payload := b[:idx]
				return payload, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return buf.Bytes(), fmt.Errorf("unexpected EOF before stderr sentinel: %w", io.ErrUnexpectedEOF)
			}
			return buf.Bytes(), err
		}
	}
}

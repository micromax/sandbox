package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sync"
	"time"

	"github.com/micromax/sandbox"
)

var (
	_ sandbox.Backend        = (*Backend)(nil)
	_ sandbox.NetworkCapable = (*Backend)(nil)
	_ sandbox.ServeBackend   = (*Backend)(nil)
)

// Option configures the Docker backend.
type Option func(*Backend)

// WithSocket configures a custom Docker socket path or URL.
func WithSocket(socket string) Option {
	return func(b *Backend) {
		b.customSocket = socket
	}
}

// WithRuntime configures an alternative OCI runtime, e.g. "runsc" (gVisor).
func WithRuntime(runtime string) Option {
	return func(b *Backend) {
		b.runtime = runtime
	}
}

// Backend implements [sandbox.Backend] by executing code inside hardened containers.
type Backend struct {
	customSocket string
	runtime      string

	mu      sync.Mutex
	client  *Client
	reaper  *Reaper
	initErr error
}

// New creates a new Docker backend.
func New(opts ...Option) (*Backend, error) {
	b := &Backend{}
	for _, opt := range opts {
		opt(b)
	}

	// Probe daemon connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client, err := NewClient(ctx, b.customSocket)
	if err != nil {
		// Do not fail constructor; store init error so if routing policy prefers Wasm,
		// the application still starts smoothly and only errors if Docker is actually invoked.
		b.initErr = err
		return b, nil
	}

	b.client = client
	b.reaper = newReaper(client, 5*time.Minute)
	return b, nil
}

// Close halts the background orphan container reaper.
func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reaper != nil {
		b.reaper.Close()
		b.reaper = nil
	}
	return nil
}

// Name implements [sandbox.Backend].
func (b *Backend) Name() string {
	return "docker"
}

// Supports implements [sandbox.Backend].
func (b *Backend) Supports(p *sandbox.Pack) bool {
	return p != nil && p.Docker != nil
}

// SupportsNetwork implements [sandbox.NetworkCapable].
func (b *Backend) SupportsNetwork() bool {
	return true
}

// Run executes the request in an isolated, hardened container.
func (b *Backend) Run(ctx context.Context, req *sandbox.Request) (sandbox.Outcome, error) {
	if req.Pack == nil || req.Pack.Docker == nil {
		return sandbox.Outcome{}, fmt.Errorf("%w: pack %q has no docker spec", sandbox.ErrUnsupported, req.Pack.Name)
	}

	b.mu.Lock()
	client := b.client
	initErr := b.initErr
	b.mu.Unlock()

	if client == nil {
		return sandbox.Outcome{}, fmt.Errorf("%w: %v", sandbox.ErrBackendUnavailable, initErr)
	}

	dockerSpec := req.Pack.Docker
	image := dockerSpec.Image

	// 1. Ensure image is present
	present, err := client.ImageInspect(ctx, image)
	if err != nil && !present {
		return sandbox.Outcome{}, fmt.Errorf("checking image %s: %w", image, err)
	}
	if !present {
		fmt.Printf("[docker] Image %s not found locally, pulling (this only happens on first use)...\n", image)
		if err := client.ImagePull(ctx, image); err != nil {
			return sandbox.Outcome{}, fmt.Errorf("pulling image %s: %w", image, err)
		}
	}

	// 2. Prepare container name and security hardening profile
	randBytes := make([]byte, 8)
	_, _ = rand.Read(randBytes)
	containerName := fmt.Sprintf("sb-%s-%s", req.Pack.Name, hex.EncodeToString(randBytes))

	// Filesystem quota for writable tmpfs
	fsQuota := req.Limits.FSQuota
	if fsQuota == 0 || fsQuota == sandbox.Unlimited {
		fsQuota = 64 << 20 // 64 MiB default
	}

	// Memory limits
	memLimit := int64(req.Limits.Memory)
	if memLimit <= 0 || req.Limits.Memory == sandbox.Unlimited {
		memLimit = 512 << 20 // 512 MiB default
	}

	// Network mode
	netMode := "none"
	if req.Spec.Net != nil && len(req.Spec.Net.AllowHosts) > 0 {
		netMode = "bridge"
	}

	// Working directory & command
	workdir := dockerSpec.Workdir
	if workdir == "" {
		workdir = "/work"
	}

	cmd := make([]string, len(dockerSpec.Cmd))
	copy(cmd, dockerSpec.Cmd)
	if len(req.Spec.Args) > 0 {
		cmd = append(cmd, req.Spec.Args...)
	}

	envList := make([]string, 0, len(req.Spec.Env))
	for k, v := range req.Spec.Env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	cfg := ContainerCreateRequest{
		Image:          image,
		Cmd:            cmd,
		WorkingDir:     workdir,
		Env:            envList,
		Labels:         map[string]string{sandboxLabel: "1", "micromax.created": time.Now().Format(time.RFC3339)},
		NetworkMode:    netMode,
		ReadonlyRootfs: false,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Memory:         memLimit,
		MemorySwap:     memLimit, // disable swap beyond memory limit
		NanoCPUs:  int64(1_000_000_000),
		PidsLimit: int64(req.Limits.MaxProcs),
		Runtime:   b.runtime,
	}

	// 3. Create container
	containerID, err := client.ContainerCreate(ctx, containerName, cfg)
	if err != nil {
		return sandbox.Outcome{}, fmt.Errorf("creating container: %w", err)
	}
	defer func() {
		// Guaranteed cleanup
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		_ = client.ContainerKill(cleanCtx, containerID)
		_ = client.ContainerRemove(cleanCtx, containerID)
	}()

	// 4. Upload code and injected files into writable tmpfs
	workFiles := make(map[string][]byte)
	if req.Spec.Code != "" {
		codeName := "main"
		switch req.Pack.Name {
		case "go":
			codeName = "main.go"
		case "rust":
			codeName = "main.rs"
		case "java":
			codeName = "Main.java"
		case "node", "javascript", "js":
			codeName = "index.js"
		case "bash", "sh":
			codeName = "script.sh"
		}
		workFiles[codeName] = []byte(req.Spec.Code)
	}
	if len(workFiles) > 0 {
		tarData, err := buildArchive(workFiles, "")
		if err != nil {
			return sandbox.Outcome{}, fmt.Errorf("building work archive: %w", err)
		}
		if err := client.UploadArchive(ctx, containerID, "/work", tarData); err != nil {
			return sandbox.Outcome{}, fmt.Errorf("uploading work archive: %w", err)
		}
	}

	inFiles := req.FS.Snapshot("in")
	if len(inFiles) > 0 {
		tarData, err := buildArchive(inFiles, "")
		if err != nil {
			return sandbox.Outcome{}, fmt.Errorf("building in archive: %w", err)
		}
		if err := client.UploadArchive(ctx, containerID, "/in", tarData); err != nil {
			return sandbox.Outcome{}, fmt.Errorf("uploading in archive: %w", err)
		}
	}

	// 5. Start container
	if err := client.ContainerStart(ctx, containerID); err != nil {
		return sandbox.Outcome{}, fmt.Errorf("starting container: %w", err)
	}

	// 6. Wait for container to complete execution
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	waitCh := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := client.ContainerWait(runCtx, containerID)
		waitCh <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()

	var exitCode int
	var runErr error

	select {
	case waitRes := <-waitCh:
		exitCode = waitRes.code
		if waitRes.err != nil {
			runErr = waitRes.err
		}
	case <-runCtx.Done():
		if ctx.Err() != nil {
			runErr = sandbox.ErrTimeout
		} else {
			runErr = runCtx.Err()
		}
	}

	// 7. Collect output logs
	logCtx, cancelLog := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelLog()
	logErr := client.ContainerLogs(logCtx, containerID, req.Stdout, req.Stderr, req.Limits.MaxOutput, cancelRun)
	if errors.Is(logErr, sandbox.ErrOutputLimit) {
		runErr = sandbox.ErrOutputLimit
	}

	// Check if OOM killed
	inspectInfo, inspectErr := client.ContainerInspect(context.Background(), containerID)
	if inspectErr == nil && inspectInfo.State.OOMKilled {
		runErr = sandbox.ErrMemoryLimit
	}

	// 7. Collect output files from /out
	archiveReader, dlErr := client.DownloadArchive(context.Background(), containerID, "/out")
	if dlErr == nil && archiveReader != nil {
		defer archiveReader.Close()
		extracted, exErr := extractAndSanitizeArchive(archiveReader, req.Limits.FSQuota, req.Limits.MaxFiles)
		if exErr != nil && runErr == nil {
			runErr = exErr
		} else if exErr == nil {
			for name, data := range extracted {
				_ = req.FS.WriteFile(path.Join("out", name), data)
			}
		}
	}

	return sandbox.Outcome{
		ExitCode: exitCode,
	}, runErr
}

// Serve implements sandbox.ServeBackend for the Docker backend.
func (b *Backend) Serve(ctx context.Context, req *sandbox.ServeRequest) (sandbox.GuestService, error) {
	if req.Pack == nil || req.Pack.Docker == nil {
		return nil, fmt.Errorf("%w: pack %q has no docker spec", sandbox.ErrUnsupported, req.Pack.Name)
	}

	b.mu.Lock()
	client := b.client
	initErr := b.initErr
	b.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("%w: %v", sandbox.ErrBackendUnavailable, initErr)
	}

	dockerSpec := req.Pack.Docker
	image := dockerSpec.Image

	// 1. Ensure image is present
	present, err := client.ImageInspect(ctx, image)
	if err != nil && !present {
		return nil, fmt.Errorf("checking image %s: %w", image, err)
	}
	if !present {
		if err := client.ImagePull(ctx, image); err != nil {
			return nil, fmt.Errorf("pulling image %s: %w", image, err)
		}
	}

	// 2. Prepare container name and security hardening profile
	randBytes := make([]byte, 8)
	_, _ = rand.Read(randBytes)
	containerName := fmt.Sprintf("sb-serve-%s-%s", req.Pack.Name, hex.EncodeToString(randBytes))

	memLimit := int64(req.Limits.Memory)
	if memLimit <= 0 || req.Limits.Memory == sandbox.Unlimited {
		memLimit = 512 << 20
	}

	workdir := dockerSpec.Workdir
	if workdir == "" {
		workdir = "/work"
	}

	cmd := make([]string, len(dockerSpec.Cmd))
	copy(cmd, dockerSpec.Cmd)
	if len(req.Spec.Args) > 0 {
		cmd = append(cmd, req.Spec.Args...)
	}

	envList := make([]string, 0, len(req.Spec.Env))
	for k, v := range req.Spec.Env {
		envList = append(envList, fmt.Sprintf("%s=%s", k, v))
	}

	exposedPorts := make(map[string]struct{})
	portBindings := make(map[string][]PortBinding)
	for _, pm := range req.Ports {
		key := fmt.Sprintf("%d/tcp", pm.Guest)
		exposedPorts[key] = struct{}{}
		portBindings[key] = []PortBinding{
			{
				HostIP:   "127.0.0.1",
				HostPort: "0",
			},
		}
	}

	cfg := ContainerCreateRequest{
		Image:          image,
		Cmd:            cmd,
		WorkingDir:     workdir,
		Env:            envList,
		Labels:         map[string]string{sandboxLabel: "1", "micromax.created": time.Now().Format(time.RFC3339)},
		NetworkMode:    "bridge",
		ReadonlyRootfs: false,
		CapDrop:        []string{"ALL"},
		SecurityOpt:    []string{"no-new-privileges:true"},
		Memory:         memLimit,
		MemorySwap:     memLimit,
		NanoCPUs:       int64(1_000_000_000),
		PidsLimit:      int64(req.Limits.MaxProcs),
		Runtime:        b.runtime,
		ExposedPorts:   exposedPorts,
		PortBindings:   portBindings,
	}

	// 3. Create container
	containerID, err := client.ContainerCreate(ctx, containerName, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating serve container: %w", err)
	}

	cleanup := func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanCancel()
		_ = client.ContainerKill(cleanCtx, containerID)
		_ = client.ContainerRemove(cleanCtx, containerID)
	}

	// 4. Upload code and injected files
	workFiles := make(map[string][]byte)
	if req.Spec.Code != "" {
		codeName := "main"
		switch req.Pack.Name {
		case "go":
			codeName = "main.go"
		case "rust":
			codeName = "main.rs"
		case "java":
			codeName = "Main.java"
		case "node", "javascript", "js":
			codeName = "index.js"
		case "python":
			codeName = "main.py"
		case "bash", "sh":
			codeName = "script.sh"
		}
		workFiles[codeName] = []byte(req.Spec.Code)
	}
	if len(workFiles) > 0 {
		tarData, err := buildArchive(workFiles, "")
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("building work archive: %w", err)
		}
		if err := client.UploadArchive(ctx, containerID, "/work", tarData); err != nil {
			cleanup()
			return nil, fmt.Errorf("uploading work archive: %w", err)
		}
	}

	inFiles := req.FS.Snapshot("in")
	if len(inFiles) > 0 {
		tarData, err := buildArchive(inFiles, "")
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("building in archive: %w", err)
		}
		if err := client.UploadArchive(ctx, containerID, "/in", tarData); err != nil {
			cleanup()
			return nil, fmt.Errorf("uploading in archive: %w", err)
		}
	}

	// 5. Start container
	if err := client.ContainerStart(ctx, containerID); err != nil {
		cleanup()
		return nil, fmt.Errorf("starting serve container: %w", err)
	}

	// 6. Inspect container to resolve assigned host ports
	info, err := client.ContainerInspect(ctx, containerID)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("inspecting container for ports: %w", err)
	}

	portsMap := make(map[int]string)
	for _, pm := range req.Ports {
		key := fmt.Sprintf("%d/tcp", pm.Guest)
		bindings := info.NetworkSettings.Ports[key]
		if len(bindings) == 0 || bindings[0].HostPort == "" {
			cleanup()
			return nil, fmt.Errorf("guest port %d was not assigned a host port", pm.Guest)
		}
		portsMap[pm.Guest] = fmt.Sprintf("127.0.0.1:%s", bindings[0].HostPort)
	}

	svc := &dockerGuestService{
		client:      client,
		containerID: containerID,
		ports:       portsMap,
		waitCh:      make(chan struct{}),
	}

	// 7. Background log streamer
	if req.LogWriter != nil {
		go func() {
			_ = client.ContainerLogs(context.Background(), containerID, req.LogWriter, req.LogWriter, 0, nil)
		}()
	}

	// 8. Background wait goroutine
	go func() {
		code, waitErr := client.ContainerWait(context.Background(), containerID)
		svc.mu.Lock()
		svc.outcome = sandbox.Outcome{ExitCode: code}
		svc.err = waitErr
		svc.mu.Unlock()
		close(svc.waitCh)
	}()

	return svc, nil
}

type dockerGuestService struct {
	client      *Client
	containerID string
	ports       map[int]string
	waitCh      chan struct{}
	outcome     sandbox.Outcome
	err         error
	stopOnce    sync.Once
	mu          sync.RWMutex
}

func (s *dockerGuestService) PortMapping(guestPort int) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	addr, ok := s.ports[guestPort]
	if !ok {
		return "", fmt.Errorf("guest port %d is not mapped", guestPort)
	}
	return addr, nil
}

func (s *dockerGuestService) Handler() http.Handler {
	return nil
}

func (s *dockerGuestService) Wait() (sandbox.Outcome, error) {
	<-s.waitCh
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.outcome, s.err
}

func (s *dockerGuestService) Stop() error {
	s.stopOnce.Do(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.client.ContainerKill(cleanCtx, s.containerID)
		_ = s.client.ContainerRemove(cleanCtx, s.containerID)
	})
	return nil
}


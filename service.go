package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/micromax/sandbox/internal/ringbuf"
	"github.com/micromax/sandbox/proxy"
	"github.com/micromax/sandbox/vfs"
)

// BindMode specifies the host IP or interface for the service proxy.
type BindMode string

const (
	// Loopback binds the proxy to 127.0.0.1 (default, only reachable by the local host).
	Loopback BindMode = "127.0.0.1"

	// AllInterfaces binds the proxy to 0.0.0.0 (reachable from outside the host).
	AllInterfaces BindMode = "0.0.0.0"
)

// Interface specifies a custom IP or interface address to bind to.
func Interface(addr string) BindMode {
	return BindMode(addr)
}

// PortMap defines a port mapping between the guest and the host.
type PortMap struct {
	// Guest is the port number inside the guest container or runtime.
	Guest int

	// Host is the optional host port to bind to. If 0, an ephemeral port is chosen.
	Host int
}

// AuthOption configures authentication at the host proxy layer.
type AuthOption struct {
	Enabled bool
	Token   string
}

// BearerToken enables HTTP Bearer token authentication at the proxy layer.
// If token is empty (""), a cryptographically secure random token is generated.
func BearerToken(token string) AuthOption {
	if token == "" {
		token = generateAuthToken()
	}
	return AuthOption{
		Enabled: true,
		Token:   token,
	}
}

// NoAuth disables proxy-level authentication.
var NoAuth = AuthOption{Enabled: false}

// ReadinessProbe verifies that a service is ready to accept traffic before opening the host listener.
type ReadinessProbe interface {
	Check(ctx context.Context, targetAddr string, handler http.Handler) error
	Timeout() time.Duration
}

type tcpProbe struct {
	timeout time.Duration
}

// TCPReady creates a readiness probe that checks whether the guest port accepts TCP connections.
func TCPReady(timeout time.Duration) ReadinessProbe {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &tcpProbe{timeout: timeout}
}

func (p *tcpProbe) Timeout() time.Duration {
	return p.timeout
}

func (p *tcpProbe) Check(ctx context.Context, targetAddr string, handler http.Handler) error {
	if handler != nil {
		// In-process handler (Wasm) is ready immediately
		return nil
	}
	deadline := time.Now().Add(p.timeout)
	for {
		if time.Now().After(deadline) {
			return errors.New("TCP dial readiness probe timed out")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		conn, err := net.DialTimeout("tcp", targetAddr, 250*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type httpProbe struct {
	path    string
	timeout time.Duration
}

// HTTPReady creates a readiness probe that checks whether an HTTP GET request returns 2xx or 3xx status.
func HTTPReady(path string, timeout time.Duration) ReadinessProbe {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if path == "" {
		path = "/"
	}
	return &httpProbe{path: path, timeout: timeout}
}

func (p *httpProbe) Timeout() time.Duration {
	return p.timeout
}

func (p *httpProbe) Check(ctx context.Context, targetAddr string, handler http.Handler) error {
	deadline := time.Now().Add(p.timeout)
	for {
		if time.Now().After(deadline) {
			return errors.New("HTTP GET readiness probe timed out")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if handler != nil {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, p.path, nil)
			handler.ServeHTTP(rec, req)
			if rec.Code >= 200 && rec.Code < 400 {
				return nil
			}
		} else {
			url := fmt.Sprintf("http://%s%s", targetAddr, p.path)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err == nil {
				client := &http.Client{Timeout: 500 * time.Millisecond}
				resp, err := client.Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode >= 200 && resp.StatusCode < 400 {
						return nil
					}
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// ServeOpts configures how a service is exposed and managed.
type ServeOpts struct {
	// Ports specifies guest ports to expose on the host.
	Ports []PortMap

	// Bind specifies the host interface to bind to. Defaults to [Loopback] ("127.0.0.1").
	Bind BindMode

	// TTL is the maximum lifetime for this service. When it expires, the service is stopped.
	TTL time.Duration

	// Auth configures Bearer token authorization at the proxy layer.
	Auth AuthOption

	// Ready specifies the readiness probe. Defaults to [TCPReady](15*time.Second).
	Ready ReadinessProbe

	// Proxy limits
	MaxConnections int
	MaxRequestBody int64
	MaxHeaderBytes int
	IdleTimeout    time.Duration
}

// ServeRequest contains all parameters handed to a backend that supports Serve.
type ServeRequest struct {
	Pack      *Pack
	Spec      Spec
	Limits    Limits
	FS        *vfs.FS
	Ports     []PortMap
	LogWriter io.Writer
}

// GuestService is the backend handle for a running server process or container.
type GuestService interface {
	// PortMapping returns the host target address (e.g. "127.0.0.1:49152") for the guest port.
	PortMapping(guestPort int) (string, error)

	// Handler returns an in-process HTTP handler for Wasm bridge services, or nil for network containers.
	Handler() http.Handler

	// Wait blocks until the guest process or container terminates.
	Wait() (Outcome, error)

	// Stop terminates the guest process or container.
	Stop() error
}

// ServeBackend is implemented by backends capable of running background servers.
type ServeBackend interface {
	Serve(ctx context.Context, req *ServeRequest) (GuestService, error)
}

// Service represents an actively running and exposed guest service.
type Service struct {
	primaryURL string
	urls       map[int]string
	addrs      map[int]string
	token      string
	logs       *ringbuf.RingBuffer
	proxies    []*proxy.Proxy
	guest      GuestService

	doneCh    chan struct{}
	closeOnce sync.Once
	stopErr   error
	mu        sync.RWMutex
}

// URL returns the default primary HTTP URL for accessing the service.
func (s *Service) URL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.primaryURL
}

// URLFor returns the HTTP URL exposed for a specific guest port.
func (s *Service) URLFor(guestPort int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.urls[guestPort]
}

// Addr returns the host "ip:port" address for the given guest port.
func (s *Service) Addr(guestPort int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.addrs[guestPort]
}

// Token returns the Bearer auth token if one was configured or generated.
func (s *Service) Token() string {
	return s.token
}

// Logs returns an io.Reader streaming the recent captured guest logs.
func (s *Service) Logs() io.Reader {
	return s.logs.Reader()
}

// RecentLogs returns a string snapshot of recent stdout/stderr output from the guest.
func (s *Service) RecentLogs() string {
	return s.logs.String()
}

// Done returns a channel that is closed when the service stops, exits, or times out.
func (s *Service) Done() <-chan struct{} {
	return s.doneCh
}

// Err returns the error that caused the service to terminate, if any.
func (s *Service) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stopErr
}

// Stop shuts down the host proxies and stops the underlying guest process.
func (s *Service) Stop() error {
	s.closeOnce.Do(func() {
		// 1. Close host listeners so no new connections are accepted
		for _, p := range s.proxies {
			_ = p.Close()
		}

		// 2. Stop guest process
		if s.guest != nil {
			_ = s.guest.Stop()
		}

		close(s.doneCh)
	})
	return nil
}

// Serve runs a server inside the sandbox and safely exposes it on the host machine.
func (s *Sandbox) Serve(ctx context.Context, spec Spec, opts ServeOpts) (*Service, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	pack, err := s.lookup(spec.Lang)
	if err != nil {
		return nil, err
	}
	lim, err := resolveLimits(s.defaults, spec.Limits)
	if err != nil {
		return nil, err
	}

	// Default ports
	if len(opts.Ports) == 0 {
		opts.Ports = []PortMap{{Guest: 8000}}
	}

	// Security default: Loopback
	if opts.Bind == "" {
		opts.Bind = Loopback
	} else if opts.Bind != Loopback {
		log.Printf("sandbox: WARNING: Service bound to non-loopback interface %q; service is exposed to the local network", opts.Bind)
	}

	if opts.Ready == nil {
		opts.Ready = TCPReady(15 * time.Second)
	}

	backend, err := s.route(pack)
	if err != nil {
		return nil, err
	}

	serveBackend, ok := backend.(ServeBackend)
	if !ok {
		return nil, fmt.Errorf("%w: backend %q does not support Serve (supported for servers in Docker, or handler-style apps in Wasm)",
			ErrUnsupported, backend.Name())
	}

	fsys, err := newWorkspace(lim, spec.Files)
	if err != nil {
		return nil, err
	}

	logRing := ringbuf.New(256 * 1024)

	serveReq := &ServeRequest{
		Pack:      pack,
		Spec:      spec,
		Limits:    lim,
		FS:        fsys,
		Ports:     opts.Ports,
		LogWriter: logRing,
	}

	guest, err := serveBackend.Serve(ctx, serveReq)
	if err != nil {
		return nil, err
	}

	// Check readiness for primary port before opening host listeners
	primaryPort := opts.Ports[0].Guest
	var targetAddr string
	var handler http.Handler

	if guest.Handler() != nil {
		handler = guest.Handler()
	} else {
		targetAddr, err = guest.PortMapping(primaryPort)
		if err != nil {
			_ = guest.Stop()
			return nil, fmt.Errorf("resolving target address for guest port %d: %w", primaryPort, err)
		}
	}

	// Probe readiness
	probeCtx, cancelProbe := context.WithTimeout(ctx, opts.Ready.Timeout())
	defer cancelProbe()

	if err := opts.Ready.Check(probeCtx, targetAddr, handler); err != nil {
		_ = guest.Stop()
		recentLogs := logRing.String()
		return nil, fmt.Errorf("service failed readiness check: %w\nGuest logs:\n%s", err, recentLogs)
	}

	// Open host proxies for each configured port
	proxies := make([]*proxy.Proxy, 0, len(opts.Ports))
	urls := make(map[int]string)
	addrs := make(map[int]string)

	proxyLimits := proxy.Limits{
		MaxConnections: opts.MaxConnections,
		MaxRequestBody: opts.MaxRequestBody,
		MaxHeaderBytes: opts.MaxHeaderBytes,
		IdleTimeout:    opts.IdleTimeout,
	}

	for _, pm := range opts.Ports {
		var pTarget string
		var pHandler http.Handler

		if guest.Handler() != nil {
			pHandler = guest.Handler()
		} else {
			pTarget, err = guest.PortMapping(pm.Guest)
			if err != nil {
				for _, p := range proxies {
					_ = p.Close()
				}
				_ = guest.Stop()
				return nil, fmt.Errorf("resolving target for port %d: %w", pm.Guest, err)
			}
		}

		px, err := proxy.New(proxy.Config{
			Mode:          proxy.ModeHTTP,
			Bind:          string(opts.Bind),
			Port:          pm.Host,
			Target:        pTarget,
			TargetHandler: pHandler,
			AuthToken:     opts.Auth.Token,
			Limits:        proxyLimits,
		})
		if err != nil {
			for _, p := range proxies {
				_ = p.Close()
			}
			_ = guest.Stop()
			return nil, fmt.Errorf("starting proxy for port %d: %w", pm.Guest, err)
		}

		proxies = append(proxies, px)
		urls[pm.Guest] = px.URL()
		addrs[pm.Guest] = fmt.Sprintf("%s:%d", opts.Bind, px.Port())
	}

	svc := &Service{
		primaryURL: urls[primaryPort],
		urls:       urls,
		addrs:      addrs,
		token:      opts.Auth.Token,
		logs:       logRing,
		proxies:    proxies,
		guest:      guest,
		doneCh:     make(chan struct{}),
	}

	// TTL timer
	if opts.TTL > 0 {
		time.AfterFunc(opts.TTL, func() {
			svc.mu.Lock()
			if svc.stopErr == nil {
				svc.stopErr = ErrTimeout
			}
			svc.mu.Unlock()
			_ = svc.Stop()
		})
	}

	// Background monitor for guest exit or context cancellation
	go func() {
		defer svc.Stop()

		waitCh := make(chan error, 1)
		go func() {
			_, waitErr := guest.Wait()
			waitCh <- waitErr
		}()

		select {
		case <-ctx.Done():
			svc.mu.Lock()
			if svc.stopErr == nil {
				svc.stopErr = ctx.Err()
			}
			svc.mu.Unlock()
		case waitErr := <-waitCh:
			svc.mu.Lock()
			if svc.stopErr == nil && waitErr != nil {
				svc.stopErr = waitErr
			}
			svc.mu.Unlock()
		case <-svc.doneCh:
		}
	}()

	return svc, nil
}

func generateAuthToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tok_%x", time.Now().UnixNano())
	}
	return "sb_" + hex.EncodeToString(b)
}

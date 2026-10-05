package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Mode determines how incoming traffic is inspected and forwarded.
type Mode string

const (
	// ModeHTTP inspects HTTP requests, enforces request limits, checks authorization,
	// and supports WebSocket upgrades.
	ModeHTTP Mode = "http"

	// ModeTCP provides stream-level forwarding without protocol inspection.
	ModeTCP Mode = "tcp"
)

// Config configures a Proxy instance.
type Config struct {
	// Mode is the operating mode (ModeHTTP or ModeTCP). Defaults to ModeHTTP.
	Mode Mode

	// Bind is the local IP or interface address to bind the listener to.
	// Defaults to "127.0.0.1" (loopback).
	Bind string

	// Port is the local port to listen on. If 0, an ephemeral port is selected.
	Port int

	// Target is the backend network address (e.g. "127.0.0.1:49153") to forward to.
	// Required for ModeTCP, and for ModeHTTP when TargetHandler is nil.
	Target string

	// TargetHandler allows routing HTTP requests directly to an in-process handler
	// (useful for Wasm handler bridges without intermediate sockets).
	TargetHandler http.Handler

	// AuthToken is an optional bearer token. If set, HTTP requests must include
	// "Authorization: Bearer <token>" or they receive a 401 Unauthorized response.
	AuthToken string

	// Limits specifies resource and timeout limits.
	Limits Limits
}

// Proxy listens on a host port and forwards traffic to a sandbox guest service.
type Proxy struct {
	cfg        Config
	listener   net.Listener
	httpServer *http.Server

	activeConns int64
	closed      chan struct{}
	closeOnce   sync.Once
	closeErr    error

	// Track connections for clean teardown
	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

// New creates and starts a Proxy according to cfg.
func New(cfg Config) (*Proxy, error) {
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1"
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeHTTP
	}
	cfg.Limits = resolveLimits(cfg.Limits)

	if cfg.Mode == ModeTCP && cfg.Target == "" {
		return nil, errors.New("proxy: Target is required for ModeTCP")
	}
	if cfg.Mode == ModeHTTP && cfg.Target == "" && cfg.TargetHandler == nil {
		return nil, errors.New("proxy: Target or TargetHandler is required for ModeHTTP")
	}

	addr := fmt.Sprintf("%s:%d", cfg.Bind, cfg.Port)
	baseLn, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("proxy: listen on %s failed: %w", addr, err)
	}

	p := &Proxy{
		cfg:      cfg,
		closed:   make(chan struct{}),
		conns:    make(map[net.Conn]struct{}),
	}

	// Wrap listener with connection limiter and tracker
	limitLn := newLimitListener(baseLn, cfg.Limits.MaxConnections, p)
	p.listener = limitLn

	if cfg.Mode == ModeHTTP {
		if err := p.startHTTP(); err != nil {
			baseLn.Close()
			return nil, err
		}
	} else {
		p.startTCP()
	}

	return p, nil
}

// Addr returns the listener's network address.
func (p *Proxy) Addr() net.Addr {
	return p.listener.Addr()
}

// Port returns the port number the proxy is listening on.
func (p *Proxy) Port() int {
	return p.listener.Addr().(*net.TCPAddr).Port
}

// URL returns the base HTTP URL (e.g. "http://127.0.0.1:49213").
func (p *Proxy) URL() string {
	tcpAddr := p.listener.Addr().(*net.TCPAddr)
	host := tcpAddr.IP.String()
	if tcpAddr.IP.To4() == nil {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%d", host, tcpAddr.Port)
}

// ActiveConns returns the current number of active client connections.
func (p *Proxy) ActiveConns() int {
	return int(atomic.LoadInt64(&p.activeConns))
}

// Close shuts down the proxy listener and terminates all active connections.
func (p *Proxy) Close() error {
	p.closeOnce.Do(func() {
		close(p.closed)
		var errs []error

		if p.httpServer != nil {
			// Gracefully shut down HTTP server with short timeout
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := p.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, err)
			}
		}

		if err := p.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}

		// Close any remaining active connections
		p.mu.Lock()
		for c := range p.conns {
			_ = c.Close()
		}
		p.conns = make(map[net.Conn]struct{})
		p.mu.Unlock()

		if len(errs) > 0 {
			p.closeErr = errs[0]
		}
	})
	return p.closeErr
}

func (p *Proxy) trackConn(c net.Conn) net.Conn {
	atomic.AddInt64(&p.activeConns, 1)
	p.mu.Lock()
	p.conns[c] = struct{}{}
	p.mu.Unlock()

	return &trackedConn{
		Conn: c,
		onClose: func() {
			atomic.AddInt64(&p.activeConns, -1)
			p.mu.Lock()
			delete(p.conns, c)
			p.mu.Unlock()
		},
	}
}

type trackedConn struct {
	net.Conn
	onClose   func()
	closeOnce sync.Once
}

func (c *trackedConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.onClose()
		err = c.Conn.Close()
	})
	return err
}

type limitListener struct {
	net.Listener
	sem       chan struct{}
	proxy     *Proxy
	closeOnce sync.Once
	done      chan struct{}
}

func newLimitListener(l net.Listener, max int, p *Proxy) net.Listener {
	return &limitListener{
		Listener: l,
		sem:      make(chan struct{}, max),
		proxy:    p,
		done:     make(chan struct{}),
	}
}

func (l *limitListener) acquire() bool {
	select {
	case <-l.done:
		return false
	case l.sem <- struct{}{}:
		return true
	}
}

func (l *limitListener) release() {
	select {
	case <-l.sem:
	default:
	}
}

func (l *limitListener) Accept() (net.Conn, error) {
	if !l.acquire() {
		return nil, net.ErrClosed
	}

	c, err := l.Listener.Accept()
	if err != nil {
		l.release()
		return nil, err
	}

	tracked := l.proxy.trackConn(c)
	return &limitConn{
		Conn:    tracked,
		release: l.release,
	}, nil
}

func (l *limitListener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		close(l.done)
		err = l.Listener.Close()
	})
	return err
}

type limitConn struct {
	net.Conn
	release   func()
	closeOnce sync.Once
}

func (c *limitConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.release()
		err = c.Conn.Close()
	})
	return err
}

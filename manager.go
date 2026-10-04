package sandbox

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrManagerClosed is returned when an operation is performed on a closed manager.
	ErrManagerClosed = errors.New("sandbox: session manager is closed")
)

// ManagerOpts configures the session manager.
type ManagerOpts struct {
	// MaxSessions is the maximum number of concurrent active sessions. Defaults to 64.
	MaxSessions int

	// Idle is the default inactivity timeout for sessions. Defaults to 5 minutes.
	Idle time.Duration

	// MaxLifetime is the maximum allowed duration of a session. Defaults to 1 hour.
	MaxLifetime time.Duration

	// ReapInterval is how frequently the background reaper checks for expired/idle sessions.
	// Defaults to 10 seconds.
	ReapInterval time.Duration

	// DefaultLimits specifies default resource limits for sessions created by this manager.
	DefaultLimits Limits

	// AutoRestart configures whether created sessions automatically restart if killed.
	AutoRestart bool
}

// Manager coordinates concurrent interactive sessions, enforcing capacity caps,
// idle pruning, and clean shutdown without resource leaks.
type Manager struct {
	sb   *Sandbox
	opts ManagerOpts

	mu       sync.RWMutex
	sessions map[string]*managedSession
	closed   bool
	stopReap chan struct{}
	reapDone chan struct{}
}

// NewManager creates a session manager backed by the given sandbox.
func NewManager(sb *Sandbox, opts ManagerOpts) *Manager {
	if opts.MaxSessions <= 0 {
		opts.MaxSessions = 64
	}
	if opts.Idle <= 0 {
		opts.Idle = 5 * time.Minute
	}
	if opts.MaxLifetime <= 0 {
		opts.MaxLifetime = 1 * time.Hour
	}
	if opts.ReapInterval <= 0 {
		opts.ReapInterval = 10 * time.Second
	}

	m := &Manager{
		sb:       sb,
		opts:     opts,
		sessions: make(map[string]*managedSession),
		stopReap: make(chan struct{}),
		reapDone: make(chan struct{}),
	}

	go m.reaper()
	return m
}

// Create spawns a new session for the given language.
// Returns [ErrTooManySessions] if the maximum session capacity has been reached.
func (m *Manager) Create(ctx context.Context, lang string, opts ...SessionOpt) (Session, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrManagerClosed
	}
	if len(m.sessions) >= m.opts.MaxSessions {
		m.mu.Unlock()
		return nil, ErrTooManySessions
	}
	m.mu.Unlock()

	// Merge manager defaults
	mergedOpts := []SessionOpt{
		WithIdleTimeout(m.opts.Idle),
		WithMaxLifetime(m.opts.MaxLifetime),
		WithAutoRestart(m.opts.AutoRestart),
	}
	if m.opts.DefaultLimits != (Limits{}) {
		mergedOpts = append(mergedOpts, WithSessionLimits(m.opts.DefaultLimits))
	}
	mergedOpts = append(mergedOpts, opts...)

	rawSess, err := m.sb.NewSession(ctx, lang, mergedOpts...)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	ms := &managedSession{
		Session:      rawSess,
		mgr:          m,
		createdAt:    now,
		lastActivity: now,
		idleTimeout:  m.opts.Idle,
		maxLifetime:  m.opts.MaxLifetime,
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		_ = rawSess.Close()
		return nil, ErrManagerClosed
	}
	if len(m.sessions) >= m.opts.MaxSessions {
		_ = rawSess.Close()
		return nil, ErrTooManySessions
	}

	m.sessions[rawSess.ID()] = ms
	return ms, nil
}

// Get returns the session with the specified ID, or false if not found.
func (m *Manager) Get(id string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, false
	}
	s, ok := m.sessions[id]
	return s, ok
}

// Close explicitly terminates and unregisters the session with the specified ID.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrManagerClosed
	}
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return ErrSessionNotFound
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	return s.Session.Close()
}

// Count returns the number of active sessions currently managed.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Shutdown cleanly terminates all active sessions and halts the background reaper.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	close(m.stopReap)

	toClose := make([]*managedSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		toClose = append(toClose, s)
	}
	m.sessions = make(map[string]*managedSession)
	m.mu.Unlock()

	<-m.reapDone

	var firstErr error
	for _, s := range toClose {
		if err := s.Session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// reaper periodically checks for expired or idle sessions and frees them.
func (m *Manager) reaper() {
	defer close(m.reapDone)
	ticker := time.NewTicker(m.opts.ReapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopReap:
			return
		case now := <-ticker.C:
			m.reapExpired(now)
		}
	}
}

func (m *Manager) reapExpired(now time.Time) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}

	var expired []*managedSession
	for id, s := range m.sessions {
		s.mu.Lock()
		isIdle := s.idleTimeout > 0 && now.Sub(s.lastActivity) > s.idleTimeout
		isLifetimeExpired := s.maxLifetime > 0 && now.Sub(s.createdAt) > s.maxLifetime
		s.mu.Unlock()

		if isIdle || isLifetimeExpired {
			expired = append(expired, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range expired {
		_ = s.Session.Close()
	}
}

// managedSession wraps a Session to track access times and synchronize with Manager.
type managedSession struct {
	Session
	mgr *Manager

	mu           sync.Mutex
	createdAt    time.Time
	lastActivity time.Time
	evalCount    int
	idleTimeout  time.Duration
	maxLifetime  time.Duration
	closed       bool
}

func (s *managedSession) Eval(ctx context.Context, code string) (*Result, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrSessionClosed
	}
	if s.maxLifetime > 0 && time.Since(s.createdAt) > s.maxLifetime {
		s.closed = true
		s.mu.Unlock()
		_ = s.Close()
		return nil, fmt.Errorf("%w: session max lifetime reached", ErrSessionClosed)
	}
	s.lastActivity = time.Now()
	s.evalCount++
	s.mu.Unlock()

	res, err := s.Session.Eval(ctx, code)

	s.mu.Lock()
	s.lastActivity = time.Now()
	s.mu.Unlock()

	return res, err
}

func (s *managedSession) Reset(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrSessionClosed
	}
	s.lastActivity = time.Now()
	s.mu.Unlock()

	return s.Session.Reset(ctx)
}

func (s *managedSession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	// Remove from manager
	s.mgr.mu.Lock()
	delete(s.mgr.sessions, s.ID())
	s.mgr.mu.Unlock()

	return s.Session.Close()
}

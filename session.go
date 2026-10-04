package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrSessionClosed is returned when an operation is attempted on a closed session.
	ErrSessionClosed = errors.New("sandbox: session is closed")

	// ErrTooManySessions is returned by Manager when the maximum number of active sessions is reached.
	ErrTooManySessions = errors.New("sandbox: too many concurrent sessions")

	// ErrSessionNotFound is returned when a session ID is not registered in the manager.
	ErrSessionNotFound = errors.New("sandbox: session not found")
)

// Session represents a stateful interactive interpreter session (REPL).
// Variables, imports, and definitions persist across successive [Session.Eval] calls.
// Calls to Eval on the same Session are serialized.
type Session interface {
	// ID returns the unique identifier for this session.
	ID() string

	// Lang returns the canonical language name of this session.
	Lang() string

	// Eval executes code in the session and returns the execution result.
	// If the eval times out or exceeds resource limits without AutoRestart,
	// the session is killed and subsequent calls return [ErrSessionKilled].
	Eval(ctx context.Context, code string) (*Result, error)

	// Reset clears the session state or restarts the guest interpreter instance.
	Reset(ctx context.Context) error

	// Close terminates the session, killing the interpreter and freeing resources.
	Close() error

	// Alive reports whether the session interpreter is currently alive and accepting evals.
	Alive() bool
}

// SessionConfig configures an interactive session.
type SessionConfig struct {
	// ID is the session identifier. If empty, a cryptographically secure random ID is assigned.
	ID string

	// Limits holds resource limits. Zero values receive sensible defaults.
	Limits Limits

	// AutoRestart determines whether a killed session (e.g. on timeout) is automatically
	// recreated on the next Eval. When true, Result.Restarted is set to true on the first eval after restart.
	AutoRestart bool

	// IdleTimeout is how long the session can remain without an Eval call before being closed.
	IdleTimeout time.Duration

	// MaxLifetime is the hard ceiling on total session duration from creation.
	MaxLifetime time.Duration

	// MaxEvals is the maximum number of Eval calls allowed before the session closes.
	MaxEvals int

	// Env specifies environment variables passed to the session runtime.
	Env map[string]string
}

// SessionOpt configures session creation options.
type SessionOpt func(*SessionConfig)

// WithSessionID sets an explicit ID for the session.
func WithSessionID(id string) SessionOpt {
	return func(c *SessionConfig) {
		c.ID = id
	}
}

// WithSessionLimits overrides the default resource limits for this session.
func WithSessionLimits(l Limits) SessionOpt {
	return func(c *SessionConfig) {
		c.Limits = l
	}
}

// WithAutoRestart configures whether the session automatically restarts after being killed.
func WithAutoRestart(auto bool) SessionOpt {
	return func(c *SessionConfig) {
		c.AutoRestart = auto
	}
}

// WithIdleTimeout sets the inactivity duration before an idle session is eligible for reaping.
func WithIdleTimeout(d time.Duration) SessionOpt {
	return func(c *SessionConfig) {
		c.IdleTimeout = d
	}
}

// WithMaxLifetime sets the maximum total lifetime of the session.
func WithMaxLifetime(d time.Duration) SessionOpt {
	return func(c *SessionConfig) {
		c.MaxLifetime = d
	}
}

// WithMaxEvals sets the maximum number of Eval operations permitted on this session.
func WithMaxEvals(n int) SessionOpt {
	return func(c *SessionConfig) {
		c.MaxEvals = n
	}
}

// WithSessionEnv sets environment variables available to the session interpreter.
func WithSessionEnv(env map[string]string) SessionOpt {
	return func(c *SessionConfig) {
		c.Env = env
	}
}

// SessionRequest contains the parameters for starting a session on a backend.
type SessionRequest struct {
	Pack   *Pack
	Config SessionConfig
}

// SessionBackend is an optional interface implemented by backends capable of running stateful sessions.
type SessionBackend interface {
	NewSession(ctx context.Context, req *SessionRequest) (Session, error)
}

// generateSessionID produces an unguessable 128-bit random identifier.
func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-seeded format if crypto rand fails
		return fmt.Sprintf("sess_%x", time.Now().UnixNano())
	}
	return "sess_" + hex.EncodeToString(b)
}

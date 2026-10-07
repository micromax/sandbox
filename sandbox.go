package sandbox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/micromax/sandbox/internal/limitbuf"
	"github.com/micromax/sandbox/vfs"
)

// Guest-visible base directories.
const (
	dirIn   = "in"   // injected files, read-only for the guest
	dirWork = "work" // scratch space
	dirOut  = "out"  // files collected into Result.Files
)

// Sandbox runs code through registered backends. Create one with [New]. It is
// safe for concurrent use.
type Sandbox struct {
	packs    map[string]*Pack // canonical names and aliases
	backends []Backend
	policy   Policy
	defaults Limits
}

type config struct {
	packs    []*Pack
	backends []Backend
	policy   Policy
	defaults Limits
}

// Option configures [New].
type Option func(*config)

// WithPacks registers language packs.
func WithPacks(packs ...*Pack) Option {
	return func(c *config) { c.packs = append(c.packs, packs...) }
}

// WithBackends registers isolation backends.
func WithBackends(backends ...Backend) Option {
	return func(c *config) { c.backends = append(c.backends, backends...) }
}

// WithPolicy sets the routing policy. The default is [PreferWasm].
func WithPolicy(p Policy) Option {
	return func(c *config) { c.policy = p }
}

// WithDefaultLimits overrides the built-in defaults field by field. Fields
// left zero keep the built-in value.
func WithDefaultLimits(l Limits) Option {
	return func(c *config) { c.defaults = l }
}

// New creates a Sandbox. It validates every pack and limit up front so
// misconfiguration fails at startup rather than during a run.
func New(opts ...Option) (*Sandbox, error) {
	var c config
	for _, o := range opts {
		o(&c)
	}
	if len(c.policy.Order) == 0 {
		c.policy = PreferWasm
	}
	defaults := mergeLimits(DefaultLimits(), c.defaults)
	if err := defaults.Validate(); err != nil {
		return nil, err
	}

	s := &Sandbox{
		packs:    map[string]*Pack{},
		backends: nil,
		policy:   Policy{Order: append([]string(nil), c.policy.Order...)},
		defaults: defaults,
	}

	seen := map[string]bool{}
	for _, b := range c.backends {
		if b == nil {
			return nil, errors.New("sandbox: nil backend")
		}
		if seen[b.Name()] {
			return nil, fmt.Errorf("sandbox: backend %q registered twice", b.Name())
		}
		seen[b.Name()] = true
		s.backends = append(s.backends, b)
	}

	for _, p := range c.packs {
		if err := p.Validate(); err != nil {
			return nil, err
		}
		for _, name := range append([]string{p.Name}, p.Aliases...) {
			if prev, dup := s.packs[name]; dup {
				return nil, fmt.Errorf("%w: name %q is used by both %q and %q",
					ErrInvalidPack, name, prev.Name, p.Name)
			}
			s.packs[name] = p
		}
	}
	return s, nil
}

// Languages returns the canonical names of all registered packs, sorted.
func (s *Sandbox) Languages() []string {
	set := map[string]bool{}
	for _, p := range s.packs {
		set[p.Name] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (s *Sandbox) lookup(lang string) (*Pack, error) {
	p, ok := s.packs[strings.ToLower(strings.TrimSpace(lang))]
	if !ok {
		return nil, fmt.Errorf("%w: %q (registered: %s)", ErrUnknownLanguage, lang,
			strings.Join(s.Languages(), ", "))
	}
	return p, nil
}

// route picks the first backend named by the policy that supports the pack.
// Backends outside the policy are never considered.
func (s *Sandbox) route(p *Pack) (Backend, error) {
	for _, name := range s.policy.Order {
		for _, b := range s.backends {
			if b.Name() == name && b.Supports(p) {
				return b, nil
			}
		}
	}
	var registered []string
	for _, b := range s.backends {
		registered = append(registered, b.Name())
	}
	return nil, fmt.Errorf("%w: language %q, policy [%s], registered backends [%s]",
		ErrBackendUnavailable, p.Name, s.policy, strings.Join(registered, ", "))
}

func validateSpec(spec Spec) error {
	if strings.TrimSpace(spec.Lang) == "" {
		return fmt.Errorf("%w: Lang is required", ErrInvalidSpec)
	}
	for k, v := range spec.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.Contains(v, "\x00") {
			return fmt.Errorf("%w: invalid environment variable %q", ErrInvalidSpec, k)
		}
	}
	for _, a := range spec.Args {
		if strings.Contains(a, "\x00") {
			return fmt.Errorf("%w: argument contains NUL", ErrInvalidSpec)
		}
	}
	return nil
}

// newWorkspace builds the guest filesystem: /in, /work, /out and the
// injected files. Injected files go through [vfs.Clean], so a hostile key
// such as "../x" is rejected before any backend runs.
func newWorkspace(lim Limits, files map[string][]byte) (*vfs.FS, error) {
	fsys := vfs.New(lim.FSQuota, lim.MaxFiles)
	for _, d := range []string{dirIn, dirWork, dirOut} {
		if err := fsys.MkdirAll(d); err != nil {
			return nil, err
		}
	}
	// Deterministic order so quota failures are reproducible.
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		clean, err := vfs.Clean(n)
		if err != nil {
			return nil, fmt.Errorf("%w: file %q: %w", ErrInvalidSpec, n, err)
		}
		if clean == "." {
			return nil, fmt.Errorf("%w: file name %q refers to the directory itself", ErrInvalidSpec, n)
		}
		if err := fsys.WriteFile(dirIn+"/"+clean, files[n]); err != nil {
			return nil, fmt.Errorf("injecting file %q: %w", n, err)
		}
	}
	return fsys, nil
}

// Run executes spec once and returns what it produced.
//
// A non-zero guest exit code is not an error. Resource-limit violations
// return a non-nil partial Result together with the matching error
// ([ErrTimeout], [ErrMemoryLimit], [ErrOutputLimit], [ErrFSQuota]). Any other
// failure returns a nil Result.
func (s *Sandbox) Run(ctx context.Context, spec Spec) (*Result, error) {
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
	backend, err := s.route(pack)
	if err != nil {
		return nil, err
	}
	if spec.Net != nil {
		nc, ok := backend.(NetworkCapable)
		if !ok || !nc.SupportsNetwork() {
			return nil, fmt.Errorf("%w: backend %q cannot enforce a network policy",
				ErrUnsupported, backend.Name())
		}
	}

	fsys, err := newWorkspace(lim, spec.Files)
	if err != nil {
		return nil, err
	}

	group := limitbuf.NewGroup(lim.MaxOutput, ErrOutputLimit)
	stdout, stderr := group.NewWriter(), group.NewWriter()
	stdin := spec.Stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}

	runCtx := ctx
	if lim.WallTime != UnlimitedTime {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, lim.WallTime)
		defer cancel()
	}

	req := &Request{
		Pack:   pack,
		Spec:   spec,
		Limits: lim,
		FS:     fsys,
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}

	start := time.Now()
	outcome, runErr := backend.Run(runCtx, req)
	wall := time.Since(start)

	// If output limit was exceeded, prioritize ErrOutputLimit
	if group.Truncated() {
		runErr = ErrOutputLimit
	} else if runErr != nil && !isLimitError(runErr) &&
		runCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		runErr = fmt.Errorf("%w (%w): backend reported: %v", ErrTimeout, context.DeadlineExceeded, runErr)
	}

	st := fsys.Stats()
	res := &Result{
		Stdout:    stdout.Bytes(),
		Stderr:    stderr.Bytes(),
		Truncated: group.Truncated(),
		ExitCode:  outcome.ExitCode,
		Files:     fsys.Snapshot(dirOut),
		Usage: Usage{
			Wall:       wall,
			PeakMemory: outcome.PeakMemory,
			FSBytes:    st.Bytes,
			FSFiles:    st.Files + st.Dirs,
		},
		Backend: backend.Name(),
		NetLog:  outcome.NetLog,
	}

	switch {
	case runErr == nil:
		return res, nil
	case isLimitError(runErr):
		return res, runErr
	default:
		return nil, runErr
	}
}

// NewSession starts a stateful interactive REPL session for the given language.
func (s *Sandbox) NewSession(ctx context.Context, lang string, opts ...SessionOpt) (Session, error) {
	pack, err := s.lookup(lang)
	if err != nil {
		return nil, err
	}
	if !pack.Caps.Sessions {
		return nil, fmt.Errorf("%w: language %q does not support sessions", ErrUnsupported, pack.Name)
	}

	backend, err := s.route(pack)
	if err != nil {
		return nil, err
	}

	sessionBackend, ok := backend.(SessionBackend)
	if !ok {
		return nil, fmt.Errorf("%w: backend %q does not support sessions", ErrUnsupported, backend.Name())
	}

	cfg := SessionConfig{
		ID:          generateSessionID(),
		Limits:      s.defaults,
		IdleTimeout: 5 * time.Minute,
		MaxLifetime: 1 * time.Hour,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	resolvedLim, err := resolveLimits(s.defaults, &cfg.Limits)
	if err != nil {
		return nil, err
	}
	cfg.Limits = resolvedLim

	req := &SessionRequest{
		Pack:   pack,
		Config: cfg,
	}
	return sessionBackend.NewSession(ctx, req)
}


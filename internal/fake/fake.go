// Package fake is an in-process backend used only to test the sandbox core.
//
// It provides NO isolation and runs hard-coded toy "languages". It lives
// under internal/ so it can never be imported by users of the library.
package fake

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/micromax/sandbox"
)

// Backend is the fake backend. Its name is "fake".
type Backend struct{}

// Name implements sandbox.Backend.
func (Backend) Name() string { return "fake" }

// Supports implements sandbox.Backend.
func (Backend) Supports(p *sandbox.Pack) bool {
	_, ok := p.Extra["fake"]
	return ok
}

// Pack returns a pack for one of the toy languages.
func Pack(name string) *sandbox.Pack {
	return &sandbox.Pack{
		Name:  name,
		Extra: map[string]any{"fake": true},
		Caps:  sandbox.Capabilities{Sessions: true},
	}
}

// NewSession implements sandbox.SessionBackend for fake backend.
func (Backend) NewSession(ctx context.Context, req *sandbox.SessionRequest) (sandbox.Session, error) {
	return &fakeSession{
		id:    req.Config.ID,
		lang:  req.Pack.Name,
		cfg:   req.Config,
		state: make(map[string]string),
	}, nil
}

type fakeSession struct {
	id     string
	lang   string
	cfg    sandbox.SessionConfig
	mu     sync.Mutex
	closed bool
	dead   bool
	state  map[string]string
}

func (s *fakeSession) ID() string   { return s.id }
func (s *fakeSession) Lang() string { return s.lang }

func (s *fakeSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && !s.dead
}

func (s *fakeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *fakeSession) Reset(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return sandbox.ErrSessionClosed
	}
	s.dead = false
	s.state = make(map[string]string)
	return nil
}

func (s *fakeSession) Eval(ctx context.Context, code string) (*sandbox.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, sandbox.ErrSessionClosed
	}
	restarted := false
	if s.dead {
		if !s.cfg.AutoRestart {
			return nil, sandbox.ErrSessionKilled
		}
		s.dead = false
		s.state = make(map[string]string)
		restarted = true
	}

	if code == "spin" {
		select {
		case <-ctx.Done():
			s.dead = true
			return nil, fmt.Errorf("%w: %w", sandbox.ErrSessionKilled, sandbox.ErrTimeout)
		}
	}
	if code == "kill" {
		s.dead = true
		return nil, sandbox.ErrSessionKilled
	}
	if strings.HasPrefix(code, "set ") {
		parts := strings.SplitN(strings.TrimPrefix(code, "set "), "=", 2)
		if len(parts) == 2 {
			s.state[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
		return &sandbox.Result{ExitCode: 0, Backend: "fake", Restarted: restarted}, nil
	}
	if strings.HasPrefix(code, "get ") {
		k := strings.TrimSpace(strings.TrimPrefix(code, "get "))
		val := s.state[k]
		return &sandbox.Result{Stdout: []byte(val), ExitCode: 0, Backend: "fake", Restarted: restarted}, nil
	}

	return &sandbox.Result{Stdout: []byte(code), ExitCode: 0, Backend: "fake", Restarted: restarted}, nil
}

// Packs returns every toy language:
//
//	echo    prints the code to stdout
//	fail    prints the code to stderr and exits with status 3
//	writer  writes the code to /out/result.txt
//	spin    blocks until the context is done
//	flood   prints forever
//	cat     copies stdin to stdout
//	env     prints the environment, sorted, as KEY=VALUE lines
//	files   lists the files under /in, one per line
func Packs() []*sandbox.Pack {
	names := []string{"echo", "fail", "writer", "spin", "flood", "cat", "env", "files"}
	out := make([]*sandbox.Pack, len(names))
	for i, n := range names {
		out[i] = Pack(n)
	}
	return out
}

// Run implements sandbox.Backend.
func (Backend) Run(ctx context.Context, req *sandbox.Request) (sandbox.Outcome, error) {
	switch req.Pack.Name {
	case "echo":
		_, err := io.WriteString(req.Stdout, req.Spec.Code)
		return sandbox.Outcome{}, err

	case "fail":
		_, err := io.WriteString(req.Stderr, req.Spec.Code)
		return sandbox.Outcome{ExitCode: 3}, err

	case "writer":
		return sandbox.Outcome{}, req.FS.WriteFile("out/result.txt", []byte(req.Spec.Code))

	case "spin":
		<-ctx.Done()
		return sandbox.Outcome{}, ctx.Err()

	case "flood":
		chunk := []byte(strings.Repeat("x", 1024))
		for {
			if _, err := req.Stdout.Write(chunk); err != nil {
				return sandbox.Outcome{}, err
			}
			if ctx.Err() != nil {
				return sandbox.Outcome{}, ctx.Err()
			}
		}

	case "cat":
		_, err := io.Copy(req.Stdout, req.Stdin)
		return sandbox.Outcome{}, err

	case "env":
		keys := make([]string, 0, len(req.Spec.Env))
		for k := range req.Spec.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := fmt.Fprintf(req.Stdout, "%s=%s\n", k, req.Spec.Env[k]); err != nil {
				return sandbox.Outcome{}, err
			}
		}
		return sandbox.Outcome{}, nil

	case "files":
		snap := req.FS.Snapshot("in")
		names := make([]string, 0, len(snap))
		for n := range snap {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if _, err := fmt.Fprintln(req.Stdout, n); err != nil {
				return sandbox.Outcome{}, err
			}
		}
		return sandbox.Outcome{}, nil
	}
	return sandbox.Outcome{}, fmt.Errorf("fake: unknown language %q", req.Pack.Name)
}

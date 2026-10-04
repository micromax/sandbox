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
	return &sandbox.Pack{Name: name, Extra: map[string]any{"fake": true}}
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

// Package hostile holds a corpus of attack programs ("hostile guests") and the
// outcome the sandbox must produce for each.
//
// The corpus is plain data so every backend can run the same attacks: a backend
// test builds a Fixture, calls [Cases], runs each case through its own
// sandbox.Sandbox and checks the result with [Case.Check]. A new attack found
// anywhere should be added here as a regression test.
//
// Every attack that could succeed prints a marker (see [Marker]) after the
// dangerous step, and the checks forbid that marker, so a vacuous pass (the
// attack never ran) cannot hide a failure. "Control" cases prove that the
// probing technique itself works, e.g. that a guest can read an injected file.
package hostile

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/micromax/sandbox"
)

// luaErrorsUnsupported documents the root cause behind several Lua cases.
const luaErrorsUnsupported = "lua.wasm cannot raise Lua errors: pcall/error/load-failure/out-of-memory all abort the guest " +
	"with 'throw, errcode=N' and a raw 'wasm error: unreachable' (longjmp is unsupported in the WASI build), " +
	"and Run returns no Result"

// Marker is printed by an attack program after the step that must be blocked.
// Its appearance in stdout means the attack got through.
const Marker = "ATTACK_SUCCEEDED"

// Category groups attacks, following docs/plan/09-security-and-testing.md.
type Category string

const (
	CPU     Category = "cpu"
	Memory  Category = "memory"
	Disk    Category = "disk"
	Output  Category = "output"
	Leak    Category = "leak"
	Process Category = "process"
	Network Category = "network"
	Time    Category = "time"
	Control Category = "control"
)

// Mode says what counts as a pass.
type Mode int

const (
	// Blocked: the attack must not complete cleanly. Run must return one of
	// Case.Errs, or (with AllowNonZeroExit) finish with a non-zero exit code.
	Blocked Mode = iota
	// Contained: the program may finish or be stopped, but only by an error in
	// Case.Errs. Used when the guest legitimately handles the failure itself.
	Contained
)

// Case is one attack program and its expected outcome.
type Case struct {
	Name     string
	Category Category
	Lang     string
	Code     string
	Mode     Mode

	// Limits overrides sandbox defaults (nil keeps them).
	Limits *sandbox.Limits
	Env    map[string]string
	Files  map[string][]byte

	// Errs lists the sentinel errors Run may return (matched with errors.Is).
	Errs []error
	// AllowNonZeroExit lets a Blocked case pass by exiting non-zero without a
	// Run error, e.g. the guest runtime reporting "out of memory" itself.
	AllowNonZeroExit bool

	// StdoutHas / StdoutNot are substrings required in / forbidden from stdout.
	StdoutHas []string
	StdoutNot []string
	// StdoutEq, when non-nil, must equal stdout exactly.
	StdoutEq []byte
	// Max bounds the wall-clock time of the whole run (default 20s).
	Max time.Duration

	// After runs extra host-side assertions (files that must not exist, ...).
	After func(t *testing.T, fx *Fixture)

	// Known marks a real, unfixed gap. The runner expects the case to fail and
	// skips it with this text; if it starts passing the runner fails so the
	// marker is removed. Keep the text pointing at the tracking issue.
	Known string
}

// Fixture is the host-side environment attacks probe. A test creates it, so the
// canaries are real: if a guest can read or touch them, the sandbox leaked.
type Fixture struct {
	// CanaryEnvName is a host environment variable holding CanarySecret.
	CanaryEnvName string
	CanarySecret  string
	// CanaryPath is an absolute host file whose content is CanarySecret.
	CanaryPath string
	// WritePath is an absolute host path in an existing directory that the
	// guest must never manage to create.
	WritePath string
	// LoopbackHost/Port is a listener on the host's loopback interface.
	LoopbackHost string
	LoopbackPort int

	loopbackHits atomic.Int64
}

// RecordLoopbackHit is called by the test's listener for every connection.
func (fx *Fixture) RecordLoopbackHit() { fx.loopbackHits.Add(1) }

// LoopbackHits is how many connections reached the host listener.
func (fx *Fixture) LoopbackHits() int64 { return fx.loopbackHits.Load() }

// Check validates a run against the case and returns one message per violation.
func (c *Case) Check(res *sandbox.Result, err error, elapsed time.Duration) []string {
	var bad []string
	add := func(format string, a ...any) { bad = append(bad, fmt.Sprintf(format, a...)) }

	max := c.Max
	if max == 0 {
		max = 20 * time.Second
	}
	if elapsed > max {
		add("took %v, bound is %v", elapsed.Round(time.Millisecond), max)
	}

	allowed := func(err error) bool {
		for _, e := range c.Errs {
			if errors.Is(err, e) {
				return true
			}
		}
		return false
	}

	switch {
	case err != nil && !allowed(err):
		add("unexpected error %q, allowed %v", err, c.Errs)
	case err == nil && c.Mode == Blocked && !(c.AllowNonZeroExit && res != nil && res.ExitCode != 0):
		add("attack completed cleanly (err=nil, exit=%d); it should have been blocked", exitCode(res))
	}

	if res == nil {
		// Non-limit errors return no Result; nothing more to inspect.
		if len(c.StdoutHas) > 0 || c.StdoutEq != nil {
			add("no result to inspect, but stdout expectations were set")
		}
		return bad
	}

	out := string(res.Stdout)
	if strings.Contains(out, Marker) {
		add("stdout contains %q: the attack got through:\n%s", Marker, out)
	}
	for _, s := range c.StdoutNot {
		if strings.Contains(out, s) {
			add("stdout contains forbidden %q:\n%s", s, out)
		}
	}
	for _, s := range c.StdoutHas {
		if !strings.Contains(out, s) {
			add("stdout lacks %q:\n%s", s, out)
		}
	}
	if c.StdoutEq != nil && !bytes.Equal(res.Stdout, c.StdoutEq) {
		add("stdout = %q, want %q", res.Stdout, c.StdoutEq)
	}
	return bad
}

func exitCode(res *sandbox.Result) int {
	if res == nil {
		return -1
	}
	return res.ExitCode
}

// expand substitutes {{PLACEHOLDERS}} in attack source with fixture values.
func (fx *Fixture) expand(code string) string {
	return strings.NewReplacer(
		"{{MARKER}}", Marker,
		"{{ENV}}", fx.CanaryEnvName,
		"{{SECRET}}", fx.CanarySecret,
		"{{CANARY_PATH}}", fx.CanaryPath,
		"{{WRITE_PATH}}", fx.WritePath,
		"{{LOOP_HOST}}", fx.LoopbackHost,
		"{{LOOP_PORT}}", fmt.Sprint(fx.LoopbackPort),
	).Replace(code)
}

func lim(l sandbox.Limits) *sandbox.Limits { return &l }

// Cases returns the corpus for the Wasm-tier languages (js, lua, python).
func Cases(fx *Fixture) []Case {
	cs := wasmCases()
	for i := range cs {
		cs[i].Code = fx.expand(cs[i].Code)
		for j, n := range cs[i].StdoutNot {
			cs[i].StdoutNot[j] = fx.expand(n)
		}
		if cs[i].Max == 0 {
			cs[i].Max = 20 * time.Second
		}
	}
	return cs
}

func wasmCases() []Case {
	timeout := []error{sandbox.ErrTimeout}
	oom := []error{sandbox.ErrMemoryLimit}
	oomOrTimeout := []error{sandbox.ErrMemoryLimit, sandbox.ErrTimeout}
	quota := []error{sandbox.ErrFSQuota}
	outLimit := []error{sandbox.ErrOutputLimit}
	short := sandbox.Limits{WallTime: 1500 * time.Millisecond}
	smallMem := sandbox.Limits{Memory: 64 << 20, WallTime: 10 * time.Second}
	// Memory attacks get a long wall limit so it is the memory limit, not the
	// clock, that has to stop them even on a loaded machine.
	memBomb := sandbox.Limits{Memory: 64 << 20, WallTime: 40 * time.Second}

	return []Case{
		// ---- Controls: prove the probing techniques work, so "blocked" is meaningful.
		{
			Name: "control/js-reads-injected-file", Category: Control, Lang: "js", Mode: Contained,
			Files:     map[string][]byte{"hello.txt": []byte("hello-from-in")},
			Code:      `console.log(std.loadFile('/in/hello.txt'));`,
			StdoutHas: []string{"hello-from-in"},
		},
		{
			Name: "control/lua-reads-injected-file", Category: Control, Lang: "lua", Mode: Contained,
			Files:     map[string][]byte{"hello.txt": []byte("hello-from-in")},
			Code:      `local f = assert(io.open('/in/hello.txt')) print(f:read('a'))`,
			StdoutHas: []string{"hello-from-in"},
		},
		{
			Name: "control/lua-pcall-catches-errors", Category: Control, Lang: "lua", Mode: Contained,
			Code:      "print(pcall(error, 'boom'))\nprint('after')",
			StdoutHas: []string{"false\tboom", "after"},
			Known:     luaErrorsUnsupported,
		},
		{
			Name: "control/python-reads-injected-file-and-granted-env", Category: Control, Lang: "python", Mode: Contained,
			Files:     map[string][]byte{"hello.txt": []byte("hello-from-in")},
			Env:       map[string]string{"GRANTED": "yes"},
			Code:      "import os\nprint(open('/in/hello.txt').read(), os.environ.get('GRANTED'))",
			StdoutHas: []string{"hello-from-in yes"},
		},

		// ---- CPU
		{Name: "cpu/js-infinite-loop", Category: CPU, Lang: "js", Mode: Blocked, Limits: lim(short), Errs: timeout,
			Code: `while (true) {}`},
		{Name: "cpu/lua-infinite-loop", Category: CPU, Lang: "lua", Mode: Blocked, Limits: lim(short), Errs: timeout,
			Code: `while true do end`},
		{Name: "cpu/python-infinite-loop", Category: CPU, Lang: "python", Mode: Blocked, Limits: lim(short), Errs: timeout,
			Code: "while True:\n    pass"},
		{Name: "cpu/python-regex-catastrophic-backtracking", Category: CPU, Lang: "python", Mode: Blocked, Limits: lim(short), Errs: timeout,
			Code: "import re\nre.match(r'(a+)+$', 'a' * 40 + 'b')\nprint('{{MARKER}}')"},
		{Name: "cpu/js-deep-recursion", Category: CPU, Lang: "js", Mode: Contained, Limits: lim(smallMem), Errs: oomOrTimeout,
			Code:      "function f(n) { return f(n + 1) + 1 }\ntry { f(0) } catch (e) { console.log('caught', e.name) }",
			StdoutHas: []string{"caught"}},
		{Name: "cpu/lua-deep-recursion", Category: CPU, Lang: "lua", Mode: Contained, Limits: lim(smallMem), Errs: oomOrTimeout,
			Code:      "local function f(n) return 1 + f(n + 1) end\nprint(pcall(f, 0))",
			StdoutHas: []string{"false"},
			Known:     luaErrorsUnsupported},
		{Name: "cpu/python-deep-recursion-raised-limit", Category: CPU, Lang: "python", Mode: Blocked, Limits: lim(smallMem), Max: 90 * time.Second,
			Errs: oomOrTimeout, AllowNonZeroExit: true,
			Code: "import sys\nsys.setrecursionlimit(10**9)\ndef f(n): return f(n + 1) + 1\nf(0)\nprint('{{MARKER}}')"},

		{Name: "cpu/python-deep-recursion-honors-deadline", Category: CPU, Lang: "python", Mode: Blocked,
			Limits: lim(sandbox.Limits{WallTime: time.Second}), Errs: timeout, Max: 3 * time.Second,
			Code:  "import sys\nsys.setrecursionlimit(10**9)\ndef f(n): return f(n + 1) + 1\nf(0)\nprint('{{MARKER}}')",
			Known: "measured on the Wasm backend: a deep-recursion guest outlives its wall-clock deadline by several seconds (WallTime 2s ended at ~7.6s, 5s at ~10.5s via the CLI) whereas a plain infinite loop overshoots by ~2s; cause not yet identified"},

		// ---- Memory
		{Name: "memory/js-unbounded-allocation", Category: Memory, Lang: "js", Mode: Blocked, Limits: lim(memBomb), Max: 60 * time.Second,
			Errs: oom, AllowNonZeroExit: true,
			Code: "var a = []\nfor (;;) { a.push(new Array(1 << 20).fill(1)) }\nconsole.log('{{MARKER}}')"},
		{Name: "memory/js-single-huge-allocation", Category: Memory, Lang: "js", Mode: Blocked, Limits: lim(memBomb), Max: 60 * time.Second,
			Errs: oom, AllowNonZeroExit: true,
			Code: "var s = 'x'.repeat(1 << 29)\nconsole.log('{{MARKER}}', s.length)"},
		{Name: "memory/lua-unbounded-allocation", Category: Memory, Lang: "lua", Mode: Blocked, Limits: lim(memBomb), Max: 60 * time.Second,
			Errs: oom, AllowNonZeroExit: true,
			Code:  "local t = {}\nwhile true do t[#t + 1] = string.rep('x', 1 << 20) end\nprint('{{MARKER}}')",
			Known: luaErrorsUnsupported},
		{Name: "memory/python-unbounded-allocation", Category: Memory, Lang: "python", Mode: Blocked, Limits: lim(memBomb), Max: 60 * time.Second,
			Errs: oom, AllowNonZeroExit: true,
			Code: "a = []\nwhile True:\n    a.append(bytearray(1 << 20))\nprint('{{MARKER}}')"},
		{Name: "memory/python-single-huge-allocation", Category: Memory, Lang: "python", Mode: Blocked, Limits: lim(memBomb), Max: 60 * time.Second,
			Errs: oom, AllowNonZeroExit: true,
			Code: "x = bytearray(1 << 31)\nprint('{{MARKER}}', len(x))"},

		// ---- Disk
		{Name: "disk/python-write-past-quota", Category: Disk, Lang: "python", Mode: Blocked,
			Limits: lim(sandbox.Limits{FSQuota: 1 << 20}), Errs: quota,
			Code: "open('/work/big', 'wb').write(b'x' * (8 << 20))"},
		{Name: "disk/js-too-many-files", Category: Disk, Lang: "js", Mode: Blocked,
			Limits: lim(sandbox.Limits{MaxFiles: 64}), Errs: quota,
			Code: "for (var i = 0; i < 500; i++) { var f = std.open('/work/f' + i, 'w'); if (f) { f.puts('x'); f.close() } }"},
		{Name: "disk/lua-write-to-host-absolute-path", Category: Disk, Lang: "lua", Mode: Contained,
			Code:      "local f = io.open('{{WRITE_PATH}}', 'w')\nif f then f:write('pwned') f:close() print('{{MARKER}}') else print('denied') end",
			StdoutHas: []string{"denied"},
			After:     requireNotExist},
		{Name: "disk/python-write-to-host-absolute-path", Category: Disk, Lang: "python", Mode: Contained,
			Code:      "try:\n    open('{{WRITE_PATH}}', 'w').write('pwned')\n    print('{{MARKER}}')\nexcept OSError:\n    print('denied')",
			StdoutHas: []string{"denied"},
			After:     requireNotExist},
		{Name: "disk/python-modifies-read-only-input", Category: Disk, Lang: "python", Mode: Contained,
			Files:     map[string][]byte{"data.txt": []byte("ORIGINAL")},
			Code:      "try:\n    open('/in/data.txt', 'w').write('TAMPERED')\n    print('{{MARKER}}')\nexcept OSError:\n    print('denied')",
			StdoutHas: []string{"denied"},
			Known:     "the Wasm backend mounts the whole run directory read-write, so /in is writable although the Request contract says read-only"},

		// ---- Output
		{Name: "output/python-infinite-stdout", Category: Output, Lang: "python", Mode: Blocked,
			Limits: lim(sandbox.Limits{MaxOutput: 64 << 10}), Errs: outLimit,
			Code: "while True:\n    print('x' * 100)"},
		{Name: "output/python-infinite-stderr", Category: Output, Lang: "python", Mode: Blocked,
			Limits: lim(sandbox.Limits{MaxOutput: 64 << 10}), Errs: outLimit,
			Code: "import sys\nwhile True:\n    sys.stderr.write('x' * 100 + '\\n')"},
		{Name: "output/js-giant-single-line", Category: Output, Lang: "js", Mode: Blocked,
			Limits: lim(sandbox.Limits{MaxOutput: 64 << 10}), Errs: outLimit,
			Code: "console.log('x'.repeat(8 << 20))"},
		{Name: "output/lua-nul-and-ansi-bytes-pass-through-raw", Category: Output, Lang: "lua", Mode: Contained,
			Code:     `io.write('a\0b\27[31mred\27[0m')`,
			StdoutEq: []byte("a\x00b\x1b[31mred\x1b[0m")},

		// ---- Information leaks
		{Name: "leak/js-host-environment", Category: Leak, Lang: "js", Mode: Contained,
			Code:      "console.log('ENV=' + std.getenv('{{ENV}}'))",
			StdoutHas: []string{"ENV=undefined"}, StdoutNot: []string{"{{SECRET}}"}},
		{Name: "leak/lua-host-environment", Category: Leak, Lang: "lua", Mode: Contained,
			Code:      "print('ENV=' .. tostring(os.getenv('{{ENV}}')))",
			StdoutHas: []string{"ENV=nil"}, StdoutNot: []string{"{{SECRET}}"}},
		{Name: "leak/python-host-environment", Category: Leak, Lang: "python", Mode: Contained,
			Code: "import os\nprint('ENV=' + str(os.environ.get('{{ENV}}')))\n" +
				"print(sorted(k for k in os.environ if k not in ('PYTHONHOME', 'PYTHONPATH')))",
			StdoutHas: []string{"ENV=None", "[]"}, StdoutNot: []string{"{{SECRET}}"}},
		{Name: "leak/js-host-files", Category: Leak, Lang: "js", Mode: Contained,
			Code: "['{{CANARY_PATH}}', '/etc/passwd', '/proc/self/environ', '/proc/self/cmdline', '/in/../../../etc/passwd'].forEach(function (p) {\n" +
				"  var d = std.loadFile(p)\n  if (d !== null) console.log('READ', p, d.slice(0, 60))\n})\nconsole.log('done')",
			StdoutHas: []string{"done"}, StdoutNot: []string{"READ ", "{{SECRET}}"}},
		{Name: "leak/lua-host-files", Category: Leak, Lang: "lua", Mode: Contained,
			Code: "for _, p in ipairs({'{{CANARY_PATH}}', '/etc/passwd', '/proc/self/environ', '/in/../../../etc/passwd'}) do\n" +
				"  local f = io.open(p, 'r')\n  if f then print('READ', p, f:read(60)) f:close() end\nend\nprint('done')",
			StdoutHas: []string{"done"}, StdoutNot: []string{"READ", "{{SECRET}}"}},
		{Name: "leak/python-host-files", Category: Leak, Lang: "python", Mode: Contained,
			Code: "for p in ['{{CANARY_PATH}}', '/etc/passwd', '/proc/self/environ', '/proc/self/cmdline', '/in/../../../etc/passwd', '../../../../etc/passwd']:\n" +
				"    try:\n        print('READ', p, open(p, 'rb').read(60))\n    except OSError:\n        pass\nprint('done')",
			StdoutHas: []string{"done"}, StdoutNot: []string{"READ ", "{{SECRET}}"}},
		{Name: "leak/python-root-listing-shows-only-virtual-dirs", Category: Leak, Lang: "python", Mode: Contained,
			Code:      "import os\nprint(sorted(set(os.listdir('/')) - {'in', 'work', 'out', 'lib'}))",
			StdoutHas: []string{"[]"}},

		// ---- Process
		{Name: "process/python-fork-and-exec-unavailable", Category: Process, Lang: "python", Mode: Contained,
			Code: "import os, subprocess\n" +
				"for name, fn in [('fork', lambda: os.fork()), ('system', lambda: os.system('id')), ('run', lambda: subprocess.run(['id'])), ('execv', lambda: os.execv('/bin/sh', ['sh']))]:\n" +
				"    try:\n        fn()\n        print('{{MARKER}}', name)\n    except BaseException:\n        print('blocked', name)",
			StdoutHas: []string{"blocked fork", "blocked system", "blocked run", "blocked execv"}, StdoutNot: []string{"uid="}},

		// ---- Network (no policy granted)
		{Name: "network/python-connect-to-host-loopback", Category: Network, Lang: "python", Mode: Contained,
			Code: "import socket\ntry:\n    s = socket.create_connection(('{{LOOP_HOST}}', {{LOOP_PORT}}), timeout=2)\n    print('{{MARKER}}')\n" +
				"except BaseException:\n    print('blocked')",
			StdoutHas: []string{"blocked"},
			After: func(t *testing.T, fx *Fixture) {
				t.Helper()
				if n := fx.LoopbackHits(); n != 0 {
					t.Errorf("host loopback listener received %d connection(s) from the guest", n)
				}
			}},

		// ---- Time
		{Name: "time/python-very-long-sleep", Category: Time, Lang: "python", Mode: Contained,
			Limits: lim(sandbox.Limits{WallTime: 2 * time.Second}), Errs: timeout, Max: 10 * time.Second,
			Code: "import time\ntime.sleep(10**9)\nprint('done')"},
	}
}

// requireNotExist asserts the guest could not create Fixture.WritePath on the host.
func requireNotExist(t *testing.T, fx *Fixture) {
	t.Helper()
	if _, err := os.Stat(fx.WritePath); err == nil {
		t.Errorf("guest created host file %s", fx.WritePath)
	}
}

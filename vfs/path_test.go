package vfs

import (
	"errors"
	"io/fs"
	"strings"
	"testing"
)

func TestCleanAccepts(t *testing.T) {
	tests := map[string]string{
		"":                ".",
		".":               ".",
		"/":               ".",
		"a":               "a",
		"/a":              "a",
		"a/b/c":           "a/b/c",
		"a//b":            "a/b",
		"./a/./b/":        "a/b",
		`a\b\c`:           "a/b/c",
		"/out/result.txt": "out/result.txt",
		"héllo/wörld":     "héllo/wörld",
	}
	for in, want := range tests {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestCleanRejectsHostilePaths(t *testing.T) {
	long := strings.Repeat("a", MaxNameLen+1)
	deep := strings.Repeat("d/", MaxDepth+1)
	tests := map[string]string{
		"parent":             "../etc/passwd",
		"embedded parent":    "a/../../b",
		"trailing parent":    "a/..",
		"only parent":        "..",
		"backslash parent":   `..\..\windows`,
		"mixed parent":       `a/..\b`,
		"drive":              `C:\Windows\System32`,
		"drive relative":     "c:foo",
		"drive forward":      "D:/x",
		"drive after slash":  "/A:",
		"drive after dot":    "./C:/x",
		"drive after bslash": `\C:\x`,
		"drive after many":   "//./C:",
		"unc":                `\\server\share\..\x`,
		"nul":                "a\x00b",
		"newline":            "a\nb",
		"del":                "a\x7fb",
		"invalid utf8":       "a\xffb",
		"too long element":   long,
		"too deep":           deep,
		"too long path":      strings.Repeat("a/", MaxPathLen),
	}
	for name, in := range tests {
		if got, err := Clean(in); err == nil {
			t.Errorf("%s: Clean(%q) = %q, want error", name, in, got)
		} else if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("%s: error %v does not wrap ErrInvalidPath", name, err)
		}
	}
}

func FuzzClean(f *testing.F) {
	seeds := []string{"", ".", "..", "a/b", "../x", `..\x`, "C:\\x", "a\x00", "//a//b//", "a/./b", "/..", "a/../b"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got, err := Clean(in)
		if err != nil {
			return
		}
		if !fs.ValidPath(got) {
			t.Fatalf("Clean(%q) = %q is not a valid fs path", in, got)
		}
		for _, p := range strings.Split(got, "/") {
			if p == ".." {
				t.Fatalf("Clean(%q) = %q contains ..", in, got)
			}
		}
		if strings.ContainsAny(got, "\\\x00") {
			t.Fatalf("Clean(%q) = %q contains forbidden byte", in, got)
		}
		again, err := Clean(got)
		if err != nil || again != got {
			t.Fatalf("Clean not idempotent: %q -> %q -> %q, %v", in, got, again, err)
		}
	})
}

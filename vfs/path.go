// Package vfs implements the in-memory virtual filesystem a sandboxed guest
// sees. It never touches the host disk.
//
// Every write is checked against a byte quota and a node-count quota, and
// every path is normalised by [Clean], which rejects anything that could
// escape the virtual root (".." elements, drive letters, NUL bytes, ...).
package vfs

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Limits on path shape. They bound the memory a hostile path can cost.
const (
	MaxPathLen = 4096
	MaxNameLen = 255
	MaxDepth   = 64
)

// ErrInvalidPath is returned for paths that are malformed or unsafe.
var ErrInvalidPath = errors.New("vfs: invalid path")

// Clean validates name and returns its canonical form.
//
// The result always satisfies [fs.ValidPath]: it uses forward slashes, has no
// leading or trailing slash, no "." or ".." elements, and the root is ".".
// Backslashes are treated as separators so Windows-style input cannot smuggle
// in extra path elements.
//
// Clean does not resolve ".."; it rejects it. Resolving would let a hostile
// path appear to point somewhere harmless while a naive consumer interprets
// it differently.
func Clean(name string) (string, error) {
	if len(name) > MaxPathLen {
		return "", fmt.Errorf("%w: longer than %d bytes", ErrInvalidPath, MaxPathLen)
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: not valid UTF-8", ErrInvalidPath)
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("%w: control character", ErrInvalidPath)
		}
	}
	name = strings.ReplaceAll(name, `\`, "/")

	parts := make([]string, 0, 8)
	for _, p := range strings.Split(name, "/") {
		switch p {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("%w: parent element", ErrInvalidPath)
		}
		if len(p) > MaxNameLen {
			return "", fmt.Errorf("%w: element longer than %d bytes", ErrInvalidPath, MaxNameLen)
		}
		parts = append(parts, p)
	}
	// Reject a drive-letter first element such as "C:" or "c:foo". This must
	// run on the cleaned elements: checking the raw string would let "/C:" or
	// "./C:" slip through and become "C:" after normalisation.
	if len(parts) > 0 && len(parts[0]) >= 2 && parts[0][1] == ':' && isASCIILetter(parts[0][0]) {
		return "", fmt.Errorf("%w: drive letter", ErrInvalidPath)
	}
	if len(parts) > MaxDepth {
		return "", fmt.Errorf("%w: deeper than %d elements", ErrInvalidPath, MaxDepth)
	}
	if len(parts) == 0 {
		return ".", nil
	}
	return strings.Join(parts, "/"), nil
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// split returns the elements of an already-cleaned path. The root yields nil.
func split(clean string) []string {
	if clean == "." {
		return nil
	}
	return strings.Split(clean, "/")
}

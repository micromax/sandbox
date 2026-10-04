package vfs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Quota and structure errors.
var (
	// ErrQuota is returned when a write would exceed the byte quota.
	ErrQuota = errors.New("vfs: quota exceeded")
	// ErrTooManyFiles is returned when creating a file or directory would
	// exceed the node limit. It wraps ErrQuota.
	ErrTooManyFiles = fmt.Errorf("%w: too many files", ErrQuota)
	// ErrIsDir is returned when a file operation targets a directory.
	ErrIsDir = errors.New("vfs: is a directory")
	// ErrNotDir is returned when a path element is a file but must be a directory.
	ErrNotDir = errors.New("vfs: not a directory")
)

// Unlimited disables a byte quota or node limit when passed to [New].
const (
	Unlimited      uint64 = math.MaxUint64
	UnlimitedFiles        = -1
)

type node struct {
	dir  bool
	data []byte           // file contents; never mutated in place
	kids map[string]*node // directory children
}

// Stats reports current usage.
type Stats struct {
	Bytes uint64
	Files int
	Dirs  int
}

// FS is an in-memory filesystem with quotas. It implements [fs.FS],
// [fs.ReadFileFS], [fs.ReadDirFS] and [fs.StatFS] for reading, and offers
// quota-checked mutation methods. It is safe for concurrent use.
type FS struct {
	mu       sync.RWMutex
	root     *node
	quota    uint64
	maxNodes int // files + directories, excluding the root; <0 = unlimited
	bytes    uint64
	files    int
	dirs     int
	epoch    time.Time
}

// New returns an empty filesystem. quota caps total file bytes and maxNodes
// caps the number of files plus directories (the root is free). Use
// [Unlimited] and [UnlimitedFiles] to disable a cap.
func New(quota uint64, maxNodes int) *FS {
	return &FS{
		root:     &node{dir: true, kids: map[string]*node{}},
		quota:    quota,
		maxNodes: maxNodes,
		epoch:    time.Now().UTC().Truncate(time.Second),
	}
}

// Stats returns a snapshot of current usage.
func (f *FS) Stats() Stats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return Stats{Bytes: f.bytes, Files: f.files, Dirs: f.dirs}
}

func (f *FS) find(clean string) *node {
	cur := f.root
	for _, p := range split(clean) {
		if !cur.dir {
			return nil
		}
		next, ok := cur.kids[p]
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// walkParents walks all but the last element of parts. It returns the deepest
// existing directory, the number of parent elements already present, and an
// error if an existing element is not a directory.
func (f *FS) walkParents(parts []string) (cur *node, present int, err error) {
	cur = f.root
	for present < len(parts)-1 {
		next, ok := cur.kids[parts[present]]
		if !ok {
			break
		}
		if !next.dir {
			return nil, 0, ErrNotDir
		}
		cur = next
		present++
	}
	return cur, present, nil
}

func (f *FS) checkNodes(add int) error {
	if f.maxNodes >= 0 && f.files+f.dirs+add > f.maxNodes {
		return ErrTooManyFiles
	}
	return nil
}

func (f *FS) checkBytes(grow uint64) error {
	if grow > f.quota-f.bytes {
		return ErrQuota
	}
	return nil
}

// WriteFile creates or replaces the file at name, creating parent
// directories as needed. The data is copied. Nothing is modified when an
// error is returned.
func (f *FS) WriteFile(name string, data []byte) error {
	clean, err := Clean(name)
	if err != nil {
		return err
	}
	if clean == "." {
		return fmt.Errorf("%w: %q", ErrIsDir, name)
	}
	parts := split(clean)

	f.mu.Lock()
	defer f.mu.Unlock()

	cur, present, err := f.walkParents(parts)
	if err != nil {
		return fmt.Errorf("%s: %w", clean, err)
	}
	missingDirs := len(parts) - 1 - present
	last := parts[len(parts)-1]

	var old *node
	if missingDirs == 0 {
		if n, ok := cur.kids[last]; ok {
			if n.dir {
				return fmt.Errorf("%s: %w", clean, ErrIsDir)
			}
			old = n
		}
	}

	newNodes := missingDirs
	if old == nil {
		newNodes++
	}
	if err := f.checkNodes(newNodes); err != nil {
		return err
	}
	var oldSize uint64
	if old != nil {
		oldSize = uint64(len(old.data))
	}
	if newSize := uint64(len(data)); newSize > oldSize {
		if err := f.checkBytes(newSize - oldSize); err != nil {
			return err
		}
	}

	// All checks passed; mutate.
	for i := present; i < len(parts)-1; i++ {
		d := &node{dir: true, kids: map[string]*node{}}
		cur.kids[parts[i]] = d
		cur = d
		f.dirs++
	}
	if old == nil {
		cur.kids[last] = &node{data: bytes.Clone(data)}
		f.files++
		f.bytes += uint64(len(data))
	} else {
		f.bytes = f.bytes - oldSize + uint64(len(data))
		cur.kids[last] = &node{data: bytes.Clone(data)}
	}
	return nil
}

// MkdirAll creates a directory and any missing parents. It succeeds if the
// directory already exists.
func (f *FS) MkdirAll(name string) error {
	clean, err := Clean(name)
	if err != nil {
		return err
	}
	if clean == "." {
		return nil
	}
	parts := split(clean)

	f.mu.Lock()
	defer f.mu.Unlock()

	cur := f.root
	i := 0
	for ; i < len(parts); i++ {
		next, ok := cur.kids[parts[i]]
		if !ok {
			break
		}
		if !next.dir {
			return fmt.Errorf("%s: %w", clean, ErrNotDir)
		}
		cur = next
	}
	missing := len(parts) - i
	if missing == 0 {
		return nil
	}
	if err := f.checkNodes(missing); err != nil {
		return err
	}
	for ; i < len(parts); i++ {
		d := &node{dir: true, kids: map[string]*node{}}
		cur.kids[parts[i]] = d
		cur = d
		f.dirs++
	}
	return nil
}

// Remove deletes the file or directory tree at name. Removing a path that
// does not exist is not an error. The root cannot be removed.
func (f *FS) Remove(name string) error {
	clean, err := Clean(name)
	if err != nil {
		return err
	}
	if clean == "." {
		return fmt.Errorf("%w: cannot remove root", ErrInvalidPath)
	}
	parts := split(clean)

	f.mu.Lock()
	defer f.mu.Unlock()

	parent := f.root
	if len(parts) > 1 {
		parent = f.find(strings.Join(parts[:len(parts)-1], "/"))
	}
	if parent == nil || !parent.dir {
		return nil
	}
	last := parts[len(parts)-1]
	n, ok := parent.kids[last]
	if !ok {
		return nil
	}
	b, files, dirs := measure(n)
	delete(parent.kids, last)
	f.bytes -= b
	f.files -= files
	f.dirs -= dirs
	return nil
}

func measure(n *node) (bytes uint64, files, dirs int) {
	if !n.dir {
		return uint64(len(n.data)), 1, 0
	}
	dirs = 1
	for _, k := range n.kids {
		b, fl, d := measure(k)
		bytes += b
		files += fl
		dirs += d
	}
	return
}

// ReadFile implements [fs.ReadFileFS] and returns a copy of the file's
// contents. Like every [fs.FS] method it requires a name that satisfies
// [fs.ValidPath]; pass untrusted names through [Clean] first.
func (f *FS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrInvalid}
	}
	clean := name
	f.mu.RLock()
	defer f.mu.RUnlock()
	n := f.find(clean)
	if n == nil {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	if n.dir {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: ErrIsDir}
	}
	return bytes.Clone(n.data), nil
}

// Snapshot returns every file under the directory prefix, keyed by its path
// relative to prefix (always forward slashes). Contents are copies. An
// invalid, missing or non-directory prefix yields an empty map.
func (f *FS) Snapshot(prefix string) map[string][]byte {
	out := map[string][]byte{}
	clean, err := Clean(prefix)
	if err != nil {
		return out
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n := f.find(clean)
	if n == nil || !n.dir {
		return out
	}
	var walk func(n *node, rel string)
	walk = func(n *node, rel string) {
		for name, k := range n.kids {
			p := name
			if rel != "" {
				p = rel + "/" + name
			}
			if k.dir {
				walk(k, p)
			} else {
				out[p] = bytes.Clone(k.data)
			}
		}
	}
	walk(n, "")
	return out
}

// ---- fs.FS implementation ----

type info struct {
	name  string
	size  int64
	dir   bool
	mtime time.Time
}

func (i *info) Name() string { return i.name }
func (i *info) Size() int64  { return i.size }
func (i *info) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i *info) ModTime() time.Time         { return i.mtime }
func (i *info) IsDir() bool                { return i.dir }
func (i *info) Sys() any                   { return nil }
func (i *info) Type() fs.FileMode          { return i.Mode().Type() }
func (i *info) Info() (fs.FileInfo, error) { return i, nil }

func (f *FS) infoFor(name string, n *node) *info {
	base := name
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		base = name[i+1:]
	}
	size := int64(len(n.data))
	return &info{name: base, size: size, dir: n.dir, mtime: f.epoch}
}

// Open implements [fs.FS]. name must satisfy [fs.ValidPath].
func (f *FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n := f.find(name)
	if n == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	fi := f.infoFor(name, n)
	if !n.dir {
		return &fileHandle{Reader: bytes.NewReader(n.data), fi: fi}, nil
	}
	return &dirHandle{fi: fi, entries: f.entriesLocked(name, n)}, nil
}

func (f *FS) entriesLocked(dirName string, n *node) []fs.DirEntry {
	names := make([]string, 0, len(n.kids))
	for k := range n.kids {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]fs.DirEntry, 0, len(names))
	for _, k := range names {
		full := k
		if dirName != "." {
			full = dirName + "/" + k
		}
		out = append(out, f.infoFor(full, n.kids[k]))
	}
	return out
}

// Stat implements [fs.StatFS].
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n := f.find(name)
	if n == nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return f.infoFor(name, n), nil
}

// ReadDir implements [fs.ReadDirFS].
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	n := f.find(name)
	if n == nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	if !n.dir {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: ErrNotDir}
	}
	return f.entriesLocked(name, n), nil
}

type fileHandle struct {
	*bytes.Reader
	fi *info
}

func (h *fileHandle) Stat() (fs.FileInfo, error) { return h.fi, nil }
func (h *fileHandle) Close() error               { return nil }

type dirHandle struct {
	fi      *info
	entries []fs.DirEntry
	pos     int
}

func (h *dirHandle) Stat() (fs.FileInfo, error) { return h.fi, nil }
func (h *dirHandle) Close() error               { return nil }
func (h *dirHandle) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: h.fi.name, Err: ErrIsDir}
}

func (h *dirHandle) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := h.entries[h.pos:]
	if n <= 0 {
		h.pos = len(h.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	if n > len(rest) {
		n = len(rest)
	}
	h.pos += n
	return rest[:n], nil
}

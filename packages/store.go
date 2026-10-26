// Package packages manages caching, offline resolution, and injection of
// language dependencies (Python wheels, npm tarballs, etc.) for sandboxed guests.
package packages

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	// ErrNotFound is returned when a requested package is not found in the local cache.
	ErrNotFound = errors.New("packages: package not found in cache")
	// ErrInvalidArchive is returned when a package archive (wheel or tarball) is corrupted or malformed.
	ErrInvalidArchive = errors.New("packages: invalid package archive")
	// ErrArchiveTooLarge is returned when extracting an archive would exceed [ExtractLimits].
	ErrArchiveTooLarge = errors.New("packages: archive exceeds extraction limits")
)

// PackageInfo describes a cached package.
type PackageInfo struct {
	Lang    string `json:"lang"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// Store manages cached packages on the host.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// DefaultCacheDir returns the default host cache path for guest packages.
func DefaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "micromax-sandbox", "packages")
}

// NewStore creates a new PackageStore using the specified cache directory.
// If dir is empty, [DefaultCacheDir] is used.
func NewStore(dir ...string) (*Store, error) {
	d := DefaultCacheDir()
	if len(dir) > 0 && dir[0] != "" {
		d = dir[0]
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, fmt.Errorf("packages: creating cache dir %s: %w", d, err)
	}
	return &Store{dir: d}, nil
}

// Dir returns the root directory of the package store.
func (s *Store) Dir() string {
	return s.dir
}

// Add adds a package archive directly to the cache store.
func (s *Store) Add(lang, name, filename string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleanLang := strings.ToLower(strings.TrimSpace(lang))
	cleanName := strings.ToLower(strings.TrimSpace(name))
	langDir := filepath.Join(s.dir, cleanLang, cleanName)
	if err := os.MkdirAll(langDir, 0o755); err != nil {
		return err
	}

	target := filepath.Join(langDir, filepath.Base(filename))
	return os.WriteFile(target, data, 0o644)
}

// Find looks up a package by language and name. It returns the raw archive data if found.
func (s *Store) Find(lang, name string) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanLang := strings.ToLower(strings.TrimSpace(lang))
	cleanName := strings.ToLower(strings.TrimSpace(name))
	langDir := filepath.Join(s.dir, cleanLang, cleanName)

	entries, err := os.ReadDir(langDir)
	if err != nil || len(entries) == 0 {
		return nil, "", ErrNotFound
	}

	// Pick the latest/first archive entry in the package directory
	for _, e := range entries {
		if !e.IsDir() {
			path := filepath.Join(langDir, e.Name())
			data, err := os.ReadFile(path)
			if err == nil {
				return data, e.Name(), nil
			}
		}
	}

	return nil, "", ErrNotFound
}

// List returns all cached packages for a given language.
func (s *Store) List(lang string) ([]PackageInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cleanLang := strings.ToLower(strings.TrimSpace(lang))
	langDir := filepath.Join(s.dir, cleanLang)

	entries, err := os.ReadDir(langDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []PackageInfo
	for _, pkgEntry := range entries {
		if pkgEntry.IsDir() {
			pkgDir := filepath.Join(langDir, pkgEntry.Name())
			files, err := os.ReadDir(pkgDir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if !f.IsDir() {
					fi, _ := f.Info()
					var size int64
					if fi != nil {
						size = fi.Size()
					}
					out = append(out, PackageInfo{
						Lang: cleanLang,
						Name: pkgEntry.Name(),
						Path: filepath.Join(pkgDir, f.Name()),
						Size: size,
					})
				}
			}
		}
	}
	return out, nil
}

// ExtractLimits bounds what a single archive may write to disk. Declared sizes
// in a zip header are attacker-controlled, so limits are enforced on the bytes
// actually written.
type ExtractLimits struct {
	// MaxEntries caps the number of archive entries (files and directories).
	MaxEntries int
	// MaxFileBytes caps the uncompressed size of any single file.
	MaxFileBytes int64
	// MaxTotalBytes caps the combined uncompressed size of all files.
	MaxTotalBytes int64
}

// DefaultExtractLimits are generous for real pure-Python wheels (typically well
// under a few MiB) while stopping a decompression bomb from filling the host disk.
var DefaultExtractLimits = ExtractLimits{
	MaxEntries:    10_000,
	MaxFileBytes:  32 << 20,
	MaxTotalBytes: 128 << 20,
}

// ExtractWheel extracts a pure-Python .whl (ZIP archive) into targetDir using
// [DefaultExtractLimits]. See [ExtractWheelWithLimits].
func ExtractWheel(targetDir string, wheelData []byte) error {
	return ExtractWheelWithLimits(targetDir, wheelData, DefaultExtractLimits)
}

// ExtractWheelWithLimits extracts a .whl into targetDir, treating the archive as
// hostile:
//   - entries whose names are absolute or escape targetDir fail the whole
//     extraction with [ErrInvalidArchive] instead of being skipped silently;
//   - exceeding lim returns [ErrArchiveTooLarge]; at most MaxFileBytes of any
//     one file and MaxTotalBytes overall are ever written;
//   - symlinks and other non-regular entries are ignored;
//   - file permissions are reduced to plain rwx bits (no setuid/setgid/sticky)
//     and are never below 0644.
//
// The .dist-info directory is extracted like any other entry. On error,
// targetDir may hold a partial extraction and should be discarded by the caller.
func ExtractWheelWithLimits(targetDir string, wheelData []byte, lim ExtractLimits) error {
	zr, err := zip.NewReader(bytes.NewReader(wheelData), int64(len(wheelData)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}

	if lim.MaxEntries > 0 && len(zr.File) > lim.MaxEntries {
		return fmt.Errorf("%w: %d entries exceed the limit of %d", ErrArchiveTooLarge, len(zr.File), lim.MaxEntries)
	}

	var total int64
	for _, f := range zr.File {
		rel := filepath.FromSlash(f.Name)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("%w: unsafe entry name %q", ErrInvalidArchive, f.Name)
		}
		targetPath := filepath.Join(targetDir, rel)

		mode := f.Mode()
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}
		if !mode.IsRegular() {
			continue // symlinks, devices, etc. have no place in a pure-Python wheel
		}

		// Cheap early rejection using the declared size; the copy below
		// enforces the real limit because the declared size may be forged.
		if lim.MaxFileBytes > 0 && f.UncompressedSize64 > uint64(lim.MaxFileBytes) {
			return fmt.Errorf("%w: %q declares %d bytes, limit is %d", ErrArchiveTooLarge, f.Name, f.UncompressedSize64, lim.MaxFileBytes)
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		n, err := extractFile(f, targetPath, mode.Perm()|0o644, remainingBudget(lim, total))
		total += n
		if err != nil {
			return fmt.Errorf("extracting %q: %w", f.Name, err)
		}
	}
	return nil
}

// remainingBudget returns how many more bytes one file may write, or -1 for no limit.
func remainingBudget(lim ExtractLimits, written int64) int64 {
	budget := int64(-1)
	if lim.MaxFileBytes > 0 {
		budget = lim.MaxFileBytes
	}
	if lim.MaxTotalBytes > 0 {
		if left := lim.MaxTotalBytes - written; budget < 0 || left < budget {
			budget = left
		}
	}
	return budget
}

// extractFile copies one entry to path, writing at most budget bytes (-1 = no
// limit) and failing with ErrArchiveTooLarge if the entry holds more. It returns
// the number of bytes written.
func extractFile(f *zip.File, path string, perm os.FileMode, budget int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return 0, err
	}

	var src io.Reader = rc
	if budget >= 0 {
		src = io.LimitReader(rc, budget+1) // one extra byte detects overflow
	}
	n, cpErr := io.Copy(out, src)
	closeErr := out.Close()

	if budget >= 0 && n > budget {
		// Drop the overflow byte so the file on disk never exceeds the budget.
		_ = os.Truncate(path, budget)
		return budget, fmt.Errorf("%w: entry exceeds the remaining size budget", ErrArchiveTooLarge)
	}
	if cpErr != nil {
		var corrupt flate.CorruptInputError
		if errors.Is(cpErr, zip.ErrFormat) || errors.Is(cpErr, zip.ErrChecksum) ||
			errors.Is(cpErr, zip.ErrAlgorithm) || errors.Is(cpErr, io.ErrUnexpectedEOF) ||
			errors.As(cpErr, &corrupt) {
			// Includes entries that inflate past the size they declared, which
			// archive/zip detects on its own.
			return n, fmt.Errorf("%w: %v", ErrInvalidArchive, cpErr)
		}
		return n, cpErr
	}
	return n, closeErr
}

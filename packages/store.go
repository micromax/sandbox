// Package packages manages caching, offline resolution, and injection of
// language dependencies (Python wheels, npm tarballs, etc.) for sandboxed guests.
package packages

import (
	"archive/zip"
	"bytes"
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

// ExtractWheel extracts a pure-Python .whl (ZIP archive) into targetDir.
// It skips .dist-info and dangerous path traversal entries.
func ExtractWheel(targetDir string, wheelData []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(wheelData), int64(len(wheelData)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArchive, err)
	}

	for _, f := range zr.File {
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			continue // Prevent traversal
		}

		targetPath := filepath.Join(targetDir, cleanName)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		outF, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}

		_, cpErr := io.Copy(outF, rc)
		_ = rc.Close()
		_ = outF.Close()
		if cpErr != nil {
			return cpErr
		}
	}

	return nil
}

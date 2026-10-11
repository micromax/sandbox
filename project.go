package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	// ErrProjectTooLarge is returned when the collected project files exceed the size limit.
	ErrProjectTooLarge = errors.New("sandbox: project files exceed maximum size")
)

// DefaultMaxProjectBytes is the default maximum total size of project files injected (32 MiB).
const DefaultMaxProjectBytes = 32 * 1024 * 1024

// defaultIgnoredDirs lists directory names skipped when loading local project files.
var defaultIgnoredDirs = map[string]bool{
	".git":          true,
	".github":       true,
	".venv":         true,
	"venv":          true,
	"node_modules":  true,
	"__pycache__":   true,
	".pytest_cache": true,
	".idea":         true,
	".vscode":       true,
	"target":        true,
	"dist":          true,
	"build":         true,
}

// LoadProjectFiles scans a local directory on the host and returns a map of
// relative file paths to file contents, suitable for injecting into [Spec.Files].
// It automatically ignores VCS folders, virtualenvs, node_modules, and build caches.
func LoadProjectFiles(rootDir string, maxBytes ...int64) (map[string][]byte, error) {
	limit := int64(DefaultMaxProjectBytes)
	if len(maxBytes) > 0 && maxBytes[0] > 0 {
		limit = maxBytes[0]
	}

	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolving project path: %w", err)
	}

	fi, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("reading project directory: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("project path %s is not a directory", absRoot)
	}

	files := make(map[string][]byte)
	var totalBytes int64

	err = filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if defaultIgnoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(absRoot, p)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		// Skip individual files larger than 10MB to avoid large binary dumps
		if info.Size() > 10*1024*1024 {
			return nil
		}

		if totalBytes+info.Size() > limit {
			return fmt.Errorf("%w (%d bytes limit)", ErrProjectTooLarge, limit)
		}

		content, err := os.ReadFile(p)
		if err != nil {
			return err
		}

		slashPath := filepath.ToSlash(rel)
		files[slashPath] = content
		totalBytes += int64(len(content))
		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

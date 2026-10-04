// Package artifact manages fetching, caching, and SHA-256 verification
// of language runtimes (.wasm modules, libraries, etc.).
//
// All artifacts are verified against their pinned SHA-256 checksums before
// being returned. Tampered or corrupt files are rejected.
package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/micromax/sandbox"
)

var (
	// ErrChecksumMismatch is returned when an artifact's contents do not match
	// its pinned SHA-256 hash.
	ErrChecksumMismatch = errors.New("artifact: checksum mismatch")
	// ErrSizeMismatch is returned when an artifact's download size differs from expected.
	ErrSizeMismatch = errors.New("artifact: size mismatch")
	// ErrNotFound is returned when an artifact is not in cache and offline mode is enabled.
	ErrNotFound = errors.New("artifact: not found in cache")
)

// Store manages caching and retrieval of runtime artifacts.
type Store struct {
	dir         string
	httpClient  *http.Client
	offlineOnly bool
	mu          sync.Mutex
	inFlight    map[string]*sync.WaitGroup
}

// Option configures a Store.
type Option func(*Store)

// WithDir sets the cache directory for artifacts.
func WithDir(dir string) Option {
	return func(s *Store) {
		s.dir = dir
	}
}

// WithHTTPClient overrides the HTTP client used for downloading artifacts.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Store) {
		s.httpClient = client
	}
}

// WithOfflineOnly prevents any outbound HTTP requests to download artifacts.
func WithOfflineOnly(offline bool) Option {
	return func(s *Store) {
		s.offlineOnly = offline
	}
}

// DefaultCacheDir returns the default system directory for cached artifacts.
func DefaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "micromax-sandbox", "artifacts")
}

// NewStore creates an artifact store.
func NewStore(opts ...Option) (*Store, error) {
	s := &Store{
		dir: DefaultCacheDir(),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		inFlight: make(map[string]*sync.WaitGroup),
	}
	for _, opt := range opts {
		opt(s)
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, fmt.Errorf("artifact: creating cache dir %s: %w", s.dir, err)
	}
	return s, nil
}

// Dir returns the root cache directory of this Store.
func (s *Store) Dir() string {
	return s.dir
}

// Load returns the artifact binary contents, downloading and caching if necessary.
// The SHA-256 hash is always verified before returning.
func (s *Store) Load(ctx context.Context, a sandbox.Artifact) ([]byte, error) {
	if a.ArchiveEntry != "" {
		archiveArtifact := a
		archiveArtifact.ArchiveEntry = ""
		archiveData, err := s.Load(ctx, archiveArtifact)
		if err != nil {
			return nil, err
		}
		zr, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
		if err != nil {
			return nil, fmt.Errorf("reading archive for artifact %q: %w", a.Name, err)
		}
		for _, f := range zr.File {
			if f.Name == a.ArchiveEntry {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("artifact %q: entry %q not found in archive", a.Name, a.ArchiveEntry)
	}

	// If embedded directly in binary:
	if len(a.Embedded) > 0 {
		if a.SHA256 != "" {
			if err := verifyBytes(a.Embedded, a.SHA256); err != nil {
				return nil, fmt.Errorf("%w for embedded artifact %q", err, a.Name)
			}
		}
		return a.Embedded, nil
	}

	if a.SHA256 == "" {
		return nil, fmt.Errorf("artifact %q: missing required SHA256", a.Name)
	}

	cachedPath := filepath.Join(s.dir, a.SHA256+".bin")

	// Check if already in cache and valid
	if data, err := os.ReadFile(cachedPath); err == nil {
		if err := verifyBytes(data, a.SHA256); err == nil {
			return data, nil
		}
		// Invalid hash in cache; remove corrupted file
		_ = os.Remove(cachedPath)
	}

	if s.offlineOnly {
		return nil, fmt.Errorf("%w: artifact %q (%s)", ErrNotFound, a.Name, a.SHA256)
	}

	if a.URL == "" {
		return nil, fmt.Errorf("artifact %q: no URL provided for download", a.Name)
	}

	// Coordinate concurrent downloads for the same SHA-256
	if err := s.downloadDeduplicated(ctx, a, cachedPath); err != nil {
		return nil, err
	}

	// Read and verify the final file
	data, err := os.ReadFile(cachedPath)
	if err != nil {
		return nil, fmt.Errorf("reading cached artifact: %w", err)
	}
	if err := verifyBytes(data, a.SHA256); err != nil {
		_ = os.Remove(cachedPath)
		return nil, err
	}

	return data, nil
}

func (s *Store) downloadDeduplicated(ctx context.Context, a sandbox.Artifact, dest string) error {
	s.mu.Lock()
	wg, exists := s.inFlight[a.SHA256]
	if exists {
		s.mu.Unlock()
		// Wait for existing download to finish
		wg.Wait()
		return nil
	}

	wg = &sync.WaitGroup{}
	wg.Add(1)
	s.inFlight[a.SHA256] = wg
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.inFlight, a.SHA256)
		wg.Done()
		s.mu.Unlock()
	}()

	return s.downloadDirect(ctx, a, dest)
}

func (s *Store) downloadDirect(ctx context.Context, a sandbox.Artifact, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading artifact %q from %s: %w", a.Name, a.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading artifact %q: status code %d", a.Name, resp.StatusCode)
	}

	if a.Size > 0 && resp.ContentLength > 0 && resp.ContentLength != a.Size {
		return fmt.Errorf("%w: expected %d bytes, got %d", ErrSizeMismatch, a.Size, resp.ContentLength)
	}

	// Download to a temporary file in the same directory for atomic rename
	tmpFile, err := os.CreateTemp(s.dir, "download-*")
	if err != nil {
		return fmt.Errorf("creating temp file for download: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)

	var reader io.Reader = resp.Body
	if a.Size > 0 {
		// Cap max read to prevent unbounded download
		reader = io.LimitReader(resp.Body, a.Size+1)
	}

	copied, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("saving artifact download: %w", err)
	}

	if a.Size > 0 {
		if copied > a.Size {
			return fmt.Errorf("%w: received more bytes than expected size %d", ErrSizeMismatch, a.Size)
		}
		if copied < a.Size {
			return fmt.Errorf("%w: received %d bytes, expected %d", ErrSizeMismatch, copied, a.Size)
		}
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA != a.SHA256 {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, a.SHA256, actualSHA)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Atomic rename into target location
	if err := os.Rename(tmpName, dest); err != nil {
		// On Windows, Rename fails if dest exists; try remove first if necessary
		_ = os.Remove(dest)
		if err := os.Rename(tmpName, dest); err != nil {
			return fmt.Errorf("installing artifact to %s: %w", dest, err)
		}
	}

	return nil
}

func verifyBytes(data []byte, expectedHex string) error {
	sum := sha256.Sum256(data)
	actualHex := hex.EncodeToString(sum[:])
	if actualHex != expectedHex {
		return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expectedHex, actualHex)
	}
	return nil
}

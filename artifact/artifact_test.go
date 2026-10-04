package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/micromax/sandbox"
)

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestLoadEmbedded(t *testing.T) {
	data := []byte("embedded-content")
	hash := sha256Of(data)

	store, err := NewStore(WithDir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}

	// Correct hash
	got, err := store.Load(context.Background(), sandbox.Artifact{
		Name:     "embed.bin",
		SHA256:   hash,
		Embedded: data,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}

	// Mismatched hash
	_, err = store.Load(context.Background(), sandbox.Artifact{
		Name:     "embed.bin",
		SHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
		Embedded: data,
	})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestLoadDownloadAndCache(t *testing.T) {
	content := []byte("hello-wasm-content")
	hash := sha256Of(content)

	var downloadCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&downloadCount, 1)
		w.Header().Set("Content-Length", "18")
		w.Write(content)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	store, err := NewStore(WithDir(cacheDir), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	art := sandbox.Artifact{
		Name:   "test.wasm",
		URL:    server.URL + "/test.wasm",
		SHA256: hash,
		Size:   int64(len(content)),
	}

	// First load downloads and caches
	got, err := store.Load(context.Background(), art)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("got %q, want %q", got, content)
	}
	if atomic.LoadInt64(&downloadCount) != 1 {
		t.Fatalf("download count %d, want 1", downloadCount)
	}

	// Second load hits cache without downloading
	got2, err := store.Load(context.Background(), art)
	if err != nil {
		t.Fatalf("Load 2 error: %v", err)
	}
	if string(got2) != string(content) {
		t.Fatalf("got %q, want %q", got2, content)
	}
	if atomic.LoadInt64(&downloadCount) != 1 {
		t.Fatalf("second load called download, count %d", downloadCount)
	}
}

func TestTamperedCacheReplaced(t *testing.T) {
	content := []byte("legit-data")
	hash := sha256Of(content)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(content)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	cachedFile := filepath.Join(cacheDir, hash+".bin")
	// Poison the cache with corrupted data
	if err := os.WriteFile(cachedFile, []byte("malicious-fake-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(WithDir(cacheDir), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	art := sandbox.Artifact{
		Name:   "test.wasm",
		URL:    server.URL,
		SHA256: hash,
	}

	// Store must detect the corrupt cache file and re-download the valid artifact
	got, err := store.Load(context.Background(), art)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("poisoned cache was not purged, got %q", got)
	}
}

func TestChecksumMismatchRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("wrong-data"))
	}))
	defer server.Close()

	store, err := NewStore(WithDir(t.TempDir()), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	art := sandbox.Artifact{
		Name:   "test.wasm",
		URL:    server.URL,
		SHA256: sha256Of([]byte("expected-different-data")),
	}

	_, err = store.Load(context.Background(), art)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestSizeMismatchRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("short"))
	}))
	defer server.Close()

	store, err := NewStore(WithDir(t.TempDir()), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	art := sandbox.Artifact{
		Name:   "test.wasm",
		URL:    server.URL,
		SHA256: sha256Of([]byte("short")),
		Size:   100, // declares 100, server gives 5
	}

	_, err = store.Load(context.Background(), art)
	if !errors.Is(err, ErrSizeMismatch) {
		t.Fatalf("expected ErrSizeMismatch, got %v", err)
	}
}

func TestOfflineOnlyMode(t *testing.T) {
	store, err := NewStore(WithDir(t.TempDir()), WithOfflineOnly(true))
	if err != nil {
		t.Fatal(err)
	}

	art := sandbox.Artifact{
		Name:   "missing.wasm",
		URL:    "http://example.com/missing.wasm",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	_, err = store.Load(context.Background(), art)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound in offline mode, got %v", err)
	}
}

func TestConcurrentDownloadsDeduplicated(t *testing.T) {
	content := []byte("heavy-artifact-data")
	hash := sha256Of(content)

	var downloadCalls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&downloadCalls, 1)
		time.Sleep(50 * time.Millisecond) // Simulate network latency
		w.Write(content)
	}))
	defer server.Close()

	store, err := NewStore(WithDir(t.TempDir()), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}

	art := sandbox.Artifact{
		Name:   "heavy.wasm",
		URL:    server.URL,
		SHA256: hash,
	}

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := store.Load(context.Background(), art)
			if err != nil {
				errs <- err
				return
			}
			if string(data) != string(content) {
				errs <- errors.New("data mismatch")
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent load error: %v", err)
	}

	if calls := atomic.LoadInt64(&downloadCalls); calls != 1 {
		t.Fatalf("expected 1 download call due to deduplication, got %d", calls)
	}
}

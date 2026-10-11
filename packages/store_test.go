package packages_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/micromax/sandbox/packages"
)

func createFakeWheel(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("creating zip entry: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("writing zip entry: %v", err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("closing zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestPackageStoreLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	store, err := packages.NewStore(tempDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	wheelData := createFakeWheel(t, map[string]string{
		"mymath/__init__.py": "def add(a, b): return a + b\n",
	})

	err = store.Add("python", "mymath", "mymath-1.0.0-py3-none-any.whl", wheelData)
	if err != nil {
		t.Fatalf("store.Add failed: %v", err)
	}

	foundData, filename, err := store.Find("python", "mymath")
	if err != nil {
		t.Fatalf("store.Find failed: %v", err)
	}
	if filename != "mymath-1.0.0-py3-none-any.whl" {
		t.Errorf("unexpected filename: %s", filename)
	}
	if len(foundData) != len(wheelData) {
		t.Errorf("data length mismatch: %d != %d", len(foundData), len(wheelData))
	}

	pkgs, err := store.List("python")
	if err != nil {
		t.Fatalf("store.List failed: %v", err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "mymath" {
		t.Fatalf("unexpected package list: %+v", pkgs)
	}

	// Test ExtractWheel
	targetDir := t.TempDir()
	err = packages.ExtractWheel(targetDir, wheelData)
	if err != nil {
		t.Fatalf("ExtractWheel failed: %v", err)
	}

	pyFile := filepath.Join(targetDir, "mymath", "__init__.py")
	content, err := os.ReadFile(pyFile)
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(content) != "def add(a, b): return a + b\n" {
		t.Errorf("unexpected extracted content: %s", string(content))
	}
}

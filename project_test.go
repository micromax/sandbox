package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/micromax/sandbox"
)

func TestLoadProjectFiles(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Create a simulated project directory structure
	_ = os.WriteFile(filepath.Join(tempDir, "main.py"), []byte("import utils\nprint(utils.calc())"), 0o644)
	_ = os.MkdirAll(filepath.Join(tempDir, "utils"), 0o755)
	_ = os.WriteFile(filepath.Join(tempDir, "utils", "calc.py"), []byte("def calc(): return 42"), 0o644)
	_ = os.MkdirAll(filepath.Join(tempDir, "data"), 0o755)
	_ = os.WriteFile(filepath.Join(tempDir, "data", "config.json"), []byte(`{"env": "test"}`), 0o644)

	// Subdirs that should be ignored
	_ = os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(tempDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644)
	_ = os.MkdirAll(filepath.Join(tempDir, "node_modules", "pkg"), 0o755)
	_ = os.WriteFile(filepath.Join(tempDir, "node_modules", "pkg", "index.js"), []byte("dummy"), 0o644)
	_ = os.MkdirAll(filepath.Join(tempDir, "__pycache__"), 0o755)
	_ = os.WriteFile(filepath.Join(tempDir, "__pycache__", "main.cpython.pyc"), []byte("bytecode"), 0o644)

	// 2. Load project files
	files, err := sandbox.LoadProjectFiles(tempDir)
	if err != nil {
		t.Fatalf("LoadProjectFiles failed: %v", err)
	}

	// 3. Verify collected files
	if _, ok := files["main.py"]; !ok {
		t.Errorf("missing main.py")
	}
	if _, ok := files["utils/calc.py"]; !ok {
		t.Errorf("missing utils/calc.py")
	}
	if _, ok := files["data/config.json"]; !ok {
		t.Errorf("missing data/config.json")
	}

	// 4. Verify ignored files
	for key := range files {
		if filepath.HasPrefix(key, ".git") || filepath.HasPrefix(key, "node_modules") || filepath.HasPrefix(key, "__pycache__") {
			t.Errorf("ignored file found in loaded project: %s", key)
		}
	}
}

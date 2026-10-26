package packages_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/micromax/sandbox/packages"
)

// dirBytes sums the size of every regular file under dir.
func dirBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// extractIntoSubdir extracts into <tmp>/target so tests can assert nothing
// escaped into <tmp>.
func extractIntoSubdir(t *testing.T, wheel []byte, lim packages.ExtractLimits) (parent, target string, err error) {
	t.Helper()
	parent = t.TempDir()
	target = filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return parent, target, packages.ExtractWheelWithLimits(target, wheel, lim)
}

func assertNothingEscaped(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "target" {
			t.Errorf("entry %q escaped the target directory", e.Name())
		}
	}
}

func TestExtractWheelRejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{
		"../evil.py",
		"pkg/../../evil.py",
		"/abs/evil.py",
		"..",
		"a/b/../../../evil.py",
	} {
		t.Run(name, func(t *testing.T) {
			wheel := createFakeWheel(t, map[string]string{"ok/__init__.py": "x = 1\n", name: "boom"})
			parent, _, err := extractIntoSubdir(t, wheel, packages.DefaultExtractLimits)
			if !errors.Is(err, packages.ErrInvalidArchive) {
				t.Fatalf("err = %v, want ErrInvalidArchive", err)
			}
			assertNothingEscaped(t, parent)
		})
	}
}

func TestExtractWheelAllowsDotDotPrefixedNames(t *testing.T) {
	// "..foo" is an ordinary name, not a traversal; the old HasPrefix("..") check rejected it.
	wheel := createFakeWheel(t, map[string]string{"..data/x.py": "y = 2\n"})
	_, target, err := extractIntoSubdir(t, wheel, packages.DefaultExtractLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "..data", "x.py")); err != nil {
		t.Fatal(err)
	}
}

func TestExtractWheelEntryLimit(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d"} {
		files["pkg/"+n+".py"] = "1"
	}
	_, _, err := extractIntoSubdir(t, createFakeWheel(t, files), packages.ExtractLimits{MaxEntries: 3})
	if !errors.Is(err, packages.ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
	}
}

func TestExtractWheelPerFileLimit(t *testing.T) {
	wheel := createFakeWheel(t, map[string]string{"pkg/big.py": strings.Repeat("x", 2000)})
	_, _, err := extractIntoSubdir(t, wheel, packages.ExtractLimits{MaxFileBytes: 1000})
	if !errors.Is(err, packages.ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
	}
}

func TestExtractWheelTotalLimit(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d"} {
		files["pkg/"+n+".py"] = strings.Repeat("x", 400) // each fine, 1600 together
	}
	_, target, err := extractIntoSubdir(t, createFakeWheel(t, files), packages.ExtractLimits{MaxFileBytes: 500, MaxTotalBytes: 1000})
	if !errors.Is(err, packages.ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
	}
	if got := dirBytes(t, target); got > 1000 {
		t.Fatalf("wrote %d bytes, budget was 1000", got)
	}
}

func TestExtractWheelDecompressionBomb(t *testing.T) {
	// 16 MiB of zeros deflates to ~16 KiB: tiny archive, huge extraction.
	wheel := createFakeWheel(t, map[string]string{"pkg/bomb.bin": strings.Repeat("\x00", 16<<20)})
	if len(wheel) > 1<<20 {
		t.Fatalf("test archive unexpectedly large: %d bytes", len(wheel))
	}
	const limit = 1 << 20
	_, target, err := extractIntoSubdir(t, wheel, packages.ExtractLimits{MaxFileBytes: limit})
	if !errors.Is(err, packages.ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrArchiveTooLarge", err)
	}
	if got := dirBytes(t, target); got > limit {
		t.Fatalf("wrote %d bytes, per-file limit is %d", got, limit)
	}
}

// The declared size is attacker-controlled. Forge it to look tiny: archive/zip notices
// the entry inflating past its declared size and fails, so nothing near the real size
// is written. The extractor's own byte budget is the second line of defence (see
// TestExtractWheelTotalLimit, where declared sizes are honest).
func TestExtractWheelForgedDeclaredSize(t *testing.T) {
	wheel := createFakeWheel(t, map[string]string{"pkg/bomb.bin": strings.Repeat("\x00", 4<<20)})

	cd := bytes.Index(wheel, []byte("PK\x01\x02"))
	if cd < 0 {
		t.Fatal("central directory header not found")
	}
	binary.LittleEndian.PutUint32(wheel[cd+24:], 10) // uncompressed size field

	zr, err := zip.NewReader(bytes.NewReader(wheel), int64(len(wheel)))
	if err != nil || zr.File[0].UncompressedSize64 != 10 {
		t.Fatalf("forgery did not take effect: err=%v", err)
	}

	const limit = 1 << 20
	_, target, err := extractIntoSubdir(t, wheel, packages.ExtractLimits{MaxFileBytes: limit})
	if !errors.Is(err, packages.ErrInvalidArchive) && !errors.Is(err, packages.ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want ErrInvalidArchive or ErrArchiveTooLarge", err)
	}
	if got := dirBytes(t, target); got > limit {
		t.Fatalf("wrote %d bytes, limit is %d", got, limit)
	}
}

func TestExtractWheelStripsSpecialModesAndSkipsSymlinks(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	h := &zip.FileHeader{Name: "pkg/tool.py"}
	h.SetMode(0o4755) // setuid + rwxr-xr-x
	w, _ := zw.CreateHeader(h)
	w.Write([]byte("print(1)\n"))

	ln := &zip.FileHeader{Name: "pkg/link"}
	ln.SetMode(os.ModeSymlink | 0o777)
	w, _ = zw.CreateHeader(ln)
	w.Write([]byte("/etc/passwd"))

	zw.Close()

	_, target, err := extractIntoSubdir(t, buf.Bytes(), packages.DefaultExtractLimits)
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(filepath.Join(target, "pkg", "tool.py"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Errorf("special mode bits survived extraction: %v", info.Mode())
	}
	if _, err := os.Lstat(filepath.Join(target, "pkg", "link")); !os.IsNotExist(err) {
		t.Errorf("symlink entry should have been skipped, Lstat err = %v", err)
	}
}

func TestExtractWheelRejectsGarbage(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("not a zip"), []byte("PK\x03\x04truncated")} {
		if err := packages.ExtractWheel(t.TempDir(), data); !errors.Is(err, packages.ErrInvalidArchive) {
			t.Errorf("ExtractWheel(%q) err = %v, want ErrInvalidArchive", data, err)
		}
	}
}

func FuzzExtractWheel(f *testing.F) {
	seed := func(files map[string]string) []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for name, content := range files {
			w, _ := zw.Create(name)
			w.Write([]byte(content))
		}
		zw.Close()
		return buf.Bytes()
	}
	f.Add(seed(map[string]string{"pkg/__init__.py": "x = 1\n", "pkg-1.0.dist-info/METADATA": "Name: pkg\n"}))
	f.Add(seed(map[string]string{"../evil.py": "x"}))
	f.Add(seed(map[string]string{"/abs.py": "x", "a/../../b.py": "y"}))
	f.Add(seed(map[string]string{"big.bin": strings.Repeat("\x00", 1<<20)}))
	f.Add([]byte{})

	lim := packages.ExtractLimits{MaxEntries: 64, MaxFileBytes: 32 << 10, MaxTotalBytes: 64 << 10}

	f.Fuzz(func(t *testing.T, data []byte) {
		parent := t.TempDir()
		target := filepath.Join(parent, "target")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}

		_ = packages.ExtractWheelWithLimits(target, data, lim) // any error is fine; panics and escapes are not

		entries, err := os.ReadDir(parent)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "target" {
			t.Fatalf("extraction wrote outside target: %v", entries)
		}

		var total int64
		err = filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				t.Errorf("symlink created: %s", p)
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
				t.Errorf("special mode bits on %s: %v", p, info.Mode())
			}
			total += info.Size()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if total > lim.MaxTotalBytes {
			t.Fatalf("wrote %d bytes, total limit is %d", total, lim.MaxTotalBytes)
		}
	})
}

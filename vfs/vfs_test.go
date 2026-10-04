package vfs

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
)

func TestWriteReadRoundTrip(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	if err := f.WriteFile("a/b/c.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := f.ReadFile("a/b/c.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	st := f.Stats()
	if st.Bytes != 5 || st.Files != 1 || st.Dirs != 2 {
		t.Fatalf("Stats = %+v", st)
	}
}

func TestDataIsCopied(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	in := []byte("abc")
	f.WriteFile("x", in)
	in[0] = 'Z'
	got, _ := f.ReadFile("x")
	if string(got) != "abc" {
		t.Fatal("WriteFile must copy its input")
	}
	got[0] = 'Q'
	again, _ := f.ReadFile("x")
	if string(again) != "abc" {
		t.Fatal("ReadFile must return a copy")
	}
}

func TestOverwriteAccounting(t *testing.T) {
	f := New(10, UnlimitedFiles)
	f.WriteFile("a", []byte("12345678"))
	if err := f.WriteFile("a", []byte("12")); err != nil {
		t.Fatal(err)
	}
	if f.Stats().Bytes != 2 {
		t.Fatalf("bytes = %d, want 2", f.Stats().Bytes)
	}
	// After shrinking, there is room again.
	if err := f.WriteFile("b", []byte("12345678")); err != nil {
		t.Fatal(err)
	}
}

func TestByteQuota(t *testing.T) {
	f := New(10, UnlimitedFiles)
	if err := f.WriteFile("a", []byte("123456")); err != nil {
		t.Fatal(err)
	}
	err := f.WriteFile("b", []byte("12345"))
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrQuota", err)
	}
	if _, err := f.ReadFile("b"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("failed write must not leave a file behind")
	}
	if f.Stats().Bytes != 6 {
		t.Fatalf("bytes = %d, want 6", f.Stats().Bytes)
	}
}

func TestOverwriteGrowthCountsAgainstQuota(t *testing.T) {
	f := New(10, UnlimitedFiles)
	f.WriteFile("a", []byte("12345"))
	if err := f.WriteFile("a", []byte("12345678901")); !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrQuota", err)
	}
	got, _ := f.ReadFile("a")
	if string(got) != "12345" {
		t.Fatalf("failed overwrite must keep old data, got %q", got)
	}
}

func TestQuotaNearMaxUint64DoesNotOverflow(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	if err := f.WriteFile("a", make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
}

func TestNodeLimit(t *testing.T) {
	f := New(Unlimited, 3)
	if err := f.WriteFile("d/a", nil); err != nil { // dir d + file a = 2 nodes
		t.Fatal(err)
	}
	if err := f.WriteFile("d/b", nil); err != nil { // 3 nodes
		t.Fatal(err)
	}
	err := f.WriteFile("d/c", nil)
	if !errors.Is(err, ErrTooManyFiles) || !errors.Is(err, ErrQuota) {
		t.Fatalf("err = %v, want ErrTooManyFiles wrapping ErrQuota", err)
	}
	// Overwriting an existing file creates no node and must still work.
	if err := f.WriteFile("d/a", []byte("x")); err != nil {
		t.Fatal(err)
	}
}

func TestNodeLimitCountsMissingParents(t *testing.T) {
	f := New(Unlimited, 2)
	if err := f.WriteFile("a/b/c", nil); !errors.Is(err, ErrTooManyFiles) {
		t.Fatalf("err = %v, want ErrTooManyFiles", err)
	}
	if st := f.Stats(); st.Dirs != 0 || st.Files != 0 {
		t.Fatalf("failed write left debris: %+v", st)
	}
}

func TestManyEmptyDirsAreBounded(t *testing.T) {
	f := New(Unlimited, 10)
	var last error
	for i := 0; i < 1000; i++ {
		if last = f.MkdirAll(fmt.Sprintf("d%d", i)); last != nil {
			break
		}
	}
	if !errors.Is(last, ErrTooManyFiles) {
		t.Fatalf("expected ErrTooManyFiles, got %v", last)
	}
	if f.Stats().Dirs != 10 {
		t.Fatalf("dirs = %d, want 10", f.Stats().Dirs)
	}
}

func TestStructureErrors(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	f.WriteFile("file", []byte("x"))
	f.MkdirAll("dir")

	if err := f.WriteFile("file/child", nil); !errors.Is(err, ErrNotDir) {
		t.Errorf("write under file: %v, want ErrNotDir", err)
	}
	if err := f.WriteFile("dir", nil); !errors.Is(err, ErrIsDir) {
		t.Errorf("write over dir: %v, want ErrIsDir", err)
	}
	if err := f.WriteFile("/", nil); !errors.Is(err, ErrIsDir) {
		t.Errorf("write to root: %v, want ErrIsDir", err)
	}
	if err := f.MkdirAll("file/x"); !errors.Is(err, ErrNotDir) {
		t.Errorf("mkdir under file: %v, want ErrNotDir", err)
	}
	if _, err := f.ReadFile("dir"); !errors.Is(err, ErrIsDir) {
		t.Errorf("read dir: %v, want ErrIsDir", err)
	}
	if _, err := f.ReadFile("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("read missing: %v, want ErrNotExist", err)
	}
}

func TestTraversalRejectedEverywhere(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	for _, p := range []string{"../x", `..\x`, "a/../../x", `C:\x`, "a\x00b"} {
		if err := f.WriteFile(p, []byte("x")); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("WriteFile(%q) = %v", p, err)
		}
		if err := f.MkdirAll(p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("MkdirAll(%q) = %v", p, err)
		}
		if err := f.Remove(p); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Remove(%q) = %v", p, err)
		}
		if _, err := f.ReadFile(p); err == nil {
			t.Errorf("ReadFile(%q) succeeded", p)
		}
	}
	if st := f.Stats(); st.Files != 0 || st.Dirs != 0 {
		t.Fatalf("hostile writes left state: %+v", st)
	}
}

func TestRemove(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	f.WriteFile("d/a", []byte("123"))
	f.WriteFile("d/sub/b", []byte("45"))
	f.WriteFile("keep", []byte("6"))

	if err := f.Remove("d"); err != nil {
		t.Fatal(err)
	}
	if st := f.Stats(); st.Bytes != 1 || st.Files != 1 || st.Dirs != 0 {
		t.Fatalf("Stats after remove = %+v", st)
	}
	if err := f.Remove("nope/never"); err != nil {
		t.Fatalf("removing missing path: %v", err)
	}
	if err := f.Remove("/"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("removing root: %v", err)
	}
	// Removed space is reusable.
	small := New(4, UnlimitedFiles)
	small.WriteFile("a", []byte("1234"))
	small.Remove("a")
	if err := small.WriteFile("b", []byte("1234")); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshot(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	f.WriteFile("out/a.txt", []byte("A"))
	f.WriteFile("out/dir/b.txt", []byte("B"))
	f.WriteFile("in/secret", []byte("S"))

	snap := f.Snapshot("out")
	if len(snap) != 2 || string(snap["a.txt"]) != "A" || string(snap["dir/b.txt"]) != "B" {
		t.Fatalf("snapshot = %v", snap)
	}
	if _, leaked := snap["secret"]; leaked {
		t.Fatal("snapshot leaked a file outside its prefix")
	}
	snap["a.txt"][0] = 'Z'
	if again := f.Snapshot("out"); string(again["a.txt"]) != "A" {
		t.Fatal("snapshot must hold copies")
	}
	if got := f.Snapshot("../in"); len(got) != 0 {
		t.Fatalf("hostile prefix returned %v", got)
	}
	if got := f.Snapshot("missing"); len(got) != 0 {
		t.Fatalf("missing prefix returned %v", got)
	}
	if got := f.Snapshot("out/a.txt"); len(got) != 0 {
		t.Fatalf("file prefix returned %v", got)
	}
}

func TestImplementsFSInterface(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	f.WriteFile("a.txt", []byte("alpha"))
	f.WriteFile("dir/b.txt", []byte("beta"))
	f.WriteFile("dir/sub/c.txt", []byte("gamma"))
	f.MkdirAll("empty")

	if err := fstest.TestFS(f, "a.txt", "dir/b.txt", "dir/sub/c.txt", "empty"); err != nil {
		t.Fatal(err)
	}
	var _ fs.ReadFileFS = f
	var _ fs.ReadDirFS = f
	var _ fs.StatFS = f
}

func TestOpenRejectsInvalidNames(t *testing.T) {
	f := New(Unlimited, UnlimitedFiles)
	for _, n := range []string{"../x", "/a", "a/", "a//b", ""} {
		if _, err := f.Open(n); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Open(%q) = %v, want ErrInvalid", n, err)
		}
	}
}

func TestConcurrentWritesRespectQuota(t *testing.T) {
	const quota = 1000
	f := New(quota, UnlimitedFiles)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			f.WriteFile(fmt.Sprintf("f%d", i), make([]byte, 100))
		}(i)
	}
	wg.Wait()
	if b := f.Stats().Bytes; b > quota {
		t.Fatalf("bytes %d exceeded quota %d", b, quota)
	}
	var total uint64
	for _, v := range f.Snapshot(".") {
		total += uint64(len(v))
	}
	if total != f.Stats().Bytes {
		t.Fatalf("accounting drift: counted %d, stats %d", total, f.Stats().Bytes)
	}
}

package docker

import (
	"archive/tar"
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/micromax/sandbox"
)

func TestTarRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"hello.txt":     []byte("hello world"),
		"sub/data.json": []byte(`{"key": "value"}`),
	}

	tarData, err := buildArchive(files, "work")
	if err != nil {
		t.Fatalf("buildArchive: %v", err)
	}

	extracted, err := extractAndSanitizeArchive(bytes.NewReader(tarData), 1024*1024, 100)
	if err != nil {
		t.Fatalf("extractAndSanitizeArchive: %v", err)
	}

	if string(extracted["work/hello.txt"]) != "hello world" {
		t.Fatalf("unexpected content for hello.txt: %s", extracted["work/hello.txt"])
	}
	if string(extracted["work/sub/data.json"]) != `{"key": "value"}` {
		t.Fatalf("unexpected content for sub/data.json: %s", extracted["work/sub/data.json"])
	}
}

func TestHostileTarRejected(t *testing.T) {
	tests := []struct {
		name     string
		header   *tar.Header
		content  []byte
		maxBytes uint64
		maxFiles int
		wantErr  error
		errSub   string
	}{
		{
			name: "path traversal dot dot",
			header: &tar.Header{
				Name:     "../../etc/passwd",
				Mode:     0o644,
				Size:     4,
				Typeflag: tar.TypeReg,
			},
			content:  []byte("root"),
			maxBytes: 1024,
			maxFiles: 10,
			errSub:   "hostile tar entry rejected",
		},
		{
			name: "absolute path",
			header: &tar.Header{
				Name:     "/etc/shadow",
				Mode:     0o644,
				Size:     4,
				Typeflag: tar.TypeReg,
			},
			content:  []byte("test"),
			maxBytes: 1024,
			maxFiles: 10,
			errSub:   "hostile tar entry rejected",
		},
		{
			name: "symlink forbidden",
			header: &tar.Header{
				Name:     "symlink_file",
				Linkname: "/etc/passwd",
				Typeflag: tar.TypeSymlink,
			},
			maxBytes: 1024,
			maxFiles: 10,
			errSub:   "hostile tar entry type",
		},
		{
			name: "byte quota exceeded",
			header: &tar.Header{
				Name:     "big.bin",
				Mode:     0o644,
				Size:     100,
				Typeflag: tar.TypeReg,
			},
			content:  bytes.Repeat([]byte("A"), 100),
			maxBytes: 50,
			maxFiles: 10,
			wantErr:  sandbox.ErrFSQuota,
		},
		{
			name: "file count quota exceeded",
			header: &tar.Header{
				Name:     "file.txt",
				Mode:     0o644,
				Size:     4,
				Typeflag: tar.TypeReg,
			},
			content:  []byte("test"),
			maxBytes: 1024,
			maxFiles: 0, // quota is 0 allowed
			wantErr:  sandbox.ErrFSQuota,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			if err := tw.WriteHeader(tc.header); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
			if len(tc.content) > 0 {
				if _, err := tw.Write(tc.content); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}
			_ = tw.Close()

			_, err := extractAndSanitizeArchive(&buf, tc.maxBytes, tc.maxFiles)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
			if tc.errSub != "" && !strings.Contains(err.Error(), tc.errSub) {
				t.Fatalf("expected error containing %q, got: %v", tc.errSub, err)
			}
		})
	}
}

package docker

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/micromax/sandbox"
)

// buildArchive creates a tar stream containing the given files mapped under baseDir.
func buildArchive(files map[string][]byte, baseDir string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for relPath, content := range files {
		cleanRel := strings.TrimPrefix(path.Clean("/"+filepathToSlash(relPath)), "/")
		tarPath := path.Join(baseDir, cleanRel)

		hdr := &tar.Header{
			Name:     tarPath,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("tar write header: %w", err)
		}
		if _, err := tw.Write(content); err != nil {
			return nil, fmt.Errorf("tar write content: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar close: %w", err)
	}
	return buf.Bytes(), nil
}

// extractAndSanitizeArchive extracts files from a tar archive, enforcing strict security constraints:
// - Rejects directory traversal (e.g. "../", absolute paths)
// - Rejects symlinks, hardlinks, character/block devices, and fifos
// - Enforces hard limits on cumulative byte volume and total file count
func extractAndSanitizeArchive(r io.Reader, maxBytes uint64, maxFiles int) (map[string][]byte, error) {
	tr := tar.NewReader(r)
	out := make(map[string][]byte)

	var totalBytes uint64
	totalFiles := 0

	for {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("reading tar header: %w", err)
		}

		// 1. Path sanitization
		if strings.Contains(hdr.Name, "..") {
			return nil, fmt.Errorf("hostile tar entry rejected: %q", hdr.Name)
		}
		if strings.HasPrefix(hdr.Name, "/") || strings.HasPrefix(hdr.Name, "\\") {
			return nil, fmt.Errorf("hostile tar entry rejected: %q", hdr.Name)
		}

		rawPath := filepathToSlash(hdr.Name)
		rawPath = strings.TrimPrefix(rawPath, "out/")
		rawPath = strings.TrimPrefix(rawPath, "out")
		clean := path.Clean("/" + rawPath)
		clean = strings.TrimPrefix(clean, "/")

		if clean == "" || clean == "." {
			continue
		}

		// 2. Reject unsafe file types (symlinks, devices, fifos)
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("hostile tar entry type %c rejected for %q", hdr.Typeflag, hdr.Name)
		}

		// 3. Count limits
		totalFiles++
		if maxFiles != sandbox.UnlimitedCount && maxFiles >= 0 && totalFiles > maxFiles {
			return nil, sandbox.ErrFSQuota
		}

		// 4. Size limits
		if maxBytes != sandbox.Unlimited && maxBytes > 0 && (totalBytes+uint64(hdr.Size) > maxBytes) {
			return nil, sandbox.ErrFSQuota
		}

		// Read file content with limit
		var fileBuf bytes.Buffer
		lr := io.LimitReader(tr, int64(maxBytes-totalBytes+1))
		n, err := io.Copy(&fileBuf, lr)
		if err != nil {
			return nil, fmt.Errorf("reading tar entry %q: %w", hdr.Name, err)
		}

		totalBytes += uint64(n)
		if maxBytes != sandbox.Unlimited && maxBytes > 0 && totalBytes > maxBytes {
			return nil, sandbox.ErrFSQuota
		}

		out[clean] = fileBuf.Bytes()
	}

	return out, nil
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

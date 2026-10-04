package wasm

import (
	"archive/zip"
	"io"
)

func newZipReader(r io.ReaderAt, size int64) (*zip.Reader, error) {
	return zip.NewReader(r, size)
}

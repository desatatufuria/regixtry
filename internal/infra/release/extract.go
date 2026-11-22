package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Extraction limits guard against decompression bombs in a hostile or
// compromised release archive (variables so tests can shrink them).
var (
	maxExtractEntryBytes int64 = 512 << 20
	maxExtractTotalBytes int64 = 1 << 30
	maxExtractEntries          = 10000
)

// ErrArchiveTooLarge is returned when an archive exceeds an extraction limit.
var ErrArchiveTooLarge = errors.New("archive exceeds extraction limits")

// totalLimitReader fails (rather than truncating) once more than limit
// decompressed bytes have been read, including bytes skipped by the tar
// reader for non-matching entries.
type totalLimitReader struct {
	r    io.Reader
	left int64
}

func (l *totalLimitReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, ErrArchiveTooLarge
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n, ErrArchiveTooLarge
	}
	return n, err
}

// ErrBinaryNotFound is returned by ExtractBinary when no archive entry's
// base name matches the requested binary name.
var ErrBinaryNotFound = errors.New("binary not found in archive")

// ExtractBinary extracts the regular-file entry whose base name equals
// binaryName from the gzip-compressed tar archive at archivePath into
// destinationDir, returning the extracted binary's path. Matching on
// filepath.Base(header.Name) means archives that also carry README/LICENSE
// files (or nest the binary under a directory) still resolve correctly.
func ExtractBinary(ctx context.Context, archivePath string, destinationDir string, binaryName string) (string, error) {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer archiveFile.Close()
	gzr, err := gzip.NewReader(archiveFile)
	if err != nil {
		return "", err
	}
	defer gzr.Close()
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", err
	}
	tr := tar.NewReader(&totalLimitReader{r: gzr, left: maxExtractTotalBytes})
	for entries := 0; ; entries++ {
		if entries >= maxExtractEntries {
			return "", ErrArchiveTooLarge
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrBinaryNotFound
			}
			return "", err
		}
		if filepath.Base(strings.TrimSpace(header.Name)) != binaryName || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size > maxExtractEntryBytes {
			return "", ErrArchiveTooLarge
		}
		binaryPath := filepath.Join(destinationDir, binaryName)
		// Never write through a pre-existing path: remove whatever is there
		// (a symlink is removed itself, not followed), then create exclusively
		// without following links.
		if err := os.Remove(binaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		file, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o755)
		if err != nil {
			return "", err
		}
		written, err := io.Copy(file, io.LimitReader(tr, maxExtractEntryBytes+1))
		if err == nil && written > maxExtractEntryBytes {
			err = ErrArchiveTooLarge
		}
		if err != nil {
			_ = file.Close()
			_ = os.Remove(binaryPath)
			return "", err
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(binaryPath)
			return "", err
		}
		return binaryPath, nil
	}
}

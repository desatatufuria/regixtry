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
)

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
	tr := tar.NewReader(gzr)
	for {
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
		binaryPath := filepath.Join(destinationDir, binaryName)
		file, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(file, tr); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return binaryPath, nil
	}
}

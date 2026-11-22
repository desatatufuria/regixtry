package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractBinaryMatchesEntryByBaseNameAmongOtherFiles(t *testing.T) {
	t.Parallel()

	archiveBody := makeReleaseArchive(t, map[string]string{
		"README.md":      "docs",
		"nested/example": "example-binary",
	})
	archivePath := filepath.Join(t.TempDir(), "example_1.0.0.tar.gz")
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}

	binaryPath, err := ExtractBinary(context.Background(), archivePath, filepath.Join(t.TempDir(), "extract"), "example")
	if err != nil {
		t.Fatalf("ExtractBinary() error = %v", err)
	}
	body, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("ReadFile(binary) error = %v", err)
	}
	if string(body) != "example-binary" {
		t.Fatalf("binary body = %q, want extracted payload", string(body))
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("Stat(binary) error = %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("binary mode = %v, want executable permissions", info.Mode())
	}
}

func TestExtractBinaryFailsWhenNameNotFound(t *testing.T) {
	t.Parallel()

	archiveBody := makeReleaseArchive(t, map[string]string{"README.md": "docs"})
	archivePath := filepath.Join(t.TempDir(), "example.tar.gz")
	if err := os.WriteFile(archivePath, archiveBody, 0o600); err != nil {
		t.Fatalf("WriteFile(archive) error = %v", err)
	}
	if _, err := ExtractBinary(context.Background(), archivePath, filepath.Join(t.TempDir(), "extract"), "example"); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("ExtractBinary() error = %v, want ErrBinaryNotFound", err)
	}
}

func makeReleaseArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var raw bytes.Buffer
	gzw := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gzw)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("Write(%s) error = %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close(tar) error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("Close(gzip) error = %v", err)
	}
	return raw.Bytes()
}

// Not parallel: shrinks package-level extraction limits.
func TestExtractBinaryRejectsDecompressionBombsAndRemovesPartialOutput(t *testing.T) {
	oldEntry, oldTotal, oldEntries := maxExtractEntryBytes, maxExtractTotalBytes, maxExtractEntries
	defer func() { maxExtractEntryBytes, maxExtractTotalBytes, maxExtractEntries = oldEntry, oldTotal, oldEntries }()
	maxExtractEntryBytes, maxExtractTotalBytes, maxExtractEntries = 1<<20, 4<<20, 50

	bomb := func(files map[string]int, order []string) string {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, name := range order {
			size := files[name]
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(size), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(make([]byte, size)); err != nil { // zeros: highly compressible
				t.Fatal(err)
			}
		}
		_ = tw.Close()
		_ = gz.Close()
		path := filepath.Join(t.TempDir(), "a.tar.gz")
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("oversized entry", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "x")
		_, err := ExtractBinary(context.Background(), bomb(map[string]int{"example": 2 << 20}, []string{"example"}), dest, "example")
		if !errors.Is(err, ErrArchiveTooLarge) {
			t.Fatalf("error = %v, want ErrArchiveTooLarge", err)
		}
		if _, statErr := os.Lstat(filepath.Join(dest, "example")); !os.IsNotExist(statErr) {
			t.Fatalf("partial output left behind: %v", statErr)
		}
	})
	t.Run("total decompressed size across skipped entries", func(t *testing.T) {
		files := map[string]int{"a": 1 << 20, "b": 1 << 20, "c": 1 << 20, "d": 1 << 20, "e": 1 << 20, "example": 10}
		_, err := ExtractBinary(context.Background(), bomb(files, []string{"a", "b", "c", "d", "e", "example"}), filepath.Join(t.TempDir(), "x"), "example")
		if !errors.Is(err, ErrArchiveTooLarge) {
			t.Fatalf("error = %v, want ErrArchiveTooLarge", err)
		}
	})
	t.Run("entry count", func(t *testing.T) {
		files := map[string]int{"example": 1}
		order := []string{}
		for i := 0; i < 60; i++ {
			name := "f" + string(rune('A'+i%26)) + string(rune('a'+i/26))
			files[name] = 0
			order = append(order, name)
		}
		order = append(order, "example")
		_, err := ExtractBinary(context.Background(), bomb(files, order), filepath.Join(t.TempDir(), "x"), "example")
		if !errors.Is(err, ErrArchiveTooLarge) {
			t.Fatalf("error = %v, want ErrArchiveTooLarge", err)
		}
	})
}

func TestExtractBinaryDoesNotFollowAPreexistingSymlink(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "extract")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dest, "example")); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(archivePath, makeReleaseArchive(t, map[string]string{"example": "new-binary"}), 0o600); err != nil {
		t.Fatal(err)
	}
	binaryPath, err := ExtractBinary(context.Background(), archivePath, dest, "example")
	if err != nil {
		t.Fatalf("ExtractBinary() error = %v", err)
	}
	if got, _ := os.ReadFile(victim); string(got) != "precious" {
		t.Fatalf("symlink target was overwritten: %q", got)
	}
	info, err := os.Lstat(binaryPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("binary = %v err=%v, want a regular file replacing the symlink", info, err)
	}
	if got, _ := os.ReadFile(binaryPath); string(got) != "new-binary" {
		t.Fatalf("binary body = %q", got)
	}
}

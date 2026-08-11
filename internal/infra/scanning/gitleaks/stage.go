package gitleaks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// stagedBlob is one blob copied to local disk for scanning.
type stagedBlob struct {
	Path       string
	Descriptor domain.Descriptor
	IsConfig   bool
}

// stagingResult is the outcome of staging a manifest's blobs for a single
// scan run.
type stagingResult struct {
	RunDir  string
	ScanDir string
	Staged  []stagedBlob
	Skipped []domain.Descriptor
}

// stageManifestBlobs copies each blob in blobs to a fresh work/<run>/scan
// directory under workRoot, per design.md decision 7: whole-blob copies only
// (never per-entry extraction of any archive contents — gitleaks' own
// --max-archive-depth handles that in-process), mode 0600, filename and
// extension derived from the manifest's declared mediaType. Blobs whose
// mediaType is not recognised are recorded as skipped, never silently
// dropped. The caller MUST always invoke the returned cleanup function,
// which removes the entire run directory (work/<run>).
func stageManifestBlobs(ctx context.Context, store ports.BlobStore, workRoot string, blobs []domain.Descriptor) (stagingResult, func(), error) {
	noop := func() {}
	if store == nil {
		return stagingResult{}, noop, fmt.Errorf("gitleaks runner blob store is not configured")
	}
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return stagingResult{}, noop, err
	}
	runDir, err := os.MkdirTemp(workRoot, "run-")
	if err != nil {
		return stagingResult{}, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(runDir) }

	scanDir := filepath.Join(runDir, "scan")
	layersDir := filepath.Join(scanDir, "layers")
	configDir := filepath.Join(scanDir, "config")

	result := stagingResult{RunDir: runDir, ScanDir: scanDir}
	layerIndex := 0
	for _, blob := range blobs {
		ext, isConfig, ok := stagingExtension(blob.MediaType)
		if !ok {
			result.Skipped = append(result.Skipped, blob)
			continue
		}
		var destDir, filename string
		if isConfig {
			destDir = configDir
			filename = "config" + ext
		} else {
			layerIndex++
			destDir = layersDir
			filename = fmt.Sprintf("%03d-%s%s", layerIndex, digest12(blob.Digest), ext)
		}
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			cleanup()
			return stagingResult{}, noop, err
		}
		destPath := filepath.Join(destDir, filename)
		if err := copyBlob(ctx, store, destPath, blob.Digest); err != nil {
			cleanup()
			return stagingResult{}, noop, err
		}
		result.Staged = append(result.Staged, stagedBlob{Path: destPath, Descriptor: blob, IsConfig: isConfig})
	}
	return result, cleanup, nil
}

// copyBlob streams one whole blob from the BlobStore to destPath. It never
// extracts, inspects, or interprets archive entries itself — only a full,
// unmodified byte-for-byte copy is written, at mode 0600.
func copyBlob(ctx context.Context, store ports.BlobStore, destPath string, digest domain.Digest) error {
	reader, _, err := store.OpenBlob(ctx, digest)
	if err != nil {
		return err
	}
	defer reader.Close()

	file, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, reader)
	return err
}

// stagingExtension maps a manifest-declared mediaType to a staging file
// extension and whether it is the image config blob, per design.md's Exec
// Surface mediaType table. An unrecognised mediaType reports ok=false so the
// caller records the blob as skipped instead of guessing an extension.
func stagingExtension(mediaType string) (ext string, isConfig bool, ok bool) {
	trimmed := strings.TrimSpace(mediaType)
	switch {
	case strings.HasSuffix(trimmed, "config.v1+json"):
		return ".json", true, true
	case strings.HasSuffix(trimmed, "tar+gzip"), strings.HasSuffix(trimmed, "tar.gzip"):
		return ".tar.gz", false, true
	case strings.HasSuffix(trimmed, "tar+zstd"):
		return ".tar.zst", false, true
	case strings.HasSuffix(trimmed, "tar"):
		return ".tar", false, true
	default:
		return "", false, false
	}
}

// digest12 returns the first 12 hex characters of digest's encoded value,
// matching design.md's work/<run>/scan/layers/<NNN>-<digest12><ext> shape.
func digest12(digest domain.Digest) string {
	encoded := digest.Encoded()
	if len(encoded) < 12 {
		return encoded
	}
	return encoded[:12]
}

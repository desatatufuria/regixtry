package gitleaks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// fakeBlobStore implements ports.BlobStore for staging tests. Only OpenBlob
// is exercised by stageManifestBlobs; every other method is unused here and
// returns an error if accidentally called.
type fakeBlobStore struct {
	blobs map[domain.Digest][]byte
}

func (f *fakeBlobStore) BeginUpload(context.Context, domain.RepositoryRef) (domain.UploadState, error) {
	return domain.UploadState{}, fmt.Errorf("BeginUpload not implemented in fakeBlobStore")
}

func (f *fakeBlobStore) GetUpload(context.Context, string) (domain.UploadState, error) {
	return domain.UploadState{}, fmt.Errorf("GetUpload not implemented in fakeBlobStore")
}

func (f *fakeBlobStore) PutUploadChunk(context.Context, string, io.Reader) (domain.UploadState, error) {
	return domain.UploadState{}, fmt.Errorf("PutUploadChunk not implemented in fakeBlobStore")
}

func (f *fakeBlobStore) CommitUpload(context.Context, string, domain.Digest) (domain.Descriptor, error) {
	return domain.Descriptor{}, fmt.Errorf("CommitUpload not implemented in fakeBlobStore")
}

func (f *fakeBlobStore) CancelUpload(context.Context, string) error {
	return fmt.Errorf("CancelUpload not implemented in fakeBlobStore")
}

func (f *fakeBlobStore) BlobExists(context.Context, domain.Digest) (bool, error) {
	_, ok := f.blobs[domain.Digest("")]
	return ok, nil
}

func (f *fakeBlobStore) ListBlobs(context.Context) ([]ports.BlobFileInfo, error) {
	return nil, nil
}

func (f *fakeBlobStore) DeleteBlob(context.Context, domain.Digest) (bool, error) {
	return false, fmt.Errorf("DeleteBlob not implemented in fakeBlobStore")
}

func (f *fakeBlobStore) OpenBlob(_ context.Context, digest domain.Digest) (io.ReadSeekCloser, domain.Descriptor, error) {
	body, ok := f.blobs[digest]
	if !ok {
		return nil, domain.Descriptor{}, fmt.Errorf("blob %s not found in fakeBlobStore", digest)
	}
	return nopReadSeekCloser{strings.NewReader(string(body))}, domain.Descriptor{Digest: digest, Size: int64(len(body))}, nil
}

type nopReadSeekCloser struct {
	*strings.Reader
}

func (nopReadSeekCloser) Close() error { return nil }

// TestStageManifestBlobsWritesFilenamesAndExtensionsPerMediaTypeMap is the
// staging RED test (tasks.md 4.2): the config blob and each layer must land
// at the exact paths design.md's Exec Surface mediaType table describes, an
// unrecognised mediaType must be skipped rather than erroring, and cleanup()
// must remove the entire work/<run> directory afterward.
func TestStageManifestBlobsWritesFilenamesAndExtensionsPerMediaTypeMap(t *testing.T) {
	t.Parallel()

	configDigest := domain.DigestFromBytes([]byte("config-body"))
	layerGzipDigest := domain.DigestFromBytes([]byte("layer-gzip-body"))
	layerTarDigest := domain.DigestFromBytes([]byte("layer-tar-body"))
	layerZstdDigest := domain.DigestFromBytes([]byte("layer-zstd-body"))
	unknownDigest := domain.DigestFromBytes([]byte("unknown-body"))

	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{
		configDigest:    []byte("config-body"),
		layerGzipDigest: []byte("layer-gzip-body"),
		layerTarDigest:  []byte("layer-tar-body"),
		layerZstdDigest: []byte("layer-zstd-body"),
		unknownDigest:   []byte("unknown-body"),
	}}

	blobs := []domain.Descriptor{
		{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: 11},
		{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: layerGzipDigest, Size: 15},
		{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerTarDigest, Size: 14},
		{MediaType: "application/vnd.oci.image.layer.v1.tar+zstd", Digest: layerZstdDigest, Size: 15},
		{MediaType: "application/vnd.example.unknown+octet-stream", Digest: unknownDigest, Size: 12},
	}

	workRoot := t.TempDir()
	result, cleanup, err := stageManifestBlobs(context.Background(), store, workRoot, blobs)
	if err != nil {
		t.Fatalf("stageManifestBlobs() error = %v", err)
	}

	wantConfig := filepath.Join(result.ScanDir, "config", "config.json")
	wantGzip := filepath.Join(result.ScanDir, "layers", fmt.Sprintf("001-%s.tar.gz", digest12(layerGzipDigest)))
	wantTar := filepath.Join(result.ScanDir, "layers", fmt.Sprintf("002-%s.tar", digest12(layerTarDigest)))
	wantZstd := filepath.Join(result.ScanDir, "layers", fmt.Sprintf("003-%s.tar.zst", digest12(layerZstdDigest)))

	gotPaths := make(map[string]bool, len(result.Staged))
	for _, staged := range result.Staged {
		gotPaths[staged.Path] = true
	}
	for _, want := range []string{wantConfig, wantGzip, wantTar, wantZstd} {
		if !gotPaths[want] {
			t.Fatalf("staged paths = %#v, want to contain %q", result.Staged, want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("Stat(%s) error = %v, want the blob written to disk", want, err)
		}
	}
	if len(result.Staged) != 4 {
		t.Fatalf("len(result.Staged) = %d, want 4 recognised blobs", len(result.Staged))
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Digest != unknownDigest {
		t.Fatalf("result.Skipped = %#v, want the unknown-mediaType blob recorded as skipped, not dropped or errored", result.Skipped)
	}

	runDir := result.RunDir
	cleanup()
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Fatalf("Stat(%s) after cleanup() error = %v, want work/<run> removed", runDir, err)
	}
}

// TestStageManifestBlobsWritesBlobsWithRestrictivePermissionsAndNoOutsideEntries
// is the binary-provenance / executable-classification threat-matrix RED
// test (tasks.md 4.3): every staged blob must be written mode 0600 (whole
// blob, never per-entry extraction) and the run directory tree must contain
// no entry outside work/<run>/.
func TestStageManifestBlobsWritesBlobsWithRestrictivePermissionsAndNoOutsideEntries(t *testing.T) {
	t.Parallel()

	layerDigest := domain.DigestFromBytes([]byte("layer-body"))
	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{layerDigest: []byte("layer-body")}}
	blobs := []domain.Descriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerDigest, Size: 10}}

	workRoot := t.TempDir()
	result, cleanup, err := stageManifestBlobs(context.Background(), store, workRoot, blobs)
	if err != nil {
		t.Fatalf("stageManifestBlobs() error = %v", err)
	}
	defer cleanup()

	if len(result.Staged) != 1 {
		t.Fatalf("len(result.Staged) = %d, want 1", len(result.Staged))
	}
	info, err := os.Stat(result.Staged[0].Path)
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", result.Staged[0].Path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("staged blob permissions = %v, want 0600", perm)
	}

	if err := filepath.WalkDir(result.RunDir, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, relErr := filepath.Rel(result.RunDir, path)
		if relErr != nil {
			return relErr
		}
		if strings.HasPrefix(relative, "..") {
			return fmt.Errorf("run dir tree contains an entry outside work/<run>/: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir(%s) error = %v", result.RunDir, err)
	}
}

// TestStageManifestBlobsNeverExecutesStagedContent is the documentation-like
// path threat-matrix RED test (tasks.md 4.5): a layer whose declared name
// looks executable (README.sh) must be written as inert data only. Staging
// never inspects, extracts, or executes staged content — it only copies
// whole blobs to disk.
func TestStageManifestBlobsNeverExecutesStagedContent(t *testing.T) {
	t.Parallel()

	scriptBody := "#!/bin/sh\necho this-should-never-run > /tmp/gitleaks-stage-test-marker\n"
	layerDigest := domain.DigestFromBytes([]byte(scriptBody))
	store := &fakeBlobStore{blobs: map[domain.Digest][]byte{layerDigest: []byte(scriptBody)}}
	blobs := []domain.Descriptor{{MediaType: "application/vnd.oci.image.layer.v1.tar", Digest: layerDigest, Size: int64(len(scriptBody))}}

	workRoot := t.TempDir()
	result, cleanup, err := stageManifestBlobs(context.Background(), store, workRoot, blobs)
	if err != nil {
		t.Fatalf("stageManifestBlobs() error = %v", err)
	}
	defer cleanup()

	if len(result.Staged) != 1 {
		t.Fatalf("len(result.Staged) = %d, want 1", len(result.Staged))
	}
	body, err := os.ReadFile(result.Staged[0].Path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", result.Staged[0].Path, err)
	}
	if string(body) != scriptBody {
		t.Fatalf("staged body = %q, want the script staged verbatim as inert data (never parsed or executed)", body)
	}
	if _, err := os.Stat("/tmp/gitleaks-stage-test-marker"); !os.IsNotExist(err) {
		t.Fatalf("marker file exists, want staged content to never be executed by regixtry's own code path")
	}
}

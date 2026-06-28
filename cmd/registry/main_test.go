package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appregistry "registry/internal/app/registry"
	metadata "registry/internal/infra/metadata/sqlite"
	"registry/internal/infra/storage/fsblob"
	"registry/internal/ports"
)

func TestServeStartsAndRespondsToPing(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &bytes.Buffer{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, listener, serveConfig{
			StorageRoot:        t.TempDir(),
			DatabasePath:       filepath.Join(t.TempDir(), "registry.db"),
			AllowAnonymousPull: true,
		}, stdout)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		response, requestErr := http.Get("http://" + listener.Addr().String() + "/v2/")
		if requestErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			break
		}

		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", requestErr)
		}

		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("serve() error = %v", err)
	}

	if !strings.Contains(stdout.String(), "registry serving on") {
		t.Fatalf("stdout = %q, want start message", stdout.String())
	}
}

func TestRunTUIRendersRepositorySnapshot(t *testing.T) {
	t.Parallel()

	storageRoot := t.TempDir()
	databasePath := filepath.Join(storageRoot, "registry.db")
	seedRegistryState(t, storageRoot, databasePath)

	stdout := &bytes.Buffer{}
	err := runTUI(tuiConfig{StorageRoot: storageRoot, DatabasePath: databasePath, Tenant: "tenant-a", Snapshot: true}, strings.NewReader("q"), stdout)
	if err != nil {
		t.Fatalf("runTUI() error = %v", err)
	}

	view := stdout.String()
	if !strings.Contains(view, "Registry Console") || !strings.Contains(view, "library/alpine") {
		t.Fatalf("stdout = %q, want rendered repository view", view)
	}
}

func seedRegistryState(t *testing.T, storageRoot string, databasePath string) {
	t.Helper()

	blobStore, err := fsblob.New(filepath.Join(storageRoot, "content"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(databasePath)
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	service := appregistry.NewService(blobStore, metadataStore, localOperatorAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	upload, err := service.BeginUpload(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	digest := "sha256:0139c1c77468f75e6763a4612262743bd47a36b26cb2863d662756b3377bb029"
	if _, err := service.CompleteUpload(context.Background(), "library/alpine", upload.ID, digest, nil); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + digest + `","size":9}]}`)
	if _, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifest); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
}

package registry

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	domainauth "registry/internal/domain/auth"
	domain "registry/internal/domain/registry"
	metadata "registry/internal/infra/metadata/sqlite"
	"registry/internal/infra/storage/fsblob"
	"registry/internal/ports"
)

func TestServiceUploadPublishAndQuery(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	upload, err := service.BeginUpload(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}

	if _, err := service.AppendUpload(context.Background(), "library/alpine", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}

	blobPayload := []byte("layer-one")
	blob, err := service.CompleteUpload(context.Background(), "library/alpine", upload.ID, digestForTest(blobPayload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)

	published, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload)
	if err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}

	resolved, err := service.ResolveManifest(context.Background(), "library/alpine", "latest")
	if err != nil {
		t.Fatalf("ResolveManifest() error = %v", err)
	}

	if resolved.Digest != published.Digest {
		t.Fatalf("resolved.Digest = %q, want %q", resolved.Digest, published.Digest)
	}

	catalog, err := service.Catalog(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}

	if len(catalog.Repositories) != 1 || catalog.Repositories[0] != "library/alpine" {
		t.Fatalf("catalog.Repositories = %#v, want [library/alpine]", catalog.Repositories)
	}

	tags, err := service.Tags(context.Background(), "library/alpine", 10, "")
	if err != nil {
		t.Fatalf("Tags() error = %v", err)
	}

	if len(tags.Tags) != 1 || tags.Tags[0] != "latest" {
		t.Fatalf("tags.Tags = %#v, want [latest]", tags.Tags)
	}

	blobDetails, err := service.InspectBlob(context.Background(), "library/alpine", blob.Digest)
	if err != nil {
		t.Fatalf("InspectBlob() error = %v", err)
	}

	if blobDetails.Size != blob.Size {
		t.Fatalf("blobDetails.Size = %d, want %d", blobDetails.Size, blob.Size)
	}

	uploads, err := service.Uploads(context.Background(), "library/alpine")
	if err != nil {
		t.Fatalf("Uploads() error = %v", err)
	}

	if len(uploads) != 0 {
		t.Fatalf("len(uploads) = %d, want 0", len(uploads))
	}
}

func TestServiceRejectsManifestWithMissingBlob(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:8a5a3d2cfb08cf0c22848f3322a7fd6f1300a0a176c1f907fdbd53f5b5d2e236","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"sha256:8a5a3d2cfb08cf0c22848f3322a7fd6f1300a0a176c1f907fdbd53f5b5d2e236","size":9}]}`)

	if _, err := service.PublishManifest(context.Background(), "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err == nil {
		t.Fatal("expected missing blob conflict")
	}
}

func TestServiceAuthorizesRepositoryActionsAndFiltersCatalog(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}))
	defer cleanup()

	adminCtx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: []domainauth.Scope{{Type: "registry", Name: "catalog", Actions: []string{"*"}, Canonical: "registry:catalog:*"}, {Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}, {Type: "repository", Name: "team/other", Actions: []string{"pull", "push"}, Canonical: "repository:team/other:pull,push"}}})
	seedRepository(t, service, adminCtx, "team/app")
	seedRepository(t, service, adminCtx, "team/other")

	readerCtx := ports.ContextWithPrincipal(context.Background(), principalForGrants("team/app", domainauth.RepoRoleReader, []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull"}, Canonical: "repository:team/app:pull"}}))
	catalog, err := service.Catalog(readerCtx, 10, "")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if len(catalog.Repositories) != 1 || catalog.Repositories[0] != "team/app" {
		t.Fatalf("catalog.Repositories = %#v, want [team/app]", catalog.Repositories)
	}

	if _, err := service.Tags(readerCtx, "team/app", 10, ""); err != nil {
		t.Fatalf("Tags() error = %v", err)
	}

	if _, err := service.BeginUpload(readerCtx, "team/app"); err == nil {
		t.Fatal("expected repo-reader push to be rejected")
	} else if !domain.IsCode(err, domain.ErrorCodeUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}

	if _, err := service.Tags(readerCtx, "team/other", 10, ""); err == nil {
		t.Fatal("expected unauthorized tags access for other repository")
	}
}

func TestServiceAdminBypassesRepositoryChecks(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewPrincipalAccessController(ports.Challenge{Realm: "registry", Service: "registry"}))
	defer cleanup()

	adminCtx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{IsAdmin: true, Scopes: []domainauth.Scope{{Type: "repository", Name: "team/app", Actions: []string{"pull", "push"}, Canonical: "repository:team/app:pull,push"}, {Type: "repository", Name: "team/other", Actions: []string{"pull", "push"}, Canonical: "repository:team/other:pull,push"}, {Type: "registry", Name: "catalog", Actions: []string{"*"}, Canonical: "registry:catalog:*"}}})
	seedRepository(t, service, adminCtx, "team/app")

	if _, err := service.BeginUpload(adminCtx, "team/other"); err != nil {
		t.Fatalf("BeginUpload() admin error = %v", err)
	}
}

func newTestService(t *testing.T, accessController ports.AccessController) (*Service, func()) {
	t.Helper()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}

	service := NewService(
		blobs,
		metadataStore,
		accessController,
		ports.NewSingleTenantResolver("tenant-a"),
		ports.NewInlineJobRunner(),
	)

	return service, func() {
		_ = metadataStore.Close()
	}
}

type allowAllAccessController struct{}

func (allowAllAccessController) Authorize(context.Context, ports.Action) error {
	return nil
}

func (allowAllAccessController) Challenge(ports.Action) ports.Challenge {
	return ports.Challenge{Scheme: "Bearer", Realm: "registry", Service: "registry"}
}

func seedRepository(t *testing.T, service *Service, ctx context.Context, repository string) {
	t.Helper()

	upload, err := service.BeginUpload(ctx, repository)
	if err != nil {
		t.Fatalf("BeginUpload(%q) error = %v", repository, err)
	}

	if _, err := service.AppendUpload(ctx, repository, upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload(%q) error = %v", repository, err)
	}

	blobPayload := []byte("layer-one")
	blob, err := service.CompleteUpload(ctx, repository, upload.ID, digestForTest(blobPayload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload(%q) error = %v", repository, err)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)
	if _, err := service.PublishManifest(ctx, repository, "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest(%q) error = %v", repository, err)
	}
}

func principalForGrants(repository string, role domainauth.RepoRole, scopes []domainauth.Scope) domainauth.Principal {
	return domainauth.Principal{Grants: []domainauth.RepoGrant{{Repository: domain.MustParseRepositoryRef(repository), Role: role}}, Scopes: scopes}
}

func digestForTest(payload []byte) string {
	return domain.DigestFromBytes(payload).String()
}

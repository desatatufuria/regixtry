package ports

import (
	"context"
	"io"

	domain "registry/internal/domain/registry"
)

type BlobStore interface {
	BeginUpload(ctx context.Context, repository domain.RepositoryRef) (domain.UploadState, error)
	GetUpload(ctx context.Context, uploadID string) (domain.UploadState, error)
	PutUploadChunk(ctx context.Context, uploadID string, content io.Reader) (domain.UploadState, error)
	CommitUpload(ctx context.Context, uploadID string, expected domain.Digest) (domain.Descriptor, error)
	CancelUpload(ctx context.Context, uploadID string) error
	BlobExists(ctx context.Context, digest domain.Digest) (bool, error)
	OpenBlob(ctx context.Context, digest domain.Digest) (io.ReadSeekCloser, domain.Descriptor, error)
}

type MetadataStore interface {
	SaveUpload(ctx context.Context, tenant string, state domain.UploadState) error
	GetUpload(ctx context.Context, tenant string, uploadID string) (domain.UploadState, error)
	ListUploads(ctx context.Context, tenant string, repository *domain.RepositoryRef) ([]domain.UploadState, error)
	DeleteUpload(ctx context.Context, tenant string, uploadID string) error
	PublishManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, tag string, manifest domain.Manifest, blobs []domain.Descriptor) error
	ResolveManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, reference string) (domain.Manifest, error)
	Catalog(ctx context.Context, tenant string, limit int, after string) ([]domain.RepositoryRef, error)
	ListTags(ctx context.Context, tenant string, repository domain.RepositoryRef, limit int, after string) ([]string, error)
	ListManifestBlobs(ctx context.Context, tenant string, repository domain.RepositoryRef, manifestDigest domain.Digest) ([]domain.Descriptor, error)
}

type ActionVerb string

const (
	ActionPull    ActionVerb = "pull"
	ActionPush    ActionVerb = "push"
	ActionCatalog ActionVerb = "catalog"
	ActionInspect ActionVerb = "inspect"
)

type Action struct {
	Verb       ActionVerb
	Repository string
}

type Challenge struct {
	Scheme  string
	Realm   string
	Service string
}

type AccessController interface {
	Authorize(ctx context.Context, action Action) error
	Challenge() Challenge
}

type TenantResolver interface {
	Resolve(ctx context.Context) string
}

type JobRunner interface {
	Run(ctx context.Context, jobName string, fn func(context.Context) error) error
}

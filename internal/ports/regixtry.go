package ports

import (
	"context"
	"io"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
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
	Principal  *domainauth.Principal
}

type Challenge struct {
	Scheme  string
	Realm   string
	Service string
	Scope   string
	Error   string
}

type AccessController interface {
	Authorize(ctx context.Context, action Action) error
	Challenge(action Action) Challenge
}

type principalContextKey struct{}

func ContextWithPrincipal(ctx context.Context, principal domainauth.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) *domainauth.Principal {
	principal, ok := ctx.Value(principalContextKey{}).(domainauth.Principal)
	if !ok {
		return nil
	}

	copy := principal
	return &copy
}

func (a Action) WithPrincipal(principal *domainauth.Principal) Action {
	a.Principal = principal
	return a
}

func (a Action) Scope() string {
	switch a.Verb {
	case ActionCatalog:
		return "registry:catalog:*"
	case ActionPull, ActionInspect:
		if a.Repository == "" {
			return ""
		}
		return "repository:" + a.Repository + ":pull"
	case ActionPush:
		if a.Repository == "" {
			return ""
		}
		return "repository:" + a.Repository + ":pull,push"
	default:
		return ""
	}
}

type TenantResolver interface {
	Resolve(ctx context.Context) string
}

type JobRunner interface {
	Run(ctx context.Context, jobName string, fn func(context.Context) error) error
}

package registry

import (
	"context"
	"io"
	"time"

	domain "registry/internal/domain/registry"
	"registry/internal/ports"
)

type CatalogResult struct {
	Repositories []string `json:"repositories"`
}

type TagsResult struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type ManifestDetails struct {
	Repository  string            `json:"repository"`
	Reference   string            `json:"reference"`
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Blobs       []BlobDetails     `json:"blobs"`
}

type BlobDetails struct {
	Repository string `json:"repository,omitempty"`
	MediaType  string `json:"mediaType,omitempty"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
}

type UploadDetails struct {
	Repository string    `json:"repository"`
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Size       int64     `json:"size"`
	StartedAt  time.Time `json:"startedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Location   string    `json:"location"`
}

func (s *Service) ResolveManifest(ctx context.Context, repositoryName string, reference string) (ManifestDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return ManifestDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return ManifestDetails{}, err
	}

	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
	if err != nil {
		return ManifestDetails{}, err
	}

	blobs, err := s.metadata.ListManifestBlobs(ctx, s.tenant(ctx), repository, manifest.Digest)
	if err != nil {
		return ManifestDetails{}, err
	}

	return newManifestDetails(repository.String(), reference, manifest, blobs), nil
}

func (s *Service) OpenManifest(ctx context.Context, repositoryName string, reference string) (domain.Manifest, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return domain.Manifest{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return domain.Manifest{}, err
	}

	return s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
}

func (s *Service) Catalog(ctx context.Context, limit int, after string) (CatalogResult, error) {
	action := ports.Action{Verb: ports.ActionCatalog}
	if err := s.authorize(ctx, action); err != nil {
		return CatalogResult{}, err
	}

	repositories, err := s.metadata.Catalog(ctx, s.tenant(ctx), limit, after)
	if err != nil {
		return CatalogResult{}, err
	}

	principal := ports.PrincipalFromContext(ctx)
	result := CatalogResult{Repositories: make([]string, 0, len(repositories))}
	for _, repository := range repositories {
		if principal != nil && !principal.HasCatalogAccess(repository.String()) {
			continue
		}
		result.Repositories = append(result.Repositories, repository.String())
	}

	return result, nil
}

func (s *Service) Tags(ctx context.Context, repositoryName string, limit int, after string) (TagsResult, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return TagsResult{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect, Repository: repository.String()}); err != nil {
		return TagsResult{}, err
	}

	tags, err := s.metadata.ListTags(ctx, s.tenant(ctx), repository, limit, after)
	if err != nil {
		return TagsResult{}, err
	}

	return TagsResult{Name: repository.String(), Tags: tags}, nil
}

func (s *Service) InspectBlob(ctx context.Context, repositoryName string, digestValue string) (BlobDetails, error) {
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return BlobDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repositoryName}); err != nil {
		return BlobDetails{}, err
	}

	reader, descriptor, err := s.blobs.OpenBlob(ctx, digest)
	if err != nil {
		return BlobDetails{}, err
	}
	defer reader.Close()

	return newBlobDetails(repositoryName, descriptor), nil
}

func (s *Service) OpenBlob(ctx context.Context, repositoryName string, digestValue string) (io.ReadSeekCloser, BlobDetails, error) {
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return nil, BlobDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repositoryName}); err != nil {
		return nil, BlobDetails{}, err
	}

	reader, descriptor, err := s.blobs.OpenBlob(ctx, digest)
	if err != nil {
		return nil, BlobDetails{}, err
	}

	return reader, newBlobDetails(repositoryName, descriptor), nil
}

func (s *Service) UploadStatus(ctx context.Context, repositoryName string, uploadID string) (UploadDetails, error) {
	state, err := s.ensureUpload(ctx, repositoryName, uploadID)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: state.Repository.String()}); err != nil {
		return UploadDetails{}, err
	}

	state, err = s.blobs.GetUpload(ctx, uploadID)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.metadata.SaveUpload(ctx, s.tenant(ctx), state); err != nil {
		return UploadDetails{}, err
	}

	return newUploadDetails(state), nil
}

func (s *Service) Uploads(ctx context.Context, repositoryName string) ([]UploadDetails, error) {
	var repository *domain.RepositoryRef
	if repositoryName != "" {
		parsed, err := parseRepository(repositoryName)
		if err != nil {
			return nil, err
		}
		repository = &parsed

		if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect, Repository: parsed.String()}); err != nil {
			return nil, err
		}
	} else if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect}); err != nil {
		return nil, err
	}

	uploads, err := s.metadata.ListUploads(ctx, s.tenant(ctx), repository)
	if err != nil {
		return nil, err
	}

	result := make([]UploadDetails, 0, len(uploads))
	for _, upload := range uploads {
		result = append(result, newUploadDetails(upload))
	}

	return result, nil
}

func newManifestDetails(repository string, reference string, manifest domain.Manifest, blobs []domain.Descriptor) ManifestDetails {
	details := ManifestDetails{
		Repository:  repository,
		Reference:   reference,
		MediaType:   manifest.MediaType,
		Digest:      manifest.Digest.String(),
		Size:        manifest.Size,
		Annotations: manifest.Annotations,
		Blobs:       make([]BlobDetails, 0, len(blobs)),
	}

	for _, blob := range blobs {
		details.Blobs = append(details.Blobs, newBlobDetails(repository, blob))
	}

	return details
}

func newBlobDetails(repository string, descriptor domain.Descriptor) BlobDetails {
	return BlobDetails{
		Repository: repository,
		MediaType:  descriptor.MediaType,
		Digest:     descriptor.Digest.String(),
		Size:       descriptor.Size,
	}
}

func newUploadDetails(state domain.UploadState) UploadDetails {
	return UploadDetails{
		Repository: state.Repository.String(),
		ID:         state.ID,
		Status:     string(state.Status),
		Size:       state.Size,
		StartedAt:  state.StartedAt,
		UpdatedAt:  state.UpdatedAt,
		Location:   state.Location,
	}
}

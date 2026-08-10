package regixtry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

type Service struct {
	blobs      ports.BlobStore
	metadata   ports.MetadataStore
	access     ports.AccessController
	tenants    ports.TenantResolver
	jobs       ports.JobRunner
	scanRunner ports.ScanRunner
	runtime    FeatureRuntimeManager
	scanHost   string
	now        func() time.Time
	scanGate   *scanGate
}

type FeatureRuntimeManager interface {
	Install(ctx context.Context, version string, progress func(ports.FeatureRuntimeProgress)) (ports.TrivyRuntimeState, error)
	Upgrade(ctx context.Context, version string, progress func(ports.FeatureRuntimeProgress)) (ports.TrivyRuntimeState, error)
	Rollback(ctx context.Context) (ports.TrivyRuntimeState, error)
	Status(ctx context.Context) (ports.TrivyRuntimeState, error)
	LatestVersion(ctx context.Context) (string, error)
}

type FeatureRuntimeManagerConfig struct {
	StorageRoot string
	Store       ports.MetadataStore
	ScanRunner  ports.ScanRunner
}

func NewService(blobStore ports.BlobStore, metadataStore ports.MetadataStore, accessController ports.AccessController, tenantResolver ports.TenantResolver, jobRunner ports.JobRunner) *Service {
	if tenantResolver == nil {
		tenantResolver = ports.NewSingleTenantResolver(ports.DefaultTenant)
	}

	if accessController == nil {
		accessController = ports.NewConfigurableAccessController(ports.AccessConfig{})
	}

	if jobRunner == nil {
		jobRunner = ports.NewInlineJobRunner()
	}

	return &Service{
		blobs:    blobStore,
		metadata: metadataStore,
		access:   accessController,
		tenants:  tenantResolver,
		jobs:     jobRunner,
		now:      func() time.Time { return time.Now().UTC() },
		scanGate: newScanGate(),
	}
}

func (s *Service) SetScanRunner(runner ports.ScanRunner) {
	s.scanRunner = runner
}

func (s *Service) SetFeatureRuntimeManager(manager FeatureRuntimeManager) {
	s.runtime = manager
}

func (s *Service) SetScanHost(host string) {
	s.scanHost = strings.TrimSpace(host)
}

func (s *Service) Challenge(action ports.Action) ports.Challenge {
	return s.access.Challenge(action)
}

func (s *Service) BeginUpload(ctx context.Context, repositoryName string) (UploadDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: repository.String()}); err != nil {
		return UploadDetails{}, err
	}

	state, err := s.blobs.BeginUpload(ctx, repository)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.metadata.SaveUpload(ctx, s.tenant(ctx), state); err != nil {
		return UploadDetails{}, err
	}

	return newUploadDetails(state), nil
}

func (s *Service) AppendUpload(ctx context.Context, repositoryName string, uploadID string, content io.Reader) (UploadDetails, error) {
	state, err := s.ensureUpload(ctx, repositoryName, uploadID)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: state.Repository.String()}); err != nil {
		return UploadDetails{}, err
	}

	updated, err := s.blobs.PutUploadChunk(ctx, uploadID, content)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.metadata.SaveUpload(ctx, s.tenant(ctx), updated); err != nil {
		return UploadDetails{}, err
	}

	return newUploadDetails(updated), nil
}

func (s *Service) CompleteUpload(ctx context.Context, repositoryName string, uploadID string, expectedDigest string, finalChunk io.Reader) (BlobDetails, error) {
	state, err := s.ensureUpload(ctx, repositoryName, uploadID)
	if err != nil {
		return BlobDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: state.Repository.String()}); err != nil {
		return BlobDetails{}, err
	}

	if finalChunk != nil {
		updated, err := s.blobs.PutUploadChunk(ctx, uploadID, finalChunk)
		if err != nil {
			return BlobDetails{}, err
		}

		if err := s.metadata.SaveUpload(ctx, s.tenant(ctx), updated); err != nil {
			return BlobDetails{}, err
		}
	}

	digest, err := domain.ParseDigest(expectedDigest)
	if err != nil {
		return BlobDetails{}, err
	}

	descriptor, err := s.blobs.CommitUpload(ctx, uploadID, digest)
	if err != nil {
		return BlobDetails{}, err
	}

	if err := s.metadata.DeleteUpload(ctx, s.tenant(ctx), uploadID); err != nil {
		return BlobDetails{}, err
	}

	return newBlobDetails(state.Repository.String(), descriptor), nil
}

func (s *Service) PublishManifest(ctx context.Context, repositoryName string, reference string, mediaType string, payload []byte) (ManifestDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return ManifestDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: repository.String()}); err != nil {
		return ManifestDetails{}, err
	}

	manifest, tag, err := parseManifestPayload(reference, mediaType, payload)
	if err != nil {
		return ManifestDetails{}, err
	}

	for _, descriptor := range manifest.References() {
		exists, err := s.blobs.BlobExists(ctx, descriptor.Digest)
		if err != nil {
			return ManifestDetails{}, err
		}
		if !exists {
			return ManifestDetails{}, domain.NewConflictError(fmt.Sprintf("manifest references missing blob %s", descriptor.Digest))
		}
	}

	if err := s.metadata.PublishManifest(ctx, s.tenant(ctx), repository, tag, manifest, manifest.References()); err != nil {
		return ManifestDetails{}, err
	}

	return newManifestDetails(repository.String(), reference, manifest, manifest.References()), nil
}

func parseManifestPayload(reference string, mediaType string, payload []byte) (domain.Manifest, string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return domain.Manifest{}, "", domain.NewValidationError("manifest reference is required")
	}

	var envelope manifestEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return domain.Manifest{}, "", domain.NewInvalidManifestError("manifest payload must be valid JSON")
	}

	if mediaType == "" {
		mediaType = strings.TrimSpace(envelope.MediaType)
	}

	config, err := envelope.Config.toDescriptor()
	if err != nil {
		return domain.Manifest{}, "", err
	}

	layers := make([]domain.Descriptor, 0, len(envelope.Layers))
	for _, layer := range envelope.Layers {
		descriptor, err := layer.toDescriptor()
		if err != nil {
			return domain.Manifest{}, "", err
		}
		layers = append(layers, *descriptor)
	}

	subject, err := envelope.Subject.toDescriptor()
	if err != nil {
		return domain.Manifest{}, "", err
	}

	manifest, err := domain.NewManifest(mediaType, payload, config, layers, subject, envelope.Annotations)
	if err != nil {
		return domain.Manifest{}, "", err
	}

	if digest, err := domain.ParseDigest(reference); err == nil {
		if digest != manifest.Digest {
			return domain.Manifest{}, "", domain.NewDigestMismatchError(digest, manifest.Digest)
		}
		return manifest, "", nil
	}

	return manifest, reference, nil
}

func parseRepository(repositoryName string) (domain.RepositoryRef, error) {
	return domain.ParseRepositoryRef(strings.TrimSpace(repositoryName))
}

func (s *Service) authorize(ctx context.Context, action ports.Action) error {
	action.Principal = ports.PrincipalFromContext(ctx)
	return s.access.Authorize(ctx, action)
}

func (s *Service) tenant(ctx context.Context) string {
	return s.tenants.Resolve(ctx)
}

func (s *Service) ensureUpload(ctx context.Context, repositoryName string, uploadID string) (domain.UploadState, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return domain.UploadState{}, err
	}

	state, err := s.metadata.GetUpload(ctx, s.tenant(ctx), uploadID)
	if err != nil {
		return domain.UploadState{}, err
	}

	if state.Repository.String() != repository.String() {
		return domain.UploadState{}, domain.NewNotFoundError("upload", uploadID)
	}

	return state, nil
}

type manifestEnvelope struct {
	MediaType   string             `json:"mediaType"`
	Config      *manifestResource  `json:"config"`
	Layers      []manifestResource `json:"layers"`
	Subject     *manifestResource  `json:"subject"`
	Annotations map[string]string  `json:"annotations"`
}

type manifestResource struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type scanGate struct {
	mu       sync.Mutex
	inFlight int
}

func newScanGate() *scanGate { return &scanGate{} }

func (g *scanGate) acquire(ctx context.Context, max int) error {
	if max <= 0 {
		max = 1
	}
	for {
		g.mu.Lock()
		if g.inFlight < max {
			g.inFlight++
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (g *scanGate) release() {
	g.mu.Lock()
	if g.inFlight > 0 {
		g.inFlight--
	}
	g.mu.Unlock()
}

func (r *manifestResource) toDescriptor() (*domain.Descriptor, error) {
	if r == nil {
		return nil, nil
	}

	digest, err := domain.ParseDigest(r.Digest)
	if err != nil {
		return nil, err
	}

	descriptor := &domain.Descriptor{
		MediaType: r.MediaType,
		Digest:    digest,
		Size:      r.Size,
	}

	if err := descriptor.Validate(); err != nil {
		return nil, err
	}

	return descriptor, nil
}

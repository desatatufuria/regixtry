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
	blobs            ports.BlobStore
	metadata         ports.MetadataStore
	access           ports.AccessController
	tenants          ports.TenantResolver
	jobs             ports.JobRunner
	runnerMu         sync.RWMutex
	scanRunner       ports.ScanRunner
	secretScanRunner ports.SecretScanRunner
	runtimes         map[string]FeatureRuntimeManager
	scanHost         string
	// deleteEnabled gates DeleteManifest (design.md Decision 2). It defaults
	// to false (zero value); Phase 4 threads the actual
	// REGISTRY_DELETE_ENABLED flag value in via SetDeleteEnabled.
	deleteEnabled  bool
	now            func() time.Time
	scanGate       *scanGate
	secretScanGate *scanGate
	// scanQueueMu guards the check-then-insert dedup step shared by
	// QueueManualScan, queueScheduledScan, and queuePushScan
	// (dedupAndQueueScanRun) so a push-triggered scan's own goroutine can
	// never race a synchronous manual/scheduled scan into inserting two
	// runs for the same digest.
	scanQueueMu sync.Mutex
	// backgroundWork tracks push-triggered scan goroutines (queuePushScan)
	// so tests can drain them before closing the metadata store; unlike
	// QueueManualScan/RunScheduledScans, which only spawn goroutines from
	// dedicated scan tests that already wait for completion, queuePushScan
	// fires on every successful PublishManifest across the whole suite, so
	// an undrained goroutine racing a t.TempDir() cleanup is a real hazard,
	// not merely a hypothetical one.
	backgroundWork sync.WaitGroup
}

type FeatureRuntimeManager interface {
	Install(ctx context.Context, version string, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error)
	Upgrade(ctx context.Context, version string, progress func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error)
	Rollback(ctx context.Context) (ports.FeatureRuntimeState, error)
	Status(ctx context.Context) (ports.FeatureRuntimeState, error)
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
		blobs:          blobStore,
		metadata:       metadataStore,
		access:         accessController,
		tenants:        tenantResolver,
		jobs:           jobRunner,
		runtimes:       make(map[string]FeatureRuntimeManager),
		now:            func() time.Time { return time.Now().UTC() },
		scanGate:       newScanGate(),
		secretScanGate: newScanGate(),
	}
}

// WaitForBackgroundWork blocks until every in-flight push-triggered scan
// goroutine (queuePushScan, spawned from PublishManifest) has returned. It
// exists for graceful shutdown and for tests that close the metadata store
// right after exercising the service — without it, a fire-and-forget
// goroutine can still be querying the store when the caller closes or
// removes it.
func (s *Service) WaitForBackgroundWork() {
	s.backgroundWork.Wait()
}

// SetScanRunner/SetSecretScanRunner and their getScanRunner/getSecretScanRunner
// counterparts share a mutex (runnerMu) because queuePushScan's fire-and-
// forget goroutine (executeScanRun/executeSecretScanLeg) can now read these
// fields concurrently with a Set* call from arbitrary test or reconfigure
// timing — a plain field would be a data race under -race.
func (s *Service) SetScanRunner(runner ports.ScanRunner) {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	s.scanRunner = runner
}

func (s *Service) SetSecretScanRunner(runner ports.SecretScanRunner) {
	s.runnerMu.Lock()
	defer s.runnerMu.Unlock()
	s.secretScanRunner = runner
}

func (s *Service) getScanRunner() ports.ScanRunner {
	s.runnerMu.RLock()
	defer s.runnerMu.RUnlock()
	return s.scanRunner
}

func (s *Service) getSecretScanRunner() ports.SecretScanRunner {
	s.runnerMu.RLock()
	defer s.runnerMu.RUnlock()
	return s.secretScanRunner
}

func (s *Service) SetFeatureRuntimeManager(feature string, manager FeatureRuntimeManager) {
	if s.runtimes == nil {
		s.runtimes = make(map[string]FeatureRuntimeManager)
	}
	s.runtimes[strings.TrimSpace(feature)] = manager
}

func (s *Service) SetScanHost(host string) {
	s.scanHost = strings.TrimSpace(host)
}

// SetDeleteEnabled mirrors SetScanHost's low-churn setter shape (design.md
// Decision 2): NewService has 7 call sites in main.go, so a setter avoids an
// eighth positional constructor argument.
func (s *Service) SetDeleteEnabled(enabled bool) {
	s.deleteEnabled = enabled
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

	// PushedBy records the pushing principal's UserID (stable across
	// logins/re-issues, unlike Subject) for the opt-in unsigned-self-read
	// exemption (enforceSigningPolicy). principal should never be nil here --
	// the authorize() call above already required one -- but this stays
	// defensive rather than panicking on an unexpected nil.
	if principal := ports.PrincipalFromContext(ctx); principal != nil {
		manifest.PushedBy = principal.UserID
	}

	for _, descriptor := range manifest.BlobReferences() {
		exists, err := s.blobs.BlobExists(ctx, descriptor.Digest)
		if err != nil {
			return ManifestDetails{}, err
		}
		if !exists {
			return ManifestDetails{}, domain.NewConflictError(fmt.Sprintf("manifest references missing blob %s", descriptor.Digest))
		}
	}

	// Subject (OCI 1.1) points at another manifest by digest -- e.g. what
	// `cosign sign` pushes when signing an image -- so it is validated
	// against manifest storage, never blob storage, and kept entirely out of
	// BlobReferences (design note: fix/manifest-subject-not-blob).
	if manifest.Subject != nil {
		if _, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, manifest.Subject.Digest.String()); err != nil {
			if domain.IsCode(err, domain.ErrorCodeNotFound) {
				return ManifestDetails{}, domain.NewConflictError(fmt.Sprintf("manifest subject %s was not found in %s", manifest.Subject.Digest, repository))
			}
			return ManifestDetails{}, err
		}
	}

	if err := s.metadata.PublishManifest(ctx, s.tenant(ctx), repository, tag, manifest, manifest.BlobReferences()); err != nil {
		return ManifestDetails{}, err
	}

	// Fire-and-forget: push auto-queues a scan but must never fail or wait
	// on it (design.md Decision 4). Tenant is captured here, before the
	// goroutine, because the request context is cancelled the moment this
	// response is written.
	s.backgroundWork.Add(1)
	go func() {
		defer s.backgroundWork.Done()
		s.queuePushScan(context.Background(), s.tenant(ctx), repository.String(), reference, manifest.Digest.String())
	}()

	return newManifestDetails(repository.String(), reference, manifest, manifest.BlobReferences()), nil
}

// DeleteManifest removes a manifest by digest (cascading to every tag that
// points at it) or a single tag by name, depending on which domain.ParseDigest
// accepts for reference -- the same digest-vs-tag idiom parseManifestPayload
// already applies to the same argument on the publish path (design.md
// Decision 3).
//
// Ordering is authorization before the opt-in deleteEnabled flag (design.md
// Decision 2): an unauthorized caller is refused with
// domain.NewUnauthorizedError even when deletion is disabled, so the flag's
// on/off state is never disclosed to a caller who was never entitled to
// delete in the first place. Only a caller who would otherwise have
// succeeded learns the capability exists but is off.
func (s *Service) DeleteManifest(ctx context.Context, repositoryName string, reference string) (DeletionDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return DeletionDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionDelete, Repository: repository.String()}); err != nil {
		return DeletionDetails{}, err
	}

	if !s.deleteEnabled {
		return DeletionDetails{}, domain.NewValidationError("manifest deletion is not enabled")
	}

	if digest, err := domain.ParseDigest(reference); err == nil {
		tagsRemoved, err := s.metadata.DeleteManifestByDigest(ctx, s.tenant(ctx), repository, digest)
		if err != nil {
			return DeletionDetails{}, err
		}
		return newDeletionDetailsForDigest(repository.String(), reference, digest.String(), tagsRemoved), nil
	}

	if err := s.metadata.DeleteTag(ctx, s.tenant(ctx), repository, reference); err != nil {
		return DeletionDetails{}, err
	}

	return newDeletionDetailsForTag(repository.String(), reference), nil
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

package regixtry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
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
	// usernames resolves a manifest's pushed_by UserID to a display username
	// for TagDetails' PushedBy column (console-tags-pushed-by change).
	// Optional: nil in embedded/no-auth mode, where PushedBy always
	// resolves to "". Set once at startup (cmd/regixtry/main.go), never
	// concurrently with a request the way scanRunner/secretScanRunner can
	// be, so unlike those it needs no mutex.
	usernames UsernameResolver
	scanHost  string
	// deleteEnabled gates DeleteManifest (design.md Decision 2). It defaults
	// to false (zero value); Phase 4 threads the actual
	// REGISTRY_DELETE_ENABLED flag value in via SetDeleteEnabled.
	deleteEnabled bool
	// gcDeleteEnabled gates DeleteByGCReport (design.md Decision F / D1a): a
	// DIFFERENT flag from deleteEnabled -- REGISTRY_DELETE_ENABLED is
	// metadata-only and never touches blob files. Defaults to false; wired
	// via SetGCDeleteEnabled from REGISTRY_GC_DELETE_ENABLED.
	gcDeleteEnabled bool
	now             func() time.Time
	scanGate        *scanGate
	secretScanGate  *scanGate
	// pushScans admits push-triggered scans (see service_push_scan.go).
	pushScans *pushScanLimiter
	// skippedPushScans records dropped push scans off the request path.
	skippedPushScans skippedRecorder
	// scanRunMargin overrides defaultScanRunMargin (tests only; set before use).
	scanRunMargin time.Duration
	// stuckSecretLegs counts secret-scan goroutines abandoned after their
	// bound that have not returned yet; push scans are refused (recorded as
	// skipped) while it is at maxStuckSecretLegs.
	stuckSecretLegs atomic.Int32
	// gcGate bounds ComputeGCReport/DeleteByGCReport to a single in-flight
	// run at a time (proposal.md D2: "single in-flight run"), the same
	// scanGate primitive scan queueing already uses.
	gcGate *scanGate
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
	// beginUploadMu makes the open-upload cap check and the insert of the new
	// upload row one step, so concurrent BeginUpload calls cannot overshoot.
	beginUploadMu sync.Mutex
	// maxOpenUploads / maxOpenUploadsPerRepo cap concurrently open uploads
	// (zero or negative disables a cap); uploadTTL is how long an upload may
	// sit idle before the reaper deletes it (zero or negative disables the
	// reaper). See SetUploadLimits / SetUploadTTL.
	maxOpenUploads        int
	maxOpenUploadsPerRepo int
	uploadTTL             time.Duration
	// skipSweepDBRefresh turns the once-per-sweep vulnerability DB download
	// off (air-gapped installs); the zero value keeps the refresh on. See
	// SetScanDBRefreshOnSweep.
	skipSweepDBRefresh atomic.Bool
	// requireProject makes pushes refuse a missing project (see
	// SetRequireProject in service_options.go). Zero value: off.
	requireProject bool
}

// Default upload limits applied by NewService.
const (
	DefaultMaxOpenUploads        = 1000
	DefaultMaxOpenUploadsPerRepo = 200
	DefaultUploadTTL             = 24 * time.Hour
)

// UsernameResolver resolves a principal's opaque UserID (auth.User.ID) to
// their human-readable username, backing TagDetails' PushedBy column
// (console-tags-pushed-by change). Contract: ("", nil) for "no such user"
// (a deleted user, or any UserID the resolver simply doesn't recognize) --
// never an error for that case, since a missing user must never fail the
// whole TagDetails call. A non-nil error means a genuine infrastructure
// failure (e.g. the auth store is unreachable), which TagDetails propagates,
// mirroring its existing SignatureStatus hard-fail policy.
type UsernameResolver interface {
	ResolveUsername(ctx context.Context, userID string) (string, error)
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
		pushScans:      newPushScanLimiter(),
		gcGate:         newScanGate(),

		maxOpenUploads:        DefaultMaxOpenUploads,
		maxOpenUploadsPerRepo: DefaultMaxOpenUploadsPerRepo,
		uploadTTL:             DefaultUploadTTL,
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

func (s *Service) SetUsernameResolver(resolver UsernameResolver) {
	s.usernames = resolver
}

// SetScanDBRefreshOnSweep controls whether each scheduled sweep first
// downloads the latest Trivy vulnerability DB (default on). Turn it off for
// air-gapped installs that cannot reach the Trivy DB mirror: the sweep then
// scans against the DB already in the cache.
func (s *Service) SetScanDBRefreshOnSweep(enabled bool) {
	s.skipSweepDBRefresh.Store(!enabled)
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

// SetGCDeleteEnabled mirrors SetDeleteEnabled exactly: a low-churn setter
// rather than a constructor argument. It gates ONLY DeleteByGCReport and is
// a DIFFERENT flag from deleteEnabled (design.md Decision F).
func (s *Service) SetGCDeleteEnabled(enabled bool) {
	s.gcDeleteEnabled = enabled
}

// SetUploadLimits caps concurrently open uploads, globally and per repository
// (zero or negative disables that cap). Call it before serving traffic.
func (s *Service) SetUploadLimits(maxOpen int, maxOpenPerRepository int) {
	s.maxOpenUploads = maxOpen
	s.maxOpenUploadsPerRepo = maxOpenPerRepository
}

// SetUploadTTL sets how long an upload may sit idle before the reaper deletes
// it. Zero or negative disables the reaper.
func (s *Service) SetUploadTTL(ttl time.Duration) {
	s.uploadTTL = ttl
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

	if err := s.requireProjectForPush(ctx, repository); err != nil {
		return UploadDetails{}, err
	}

	s.beginUploadMu.Lock()
	defer s.beginUploadMu.Unlock()

	if err := s.checkOpenUploadCaps(ctx, repository); err != nil {
		return UploadDetails{}, err
	}

	state, err := s.blobs.BeginUpload(ctx, repository)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.metadata.SaveUpload(ctx, s.tenant(ctx), state); err != nil {
		_ = s.blobs.CancelUpload(ctx, state.ID)
		return UploadDetails{}, err
	}

	return newUploadDetails(state), nil
}

// checkOpenUploadCaps refuses a new upload once the global or per-repository
// open-upload cap is reached. Callers hold beginUploadMu.
func (s *Service) checkOpenUploadCaps(ctx context.Context, repository domain.RepositoryRef) error {
	if s.maxOpenUploads <= 0 && s.maxOpenUploadsPerRepo <= 0 {
		return nil
	}

	open, err := s.metadata.ListUploads(ctx, s.tenant(ctx), nil)
	if err != nil {
		return err
	}

	perRepository := 0
	for _, upload := range open {
		if upload.Repository.String() == repository.String() {
			perRepository++
		}
	}

	if s.maxOpenUploads > 0 && len(open) >= s.maxOpenUploads {
		return domain.NewTooManyRequestsError(fmt.Sprintf("too many open uploads (limit %d); finish or cancel some and retry", s.maxOpenUploads))
	}
	if s.maxOpenUploadsPerRepo > 0 && perRepository >= s.maxOpenUploadsPerRepo {
		return domain.NewTooManyRequestsError(fmt.Sprintf("too many open uploads for repository %q (limit %d); finish or cancel some and retry", repository.String(), s.maxOpenUploadsPerRepo))
	}

	return nil
}

// CancelUpload deletes an open upload (DELETE /v2/<name>/blobs/uploads/<id>):
// the session row and the temp data. Like AppendUpload it needs push
// permission on the upload's own repository, and an upload that belongs to a
// different repository is reported as not found.
func (s *Service) CancelUpload(ctx context.Context, repositoryName string, uploadID string) error {
	state, err := s.ensureUpload(ctx, repositoryName, uploadID)
	if err != nil {
		return err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: state.Repository.String()}); err != nil {
		return err
	}

	if err := s.blobs.CancelUpload(ctx, uploadID); err != nil {
		return err
	}

	if err := s.metadata.DeleteUpload(ctx, s.tenant(ctx), uploadID); err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return err
	}

	return nil
}

// UploadReaper is the optional BlobStore capability the reaper uses to remove
// an abandoned upload safely: it must skip an upload with a write or commit in
// flight and one modified after olderThan, and report whether it removed it.
type UploadReaper interface {
	ReapUpload(ctx context.Context, uploadID string, olderThan time.Time) (bool, error)
}

// OrphanUploadReaper is the optional BlobStore capability that removes upload
// directories with no metadata row (see fsblob.Store.ReapOrphanUploads).
type OrphanUploadReaper interface {
	ReapOrphanUploads(ctx context.Context, olderThan time.Time, limit int, hasRow func(ctx context.Context, uploadID string) (bool, error)) (int, error)
}

// uploadRowChecker is the optional MetadataStore capability to look an upload
// row up across tenants.
type uploadRowChecker interface {
	UploadExists(ctx context.Context, uploadID string) (bool, error)
}

// maxOrphanReapsPerPass bounds how many orphaned upload directories one reaper
// pass removes.
const maxOrphanReapsPerPass = 100

func (s *Service) uploadHasRow(ctx context.Context, uploadID string) (bool, error) {
	if checker, ok := s.metadata.(uploadRowChecker); ok {
		return checker.UploadExists(ctx, uploadID)
	}
	if _, err := s.metadata.GetUpload(ctx, s.tenant(ctx), uploadID); err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ReapAbandonedUploads deletes uploads idle for longer than the configured
// TTL, both the session row and the temp file, and returns how many it
// removed. It never touches committed blobs, and it skips an upload that has a
// write or commit in flight. It also removes orphaned upload directories that
// have no row. Disabled when the TTL is zero or negative.
func (s *Service) ReapAbandonedUploads(ctx context.Context) (int, error) {
	if s.uploadTTL <= 0 {
		return 0, nil
	}

	tenant := s.tenant(ctx)
	cutoff := s.now().Add(-s.uploadTTL)
	uploads, err := s.metadata.ListUploads(ctx, tenant, nil)
	if err != nil {
		return 0, err
	}

	reaper, _ := s.blobs.(UploadReaper)
	reaped := 0
	for _, upload := range uploads {
		if !upload.UpdatedAt.Before(cutoff) {
			continue
		}

		if reaper != nil {
			removed, err := reaper.ReapUpload(ctx, upload.ID, cutoff)
			if err != nil {
				return reaped, err
			}
			if !removed {
				continue
			}
		} else if err := s.blobs.CancelUpload(ctx, upload.ID); err != nil {
			return reaped, err
		}

		if err := s.metadata.DeleteUpload(ctx, tenant, upload.ID); err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) {
			return reaped, err
		}
		reaped++
	}

	if orphans, ok := s.blobs.(OrphanUploadReaper); ok {
		removed, err := orphans.ReapOrphanUploads(ctx, cutoff, maxOrphanReapsPerPass, s.uploadHasRow)
		reaped += removed
		if err != nil {
			return reaped, err
		}
	}

	return reaped, nil
}

// RunUploadReaper reaps abandoned uploads every interval until ctx is
// cancelled. A failed pass is logged through onError (may be nil) and retried
// on the next tick.
func (s *Service) RunUploadReaper(ctx context.Context, interval time.Duration, onError func(error)) {
	if s.uploadTTL <= 0 {
		return
	}
	if interval <= 0 {
		interval = time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.ReapAbandonedUploads(ctx); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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

	// The blob file is shared by content, so the repository link is what
	// makes it readable and referenceable in THIS repository.
	if err := s.metadata.LinkBlob(ctx, s.tenant(ctx), state.Repository, descriptor.Digest); err != nil {
		return BlobDetails{}, err
	}

	// A cancel racing the commit may already have removed the row.
	if err := s.metadata.DeleteUpload(ctx, s.tenant(ctx), uploadID); err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) {
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

	// Blob existence and repository links are verified by the store inside
	// the same write transaction that records the manifest, so blob GC
	// (ReclaimBlob, same write queue) cannot remove a blob between the check
	// and the publish.
	if err := s.metadata.PublishManifestVerified(ctx, s.tenant(ctx), repository, tag, manifest, manifest.BlobReferences(), s.blobs.BlobExists); err != nil {
		return ManifestDetails{}, err
	}

	// Fire-and-forget: push auto-queues a scan but must never fail or wait
	// on it (design.md Decision 4). Tenant is captured here, before the
	// goroutine, because the request context is cancelled the moment this
	// response is written.
	//
	// isReferrerArtifactPush means this manifest is a cosign
	// signature/attestation/SBOM pointing at another manifest, never a real
	// image with layers. Queueing a scan for it is not merely wasted work:
	// Trivy cannot scan it as an image at all, so the scan always fails,
	// permanently polluting Repository Alerts with an unfixable "failed"
	// row for every signature/attestation ever pushed (found live: every
	// cosign-signed push run in team/az-deploy-demo -- both the
	// subject-bearing bundle-referrer manifest AND the subject-less
	// bundle-index manifest above it). The manifest this one refers to is
	// unaffected and keeps getting scanned normally.
	if !isReferrerArtifactPush(manifest, tag) {
		pusher := ""
		if principal := ports.PrincipalFromContext(ctx); principal != nil {
			pusher = principal.UserID
		}
		s.startPushScan(s.tenant(ctx), repository.String(), reference, manifest.Digest.String(), pusher)
	}

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

// DeleteRepository removes an entire repository -- every manifest, tag, and
// manifest_blobs row it owns, plus the repositories row itself -- in one
// store-layer transaction (mirrors DeleteManifest's own shape/gating,
// scaled to the whole repository). It never touches blob files on disk:
// blobs are globally content-addressed and shared across repositories with
// no per-repository namespacing (ListReferencedBlobDigests' own doc
// comment), so only the existing GC mechanism may ever unlink them.
func (s *Service) DeleteRepository(ctx context.Context, repositoryName string) (RepositoryDeletionDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return RepositoryDeletionDetails{}, err
	}

	if err := s.authorizeDeleteRepository(ctx, repository.String()); err != nil {
		return RepositoryDeletionDetails{}, err
	}

	if !s.deleteEnabled {
		return RepositoryDeletionDetails{}, domain.NewValidationError("repository deletion is not enabled")
	}

	tagsRemoved, manifestsRemoved, err := s.metadata.DeleteRepository(ctx, s.tenant(ctx), repository)
	if err != nil {
		return RepositoryDeletionDetails{}, err
	}

	return RepositoryDeletionDetails{Repository: repository.String(), ManifestsRemoved: manifestsRemoved, TagsRemoved: tagsRemoved}, nil
}

// authorizeDeleteRepository gates DeleteRepository. It is deliberately
// stricter than DeleteManifest's own bare s.authorize(ActionDelete) call,
// because destroying an entire repository in one action is categorically
// more destructive than removing one manifest/tag.
//
// It starts with the exact same ports.ActionDelete authorize call
// DeleteManifest itself uses, so a real OCI-scoped bearer token and the
// local trusted-operator TUI/CLI mode's nil principal (under
// localOperatorAccessController) are evaluated identically to every other
// delete. But whenever a Principal IS present in ctx, it additionally
// requires the caller to be a global admin or hold a repo-admin grant on
// this repository, directly or through its project (the shared
// Principal.HasGrantedRepoAdminAccess predicate; internal/app/auth's
// requireAdminOrRepoAdmin is the remaining exact-only copy). It does not use
// Principal.HasRepoAdminAccess, for the same reason requireAdminOrRepoAdmin
// exists instead of that method: an admin-session login -- the TUI's own
// admin panel, or this action's own admin HTTP API route -- always
// carries zero token scopes, which would make HasDeleteAccess/
// HasRepoAdminAccess permanently false even for a genuine global admin.
//
// This closes a gap the base check alone would leave open: a real bearer
// token scoped repository:x:pull,push,delete (an ordinary repo-writer, not
// a repo-admin) already satisfies s.authorize's HasDeleteAccess check --
// correctly, for deleting one manifest/tag -- but must never also be
// sufficient to reach this whole-repository action. The four cases:
//   - nil principal: the base authorize's own answer stands (nil under
//     local trust, else Unauthorized) -- never additionally restricted.
//   - present principal, admin or repo-admin: always allowed, regardless of
//     whether the base check passed (covers the zero-scope admin-session
//     case).
//   - present principal, neither, base check failed: the base check's
//     Unauthorized error stands.
//   - present principal, neither, base check PASSED (the write+delete-scope
//     gap): rejected with Forbidden here.
func (s *Service) authorizeDeleteRepository(ctx context.Context, repository string) error {
	authorizeErr := s.authorize(ctx, ports.Action{Verb: ports.ActionDelete, Repository: repository})

	principal := ports.PrincipalFromContext(ctx)
	if principal == nil {
		return authorizeErr
	}

	if principal.HasGrantedRepoAdminAccess(repository) {
		return nil
	}

	if authorizeErr != nil {
		return authorizeErr
	}
	return domainauth.NewForbiddenError("repository administrator privileges are required")
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

	manifest, err := domain.NewManifest(mediaType, strings.TrimSpace(envelope.ArtifactType), payload, config, layers, subject, envelope.Annotations)
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

// isReferrerArtifactPush reports whether this push is a cosign referrer
// artifact -- a signature, attestation, or SBOM pointing at another
// manifest -- never a primary artifact a push-triggered scan should ever
// attempt against. Two signals, either sufficient on its own, mirror the
// two real shapes cosign v3 pushes for one signing operation:
//
//  1. manifest.Subject != nil: the OCI 1.1-native referrer manifest (the
//     bundle-referrer manifest itself, and legacy `.sig` manifests).
//  2. tag matches cosign's own referrer-tag-schema convention
//     (SignatureTag's "sha256-<hex>.sig", or BundleIndexTag's suffix-less
//     "sha256-<hex>"): the top-level Sigstore Bundle artifact is an OCI
//     Image Index at exactly this tag, carrying NO subject of its own --
//     only a "manifests" array pointing at the referrer manifest above by
//     digest, so Subject alone misses it entirely.
func isReferrerArtifactPush(manifest domain.Manifest, tag string) bool {
	if manifest.Subject != nil {
		return true
	}
	const prefix = "sha256-"
	if !strings.HasPrefix(tag, prefix) {
		return false
	}
	digest := "sha256:" + strings.TrimSuffix(strings.TrimPrefix(tag, prefix), ".sig")
	if sigTag, err := signing.SignatureTag(digest); err == nil && sigTag == tag {
		return true
	}
	if bundleTag, err := signing.BundleIndexTag(digest); err == nil && bundleTag == tag {
		return true
	}
	return false
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
	MediaType    string             `json:"mediaType"`
	ArtifactType string             `json:"artifactType"`
	Config       *manifestResource  `json:"config"`
	Layers       []manifestResource `json:"layers"`
	Subject      *manifestResource  `json:"subject"`
	Annotations  map[string]string  `json:"annotations"`
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

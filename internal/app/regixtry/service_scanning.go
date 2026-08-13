package regixtry

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func (s *Service) EnsureScanSettings(ctx context.Context, defaults ports.ScanSettings) (ports.ScanSettings, error) {
	settings, err := s.metadata.GetScanSettings(ctx, s.tenant(ctx), trivyFeatureName)
	if err == nil {
		return settings, nil
	}
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.ScanSettings{}, err
	}
	normalized, err := s.normalizeScanSettings(defaults)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	if err := s.metadata.UpsertScanSettings(ctx, s.tenant(ctx), trivyFeatureName, normalized); err != nil {
		return ports.ScanSettings{}, err
	}
	return normalized, nil
}

func (s *Service) GetScanSettings(ctx context.Context) (ports.ScanSettings, error) {
	return s.metadata.GetScanSettings(ctx, s.tenant(ctx), trivyFeatureName)
}

func (s *Service) UpdateScanSettings(ctx context.Context, input ports.ScanSettings) (ports.ScanSettings, error) {
	normalized, err := s.normalizeScanSettings(input)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	if err := s.metadata.UpsertScanSettings(ctx, s.tenant(ctx), trivyFeatureName, normalized); err != nil {
		return ports.ScanSettings{}, err
	}
	return normalized, nil
}

// GetScanPolicySettings resolves the global vulnerability policy gate
// settings with a code-level default fallback (design.md Decision 1): a
// fresh install with zero rows ever written reports enabled/CRITICAL,
// deliberately not relying on a boot-time Ensure* seed the way
// GetScanSettings/EnsureScanSettings does — a missed boot path must never
// silently produce policy-OFF.
func (s *Service) GetScanPolicySettings(ctx context.Context) (ports.ScanPolicySettings, error) {
	settings, err := s.metadata.GetScanPolicySettings(ctx, s.tenant(ctx))
	if domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}, nil
	}
	return settings, err
}

func (s *Service) UpdateScanPolicySettings(ctx context.Context, input ports.ScanPolicySettings) (ports.ScanPolicySettings, error) {
	input.UpdatedAt = s.now()
	if err := s.metadata.UpsertScanPolicySettings(ctx, s.tenant(ctx), input); err != nil {
		return ports.ScanPolicySettings{}, err
	}
	return input, nil
}

// scanPolicyViolated is the pure vulnerability policy gate evaluator
// (design.md Decision 2). It is fail-open on scan-state uncertainty: only a
// completed run whose findings meet or exceed the configured severity
// threshold blocks. An unknown/empty threshold behaves as CRITICAL — the
// permissive default branch — but the admin API rejects unknown threshold
// values before they can ever reach this function (Decision 5).
func scanPolicyViolated(settings ports.ScanPolicySettings, run ports.ScanRun) bool {
	if !settings.Enabled || run.Status != ports.ScanRunStatusCompleted {
		return false
	}
	if settings.SeverityThreshold == ports.ScanPolicyThresholdCriticalHigh {
		return run.Critical > 0 || run.High > 0
	}
	return run.Critical > 0
}

// enforceScanPolicy is the pull-time vulnerability policy gate
// (design.md Decision 3), called from OpenManifest after the digest is
// resolved. A digest with no scan run of any status allows the pull
// (NotFound is fail-open, not an error); any other store error propagates
// unchanged so an infrastructure failure is never silently swallowed into
// "allow".
func (s *Service) enforceScanPolicy(ctx context.Context, repository string, digest string) error {
	settings, err := s.GetScanPolicySettings(ctx)
	if err != nil {
		return err
	}
	run, err := s.metadata.GetLatestScanRunByDigest(ctx, s.tenant(ctx), repository, digest)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return nil
		}
		return err
	}
	if scanPolicyViolated(settings, run) {
		return domain.NewPolicyViolationError(fmt.Sprintf("pull of %s@%s is blocked by the vulnerability policy", repository, digest))
	}
	return nil
}

func (s *Service) QueueManualScan(ctx context.Context, repositoryName string, reference string) (ports.ScanRun, error) {
	settings, err := s.resolveManagedScanSettings(ctx)
	if err != nil {
		return ports.ScanRun{}, err
	}
	settings, err = s.applyRepositoryOverride(ctx, s.tenant(ctx), repositoryName, trivyFeatureName, settings)
	if err != nil {
		return ports.ScanRun{}, err
	}
	if !settings.Enabled {
		return ports.ScanRun{}, domain.NewValidationError("scan settings must be enabled")
	}
	if _, err := s.scanTarget(settings, repositoryName, "sha256:placeholder"); err != nil {
		return ports.ScanRun{}, err
	}
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return ports.ScanRun{}, err
	}
	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, strings.TrimSpace(reference))
	if err != nil {
		return ports.ScanRun{}, err
	}
	digest := manifest.Digest.String()
	run, existed, err := s.dedupAndQueueScanRun(ctx, s.tenant(ctx), repository.String(), strings.TrimSpace(reference), digest, ports.ScanTriggerManual)
	if err != nil {
		return ports.ScanRun{}, err
	}
	if !existed {
		go s.executeScanRun(context.Background(), s.tenant(ctx), run, settings)
	}
	return run, nil
}

// dedupAndQueueScanRun is the shared check-then-insert step behind
// QueueManualScan, queueScheduledScan, and queuePushScan: it returns the
// already-active run for a digest if one exists (queued|running), otherwise
// it inserts a new queued run with the given trigger. scanQueueMu makes the
// check-then-insert atomic across all three callers — without it, a
// push-triggered scan (queuePushScan's own goroutine) and a synchronous
// manual/scheduled scan for the same digest could race past the dedup check
// and both insert a run, defeating "never queue a second scan for an
// in-flight digest".
func (s *Service) dedupAndQueueScanRun(ctx context.Context, tenant string, repository string, reference string, digest string, trigger string) (ports.ScanRun, bool, error) {
	s.scanQueueMu.Lock()
	defer s.scanQueueMu.Unlock()

	if active, err := s.metadata.GetActiveScanRunByDigest(ctx, tenant, repository, digest); err == nil {
		return active, true, nil
	} else if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.ScanRun{}, false, err
	}
	now := s.now()
	run := ports.ScanRun{ID: uuid.NewString(), Repository: repository, RequestedRef: reference, Digest: digest, Status: ports.ScanRunStatusQueued, Trigger: trigger, CreatedAt: now, UpdatedAt: now}
	if err := s.metadata.UpsertScanRun(ctx, tenant, run); err != nil {
		return ports.ScanRun{}, false, err
	}
	return run, false, nil
}

func (s *Service) ListScanRuns(ctx context.Context, repository string, limit int) ([]ports.ScanRun, error) {
	return s.metadata.ListScanRuns(ctx, s.tenant(ctx), strings.TrimSpace(repository), limit)
}

func (s *Service) GetScanRunDetail(ctx context.Context, runID string) (ports.ScanRunDetail, error) {
	detail, err := s.metadata.GetScanRunDetail(ctx, s.tenant(ctx), strings.TrimSpace(runID))
	if err != nil {
		return ports.ScanRunDetail{}, err
	}
	detail.ReferenceFreshness = s.referenceFreshness(ctx, detail.Run)
	if strings.TrimSpace(detail.DBFreshness.FreshnessState) == "" {
		detail.DBFreshness.FreshnessState = ports.ScanRunDBFreshnessStateUnknown
	}
	return detail, nil
}

// GetSecretScanFindings looks up the redacted secret-scan findings for one
// image (repository@digest), mirroring GetScanRunDetail's role for the
// Trivy leg. Both legs are triggered from the same executeScanRun call for
// the same run, so repository+digest is a valid, sufficient attribution key
// (spec.md "Operator Visibility of Findings" — findings attributed to the
// image they were found in). Recent runs are scanned newest-first so an
// image rescanned multiple times resolves to its most recent secret scan.
func (s *Service) GetSecretScanFindings(ctx context.Context, repository string, digest string) (ports.SecretScanRunDetail, error) {
	repository = strings.TrimSpace(repository)
	digest = strings.TrimSpace(digest)
	runs, err := s.metadata.ListSecretScanRuns(ctx, s.tenant(ctx), repository, 50)
	if err != nil {
		return ports.SecretScanRunDetail{}, err
	}
	for _, run := range runs {
		if run.Digest == digest {
			return s.metadata.GetSecretScanRunDetail(ctx, s.tenant(ctx), run.ID)
		}
	}
	return ports.SecretScanRunDetail{}, domain.NewNotFoundError("secret scan run", repository+"@"+digest)
}

func (s *Service) RunScheduledScans(ctx context.Context) error {
	settings, err := s.resolveManagedScanSettings(ctx)
	if err != nil {
		return err
	}
	if !settings.Enabled || !settings.ScheduleEnabled {
		return nil
	}
	repositories, err := s.metadata.Catalog(ctx, s.tenant(ctx), 100, "")
	if err != nil {
		return err
	}
	for _, repository := range repositories {
		_, err := s.queueScheduledScan(ctx, repository.String(), "latest", settings)
		if err != nil && !domain.IsCode(err, domain.ErrorCodeNotFound) {
			return err
		}
	}
	return nil
}

func (s *Service) queueScheduledScan(ctx context.Context, repositoryName string, reference string, settings ports.ScanSettings) (ports.ScanRun, error) {
	settings, err := s.applyRepositoryOverride(ctx, s.tenant(ctx), repositoryName, trivyFeatureName, settings)
	if err != nil {
		return ports.ScanRun{}, err
	}
	if !settings.Enabled {
		// The sweep skips this repository (design.md Decision 4): a
		// disabling override is not an error, it is the same "nothing to
		// queue" outcome the caller already treats as a normal skip.
		return ports.ScanRun{}, nil
	}
	if _, err := s.scanTarget(settings, repositoryName, "sha256:placeholder"); err != nil {
		return ports.ScanRun{}, err
	}
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return ports.ScanRun{}, err
	}
	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, strings.TrimSpace(reference))
	if err != nil {
		return ports.ScanRun{}, err
	}
	digest := manifest.Digest.String()
	run, existed, err := s.dedupAndQueueScanRun(ctx, s.tenant(ctx), repository.String(), strings.TrimSpace(reference), digest, ports.ScanTriggerScheduled)
	if err != nil {
		return ports.ScanRun{}, err
	}
	if !existed {
		go s.executeScanRun(context.Background(), s.tenant(ctx), run, settings)
	}
	return run, nil
}

// queuePushScan is queueScheduledScan's push-time sibling (design.md
// Decision 4). It receives the digest directly (already computed by
// parseManifestPayload, so no ResolveManifest round-trip and no
// re-resolution race against a concurrent retag) and resolves settings
// itself, inside the caller's goroutine, rather than before it — unlike
// QueueManualScan/queueScheduledScan, whose synchronous settings resolution
// would surface an unready Trivy runtime as a failed push. It gates on
// settings.Enabled only, not ScheduleEnabled (which governs the periodic
// sweep, not push), and dedups via the existing GetActiveScanRunByDigest
// (queued|running) check. Every failure path returns silently: push must
// never fail or block because of scanning.
func (s *Service) queuePushScan(ctx context.Context, tenant string, repository string, reference string, digest string) {
	settings, err := s.resolveManagedScanSettingsForTenant(ctx, tenant)
	if err != nil {
		return
	}
	// The repository override is applied before the Enabled check (rather
	// than after an early return on the global row's Enabled) so an
	// Enabled: true override can re-enable push scanning for one repository
	// even while the global row is disabled (design.md Decision 4, proposal
	// resolved question 4). Every failure path still returns silently.
	settings, err = s.applyRepositoryOverride(ctx, tenant, repository, trivyFeatureName, settings)
	if err != nil || !settings.Enabled {
		return
	}
	run, existed, err := s.dedupAndQueueScanRun(ctx, tenant, repository, strings.TrimSpace(reference), digest, ports.ScanTriggerPush)
	if err != nil || existed {
		return
	}
	go s.executeScanRun(context.Background(), tenant, run, settings)
}

// resolveManagedScanSettingsForTenant mirrors resolveManagedScanSettings for
// the trivy feature, taking tenant explicitly rather than resolving it via
// s.tenant(ctx) — it is called from queuePushScan, which already holds the
// caller's resolved tenant from before its own goroutine started (same
// precedent as resolveManagedSecretScanSettings).
func (s *Service) resolveManagedScanSettingsForTenant(ctx context.Context, tenant string) (ports.ScanSettings, error) {
	settings, err := s.metadata.GetScanSettings(ctx, tenant, trivyFeatureName)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	state, err := s.metadata.GetFeatureRuntimeState(ctx, tenant, trivyFeatureName)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	if state.Status != ports.FeatureRuntimeStatusReady {
		return ports.ScanSettings{}, domain.NewValidationError(fmt.Sprintf("trivy runtime is %s", state.Status))
	}
	settings.BinaryPath = strings.TrimSpace(state.ActiveBinaryPath)
	settings.CacheDir = strings.TrimSpace(state.CacheDir)
	return settings, nil
}

func (s *Service) executeScanRun(ctx context.Context, tenant string, run ports.ScanRun, settings ports.ScanSettings) {
	scanRunner := s.getScanRunner()
	if scanRunner == nil {
		return
	}
	// The secret-scan leg reuses this same manual/scheduled rescan trigger
	// (spec.md "Reused Rescan Trigger, No Push-Time Path") and runs
	// alongside the Trivy leg in its own goroutine: it is informational
	// only, so a gitleaks failure or absence must never fail or block the
	// Trivy leg or the overall scan run's status/result.
	go s.executeSecretScanLeg(ctx, tenant, run.Repository, run.Digest, run.Trigger)
	if err := s.scanGate.acquire(ctx, settings.MaxConcurrency); err != nil {
		return
	}
	defer s.scanGate.release()
	now := s.now()
	run.Status = ports.ScanRunStatusRunning
	run.StartedAt = &now
	run.UpdatedAt = now
	s.persistAsyncScanRun(ctx, tenant, run)
	target, err := s.scanTarget(settings, run.Repository, run.Digest)
	if err != nil {
		finished := s.now()
		run.FinishedAt = &finished
		run.UpdatedAt = finished
		run.Status = ports.ScanRunStatusFailed
		run.Error = err.Error()
		s.persistAsyncScanRun(ctx, tenant, run)
		return
	}
	result, err := scanRunner.Run(ctx, target, settings)
	finished := s.now()
	run.FinishedAt = &finished
	run.UpdatedAt = finished
	if err != nil {
		run.Status = ports.ScanRunStatusFailed
		run.Error = err.Error()
		s.persistAsyncScanRun(ctx, tenant, run)
		return
	}
	run.Status = ports.ScanRunStatusCompleted
	run.Critical = result.Critical
	run.High = result.High
	run.Medium = result.Medium
	run.Low = result.Low
	run.TrivyVersion = result.TrivyVersion
	run.DBUpdatedAt = result.DBUpdatedAt
	run.Error = ""
	s.persistAsyncScanRunDetail(ctx, tenant, ports.ScanRunDetail{Run: run, Findings: result.Findings, DBFreshness: result.DBFreshness})
}

// executeSecretScanLeg runs the gitleaks secret scan for the manifest
// currently being (re)scanned by executeScanRun. It is entirely best-effort:
// any missing wiring, disabled feature, not-ready runtime, or scan failure
// simply returns without persisting anything or affecting the Trivy leg —
// secret findings are informational only (spec.md "Informational Findings
// Only"), never a gate.
func (s *Service) executeSecretScanLeg(ctx context.Context, tenant string, repository string, digest string, trigger string) {
	secretScanRunner := s.getSecretScanRunner()
	if secretScanRunner == nil {
		return
	}
	settings, err := s.resolveManagedSecretScanSettings(ctx, tenant)
	if err != nil {
		return
	}
	// Applied before the Enabled check, mirroring queuePushScan's Trivy
	// wiring: an Enabled: true override can re-enable this repository's
	// secret scanning even while the global gitleaks row is disabled
	// (design.md Decision 4, corrected image-secret-scans/spec.md).
	settings, err = s.applyRepositoryOverride(ctx, tenant, repository, gitleaksFeatureName, settings)
	if err != nil || !settings.Enabled {
		return
	}
	repo, err := parseRepository(repository)
	if err != nil {
		return
	}
	manifestDigest, err := domain.ParseDigest(digest)
	if err != nil {
		return
	}
	if _, err := s.metadata.GetActiveSecretScanRunByDigest(ctx, tenant, repository, digest); err == nil {
		return
	} else if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return
	}
	blobs, err := s.metadata.ListManifestBlobs(ctx, tenant, repo, manifestDigest)
	if err != nil {
		return
	}
	if err := s.secretScanGate.acquire(ctx, settings.MaxConcurrency); err != nil {
		return
	}
	defer s.secretScanGate.release()

	now := s.now()
	run := ports.SecretScanRun{
		ID:         uuid.NewString(),
		Repository: repository,
		Digest:     digest,
		Status:     ports.SecretScanRunStatusRunning,
		Trigger:    trigger,
		StartedAt:  &now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.persistAsyncSecretScanRun(ctx, tenant, run)

	target := ports.SecretScanTarget{Repository: repository, Digest: digest, Blobs: blobs}
	result, err := secretScanRunner.Run(ctx, target, settings)
	finished := s.now()
	run.FinishedAt = &finished
	run.UpdatedAt = finished
	if err != nil {
		run.Status = ports.SecretScanRunStatusFailed
		run.Error = err.Error()
		s.persistAsyncSecretScanRun(ctx, tenant, run)
		return
	}
	run.Status = ports.SecretScanRunStatusCompleted
	run.GitleaksVersion = result.GitleaksVersion
	run.FindingCount = len(result.Findings)
	run.Error = ""
	s.persistAsyncSecretScanRunDetail(ctx, tenant, ports.SecretScanRunDetail{Run: run, Findings: result.Findings})
}

func (s *Service) persistAsyncScanRun(ctx context.Context, tenant string, run ports.ScanRun) {
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := s.metadata.UpsertScanRun(ctx, tenant, run); err == nil {
			return
		} else if !isTransientScanRunPersistenceError(err) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
}

func (s *Service) persistAsyncScanRunDetail(ctx context.Context, tenant string, detail ports.ScanRunDetail) {
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := s.metadata.UpsertScanRunDetail(ctx, tenant, detail); err == nil {
			return
		} else if !isTransientScanRunPersistenceError(err) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
}

func (s *Service) persistAsyncSecretScanRun(ctx context.Context, tenant string, run ports.SecretScanRun) {
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := s.metadata.UpsertSecretScanRun(ctx, tenant, run); err == nil {
			return
		} else if !isTransientScanRunPersistenceError(err) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
}

func (s *Service) persistAsyncSecretScanRunDetail(ctx context.Context, tenant string, detail ports.SecretScanRunDetail) {
	const maxAttempts = 5

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := s.metadata.UpsertSecretScanRunDetail(ctx, tenant, detail); err == nil {
			return
		} else if !isTransientScanRunPersistenceError(err) {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 10 * time.Millisecond):
		}
	}
}

func (s *Service) referenceFreshness(ctx context.Context, run ports.ScanRun) string {
	if strings.TrimSpace(run.RequestedRef) == "" {
		return ports.ScanReferenceFreshnessUnknown
	}
	repository, err := parseRepository(run.Repository)
	if err != nil {
		return ports.ScanReferenceFreshnessUnknown
	}
	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, run.RequestedRef)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return ports.ScanReferenceFreshnessMissing
		}
		return ports.ScanReferenceFreshnessUnknown
	}
	if manifest.Digest.String() == strings.TrimSpace(run.Digest) {
		return ports.ScanReferenceFreshnessCurrent
	}
	return ports.ScanReferenceFreshnessMoved
}

func isTransientScanRunPersistenceError(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database is busy") ||
		strings.Contains(message, "sqlite_busy") ||
		strings.Contains(message, "sqlite_locked")
}

func (s *Service) scanTarget(settings ports.ScanSettings, repository string, digest string) (string, error) {
	base, err := s.scannerReachableRegistryBase(settings)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(base)
	if err == nil && strings.TrimSpace(parsed.Host) != "" {
		base = parsed.Host
	}
	return fmt.Sprintf("%s/%s@%s", strings.TrimSpace(base), strings.TrimSpace(repository), strings.TrimSpace(digest)), nil
}

func (s *Service) scannerReachableRegistryBase(settings ports.ScanSettings) (string, error) {
	base := strings.TrimSpace(settings.RegistryReachableURL)
	if base == "" {
		base = strings.TrimSpace(s.scanHost)
	}
	if base == "" || isLoopbackOnlyAddress(base) {
		return "", domain.NewValidationError("registry_reachable_url is required when the registry public URL is loopback-only")
	}
	return base, nil
}

func (s *Service) normalizeScanSettings(input ports.ScanSettings) (ports.ScanSettings, error) {
	settings := input
	settings.RegistryReachableURL = strings.TrimSpace(settings.RegistryReachableURL)
	if settings.RegistryReachableURL != "" {
		parsed, err := url.Parse(settings.RegistryReachableURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Host) == "" {
			return ports.ScanSettings{}, domain.NewValidationError("registry_reachable_url must use http or https")
		}
		settings.RegistryReachableURL = strings.TrimRight(parsed.String(), "/")
	}
	if settings.Timeout <= 0 {
		return ports.ScanSettings{}, domain.NewValidationError("timeout must be greater than zero")
	}
	if settings.Interval <= 0 {
		settings.Interval = 24 * time.Hour
	}
	if settings.MaxConcurrency <= 0 {
		return ports.ScanSettings{}, domain.NewValidationError("max_concurrency must be greater than zero")
	}
	settings.UpdatedAt = s.now()
	return settings, nil
}

func (s *Service) resolveManagedScanSettings(ctx context.Context) (ports.ScanSettings, error) {
	settings, err := s.GetScanSettings(ctx)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	state, err := s.metadata.GetFeatureRuntimeState(ctx, s.tenant(ctx), trivyFeatureName)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	if state.Status != ports.FeatureRuntimeStatusReady {
		detail := strings.TrimSpace(state.MigrationHint)
		if detail == "" {
			detail = strings.TrimSpace(state.LastError)
		}
		if detail == "" {
			detail = fmt.Sprintf("trivy runtime is %s", state.Status)
		}
		return ports.ScanSettings{}, domain.NewValidationError(detail)
	}
	settings.BinaryPath = strings.TrimSpace(state.ActiveBinaryPath)
	settings.CacheDir = strings.TrimSpace(state.CacheDir)
	return settings, nil
}

// resolveManagedSecretScanSettings mirrors resolveManagedScanSettings for
// the gitleaks feature, keyed independently via Phase 2's (tenant, feature)
// scan_settings/feature_runtime_state rows. It takes tenant explicitly
// (rather than resolving it again via s.tenant(ctx)) because it is always
// called from a goroutine already holding the caller's resolved tenant.
func (s *Service) resolveManagedSecretScanSettings(ctx context.Context, tenant string) (ports.ScanSettings, error) {
	settings, err := s.metadata.GetScanSettings(ctx, tenant, gitleaksFeatureName)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	state, err := s.metadata.GetFeatureRuntimeState(ctx, tenant, gitleaksFeatureName)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	if state.Status != ports.FeatureRuntimeStatusReady {
		detail := strings.TrimSpace(state.MigrationHint)
		if detail == "" {
			detail = strings.TrimSpace(state.LastError)
		}
		if detail == "" {
			detail = fmt.Sprintf("gitleaks runtime is %s", state.Status)
		}
		return ports.ScanSettings{}, domain.NewValidationError(detail)
	}
	settings.BinaryPath = strings.TrimSpace(state.ActiveBinaryPath)
	settings.CacheDir = strings.TrimSpace(state.CacheDir)
	return settings, nil
}

func isLoopbackOnlyAddress(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return true
	}
	parsed, err := url.Parse(trimmed)
	host := trimmed
	if err == nil && strings.TrimSpace(parsed.Host) != "" {
		host = parsed.Hostname()
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

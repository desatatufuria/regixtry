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

func (s *Service) QueueManualScan(ctx context.Context, repositoryName string, reference string) (ports.ScanRun, error) {
	settings, err := s.resolveManagedScanSettings(ctx)
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
	if active, err := s.metadata.GetActiveScanRunByDigest(ctx, s.tenant(ctx), repository.String(), digest); err == nil {
		return active, nil
	} else if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.ScanRun{}, err
	}
	now := s.now()
	run := ports.ScanRun{ID: uuid.NewString(), Repository: repository.String(), RequestedRef: strings.TrimSpace(reference), Digest: digest, Status: ports.ScanRunStatusQueued, Trigger: ports.ScanTriggerManual, CreatedAt: now, UpdatedAt: now}
	if err := s.metadata.UpsertScanRun(ctx, s.tenant(ctx), run); err != nil {
		return ports.ScanRun{}, err
	}
	go s.executeScanRun(context.Background(), s.tenant(ctx), run, settings)
	return run, nil
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
	if active, err := s.metadata.GetActiveScanRunByDigest(ctx, s.tenant(ctx), repository.String(), digest); err == nil {
		return active, nil
	} else if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		return ports.ScanRun{}, err
	}
	now := s.now()
	run := ports.ScanRun{ID: uuid.NewString(), Repository: repository.String(), RequestedRef: strings.TrimSpace(reference), Digest: digest, Status: ports.ScanRunStatusQueued, Trigger: ports.ScanTriggerScheduled, CreatedAt: now, UpdatedAt: now}
	if err := s.metadata.UpsertScanRun(ctx, s.tenant(ctx), run); err != nil {
		return ports.ScanRun{}, err
	}
	go s.executeScanRun(context.Background(), s.tenant(ctx), run, settings)
	return run, nil
}

func (s *Service) executeScanRun(ctx context.Context, tenant string, run ports.ScanRun, settings ports.ScanSettings) {
	if s.scanRunner == nil {
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
	result, err := s.scanRunner.Run(ctx, target, settings)
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
	if s.secretScanRunner == nil {
		return
	}
	settings, err := s.resolveManagedSecretScanSettings(ctx, tenant)
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
	result, err := s.secretScanRunner.Run(ctx, target, settings)
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

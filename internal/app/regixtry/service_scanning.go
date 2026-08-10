package regixtry

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func (s *Service) EnsureScanSettings(ctx context.Context, defaults ports.ScanSettings) (ports.ScanSettings, error) {
	settings, err := s.metadata.GetScanSettings(ctx, s.tenant(ctx))
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
	if err := s.metadata.UpsertScanSettings(ctx, s.tenant(ctx), normalized); err != nil {
		return ports.ScanSettings{}, err
	}
	return normalized, nil
}

func (s *Service) GetScanSettings(ctx context.Context) (ports.ScanSettings, error) {
	return s.metadata.GetScanSettings(ctx, s.tenant(ctx))
}

func (s *Service) UpdateScanSettings(ctx context.Context, input ports.ScanSettings) (ports.ScanSettings, error) {
	normalized, err := s.normalizeScanSettings(input)
	if err != nil {
		return ports.ScanSettings{}, err
	}
	if err := s.metadata.UpsertScanSettings(ctx, s.tenant(ctx), normalized); err != nil {
		return ports.ScanSettings{}, err
	}
	return normalized, nil
}

func (s *Service) QueueManualScan(ctx context.Context, repositoryName string, reference string) (ports.ScanRun, error) {
	settings, err := s.GetScanSettings(ctx)
	if err != nil {
		return ports.ScanRun{}, err
	}
	if !settings.Enabled {
		return ports.ScanRun{}, domain.NewValidationError("scan settings must be enabled")
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
	run := ports.ScanRun{
		ID:           uuid.NewString(),
		Repository:   repository.String(),
		RequestedRef: strings.TrimSpace(reference),
		Digest:       digest,
		Status:       ports.ScanRunStatusQueued,
		Trigger:      ports.ScanTriggerManual,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.metadata.UpsertScanRun(ctx, s.tenant(ctx), run); err != nil {
		return ports.ScanRun{}, err
	}
	go s.executeScanRun(context.Background(), s.tenant(ctx), run, settings)
	return run, nil
}

func (s *Service) ListScanRuns(ctx context.Context, repository string, limit int) ([]ports.ScanRun, error) {
	return s.metadata.ListScanRuns(ctx, s.tenant(ctx), strings.TrimSpace(repository), limit)
}

func (s *Service) RunScheduledScans(ctx context.Context) error {
	settings, err := s.GetScanSettings(ctx)
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
	if err := s.scanGate.acquire(ctx, settings.MaxConcurrency); err != nil {
		return
	}
	defer s.scanGate.release()
	now := s.now()
	run.Status = ports.ScanRunStatusRunning
	run.StartedAt = &now
	run.UpdatedAt = now
	_ = s.metadata.UpsertScanRun(ctx, tenant, run)
	target := s.scanTarget(run.Repository, run.Digest)
	result, err := s.scanRunner.Run(ctx, target, settings)
	finished := s.now()
	run.FinishedAt = &finished
	run.UpdatedAt = finished
	if err != nil {
		run.Status = ports.ScanRunStatusFailed
		run.Error = err.Error()
		_ = s.metadata.UpsertScanRun(ctx, tenant, run)
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
	_ = s.metadata.UpsertScanRun(ctx, tenant, run)
}

func (s *Service) scanTarget(repository string, digest string) string {
	host := strings.TrimSpace(s.scanHost)
	if parsed, err := url.Parse(host); err == nil && strings.TrimSpace(parsed.Host) != "" {
		host = parsed.Host
	}
	if host == "" {
		host = "registry.local"
	}
	return fmt.Sprintf("%s/%s@%s", host, strings.TrimSpace(repository), strings.TrimSpace(digest))
}

func (s *Service) normalizeScanSettings(input ports.ScanSettings) (ports.ScanSettings, error) {
	settings := input
	settings.CacheDir = strings.TrimSpace(settings.CacheDir)
	settings.BinaryPath = strings.TrimSpace(settings.BinaryPath)
	if settings.CacheDir == "" {
		return ports.ScanSettings{}, domain.NewValidationError("cache_dir is required")
	}
	if settings.BinaryPath == "" {
		settings.BinaryPath = "trivy"
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
	settings.CacheDir = filepath.Clean(settings.CacheDir)
	settings.UpdatedAt = s.now()
	return settings, nil
}

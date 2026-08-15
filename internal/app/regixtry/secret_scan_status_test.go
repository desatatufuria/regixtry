package regixtry

import (
	"context"
	"fmt"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// seedSecretScanRunForStatus inserts one secret_scan_runs row (plus
// findingCount dummy secret_scan_findings rows) directly through the
// metadata store, mirroring service_scanning.go's own persistAsyncSecretScanRun/
// persistAsyncSecretScanRunDetail write shape. FindingCount is never set on
// the persisted run row itself -- ListSecretScanRuns recomputes it from a
// COUNT(*) join (store.go:1274-1278), so the finding rows are what actually
// drive the returned FindingCount, not this helper's findingCount argument
// alone.
func seedSecretScanRunForStatus(t *testing.T, service *Service, tenant string, repository string, digest string, status string, findingCount int) ports.SecretScanRun {
	t.Helper()

	now := time.Now().UTC()
	run := ports.SecretScanRun{
		ID:         fmt.Sprintf("run-%s-%s", digest, status),
		Repository: repository,
		Digest:     digest,
		Status:     status,
		Trigger:    ports.ScanTriggerManual,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if status == ports.SecretScanRunStatusCompleted || status == ports.SecretScanRunStatusFailed {
		finished := now
		run.FinishedAt = &finished
	}

	findings := make([]ports.SecretFinding, 0, findingCount)
	for i := 0; i < findingCount; i++ {
		findings = append(findings, ports.SecretFinding{RuleID: fmt.Sprintf("rule-%d", i), BlobDigest: digest})
	}

	if err := service.metadata.UpsertSecretScanRunDetail(context.Background(), tenant, ports.SecretScanRunDetail{Run: run, Findings: findings}); err != nil {
		t.Fatalf("UpsertSecretScanRunDetail() error = %v", err)
	}
	return run
}

// TestServiceSecretScanStatusResolvesAllFiveStates is the Phase 1 RED test
// (tasks.md 1.2): table-driven, all five states resolve correctly for a
// seeded run per state, mirroring signature_status_test.go's table-driven
// shape.
func TestServiceSecretScanStatusResolvesAllFiveStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       string
		findingCount int
		want         string
		wantScan     bool
	}{
		{name: "no matching run -- unscanned", want: SecretScanStatusUnscanned, wantScan: false},
		{name: "queued -- in_progress", status: ports.SecretScanRunStatusQueued, want: SecretScanStatusInProgress, wantScan: true},
		{name: "running -- in_progress", status: ports.SecretScanRunStatusRunning, want: SecretScanStatusInProgress, wantScan: true},
		{name: "failed -- failed", status: ports.SecretScanRunStatusFailed, want: SecretScanStatusFailed, wantScan: true},
		{name: "completed, zero findings -- clean", status: ports.SecretScanRunStatusCompleted, findingCount: 0, want: SecretScanStatusClean, wantScan: true},
		{name: "completed, one finding -- findings_present", status: ports.SecretScanRunStatusCompleted, findingCount: 1, want: SecretScanStatusFindingsPresent, wantScan: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()

			repository := "library/alpine"
			digest := seedArbitraryImageManifest(t, service, repository, tt.name)

			if tt.status != "" {
				seedSecretScanRunForStatus(t, service, "tenant-a", repository, digest, tt.status, tt.findingCount)
			}

			result, err := service.SecretScanStatus(context.Background(), repository, digest)
			if err != nil {
				t.Fatalf("SecretScanStatus() error = %v", err)
			}
			if result.State != tt.want {
				t.Fatalf("SecretScanStatus().State = %q, want %q", result.State, tt.want)
			}
			if result.Repository != repository || result.Digest != digest {
				t.Fatalf("SecretScanStatus() repository/digest = %q/%q, want %q/%q", result.Repository, result.Digest, repository, digest)
			}
			if tt.wantScan && result.Scan == nil {
				t.Fatal("SecretScanStatus().Scan = nil, want a scan object")
			}
			if !tt.wantScan && result.Scan != nil {
				t.Fatalf("SecretScanStatus().Scan = %#v, want nil for state %q", result.Scan, tt.want)
			}
			if tt.wantScan && result.Scan.FindingCount != tt.findingCount {
				t.Fatalf("SecretScanStatus().Scan.FindingCount = %d, want %d", result.Scan.FindingCount, tt.findingCount)
			}
		})
	}
}

// TestServiceSecretScanStatusNeverMisreportsCompletedRunAsUnscanned is the
// Phase 1 RED test (tasks.md 1.3, design.md Decision 1, exploration's
// "Critical gotcha"): a completed run for the target digest resolves
// clean/findings_present even when a stale queued/running row exists for the
// same repository at a *different* digest -- guarding against reaching for
// GetActiveSecretScanRunByDigest, which only tracks queued/running rows and
// would misreport this as unscanned.
func TestServiceSecretScanStatusNeverMisreportsCompletedRunAsUnscanned(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	staleDigest := seedArbitraryImageManifest(t, service, repository, "-stale")
	targetDigest := seedArbitraryImageManifest(t, service, repository, "-target")

	// Stale queued/running row at a different digest in the same repository
	// -- exactly the shape GetActiveSecretScanRunByDigest would find first if
	// SecretScanStatus reached for it by mistake.
	seedSecretScanRunForStatus(t, service, "tenant-a", repository, staleDigest, ports.SecretScanRunStatusRunning, 0)
	seedSecretScanRunForStatus(t, service, "tenant-a", repository, targetDigest, ports.SecretScanRunStatusCompleted, 2)

	result, err := service.SecretScanStatus(context.Background(), repository, targetDigest)
	if err != nil {
		t.Fatalf("SecretScanStatus() error = %v", err)
	}
	if result.State != SecretScanStatusFindingsPresent {
		t.Fatalf("SecretScanStatus().State = %q, want %q (never unscanned despite a stale queued/running row for a different digest)", result.State, SecretScanStatusFindingsPresent)
	}
}

// TestServiceSecretScanStatusScanSettingsNotFoundDefaultsToDisabled is the
// Phase 1 RED test (tasks.md 1.4, design.md Decision 2): GetScanSettings
// returning NotFound (no gitleaks scan_settings row at all, a real case in
// fresh installs before EnsureScanSettings has run for gitleaks) yields
// Policy.Enabled == false, with no error -- unlike GetScanPolicySettings's
// fail-open-to-true default, gitleaks is informational only, so NotFound
// defaults to under-reporting rather than claiming scanning is on.
func TestServiceSecretScanStatusScanSettingsNotFoundDefaultsToDisabled(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, "")

	result, err := service.SecretScanStatus(context.Background(), repository, digest)
	if err != nil {
		t.Fatalf("SecretScanStatus() error = %v, want no error even though no gitleaks scan_settings row exists", err)
	}
	if result.Policy.Enabled {
		t.Fatal("SecretScanStatus().Policy.Enabled = true, want false when no gitleaks scan_settings row exists")
	}
}

// TestServiceSecretScanStatusRepositoryOverrideFlipsPolicyEnabled is the
// Phase 1 RED test (tasks.md 1.5): a repository-level gitleaks override
// flips Policy.Enabled independent of the tenant-wide row, mirroring
// repository_overrides_test.go's gitleaks case.
func TestServiceSecretScanStatusRepositoryOverrideFlipsPolicyEnabled(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	repository := "library/alpine"
	digest := seedArbitraryImageManifest(t, service, repository, "")

	seedManagedGitleaksSettings(t, service, false)
	seedGitleaksOverride(t, service, repository, ports.GitleaksOverride{Enabled: true})

	result, err := service.SecretScanStatus(context.Background(), repository, digest)
	if err != nil {
		t.Fatalf("SecretScanStatus() error = %v", err)
	}
	if !result.Policy.Enabled {
		t.Fatal("SecretScanStatus().Policy.Enabled = false, want true: repository override must flip the disabled global row")
	}
}

// TestServiceSecretScanStatusRequiresPullAuthorization proves
// SecretScanStatus authorizes with ports.ActionPull, exactly like
// ScanStatus/SignatureStatus -- not an admin action.
func TestServiceSecretScanStatusRequiresPullAuthorization(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	repository := "library/alpine"
	_, err := service.SecretScanStatus(context.Background(), repository, "latest")
	if err == nil {
		t.Fatal("SecretScanStatus() error = nil, want an authorization error when access is denied")
	}
}

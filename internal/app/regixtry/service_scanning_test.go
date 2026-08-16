package regixtry

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// TestScanPolicyViolated covers the full fail-open matrix from design.md
// Decision 2. This is the single highest-value test surface in the change:
// every non-completed status must allow (fail-open on scan-state
// uncertainty), and only a completed run whose findings meet or exceed the
// configured threshold blocks.
func TestScanPolicyViolated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		settings  ports.ScanPolicySettings
		run       ports.ScanRun
		wantBlock bool
	}{
		{
			name:      "queued run allows",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusQueued, Critical: 5},
			wantBlock: false,
		},
		{
			name:      "running run allows",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusRunning, Critical: 5},
			wantBlock: false,
		},
		{
			name:      "failed run allows",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusFailed, Critical: 5},
			wantBlock: false,
		},
		{
			name:      "completed critical at CRITICAL threshold blocks",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 1},
			wantBlock: true,
		},
		{
			name:      "completed critical at CRITICAL_HIGH threshold blocks",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 1},
			wantBlock: true,
		},
		{
			name:      "completed high-only at CRITICAL threshold allows",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 0, High: 3},
			wantBlock: false,
		},
		{
			name:      "completed high-only at CRITICAL_HIGH threshold blocks",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 0, High: 3},
			wantBlock: true,
		},
		{
			name:      "completed clean at CRITICAL threshold allows",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 0, High: 0, Medium: 2, Low: 1},
			wantBlock: false,
		},
		{
			name:      "completed clean at CRITICAL_HIGH threshold allows",
			settings:  ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCriticalHigh},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 0, High: 0},
			wantBlock: false,
		},
		{
			name:      "policy disabled allows a violating completed run",
			settings:  ports.ScanPolicySettings{Enabled: false, SeverityThreshold: ports.ScanPolicyThresholdCritical},
			run:       ports.ScanRun{Status: ports.ScanRunStatusCompleted, Critical: 9},
			wantBlock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := scanPolicyViolated(tt.settings, tt.run)
			if got != tt.wantBlock {
				t.Fatalf("scanPolicyViolated(%#v, %#v) = %v, want %v", tt.settings, tt.run, got, tt.wantBlock)
			}
		})
	}
}

// TestApplyRepositoryOverrideResolvesRowPresenceBoundary is the Phase 3 RED
// test (tasks.md 3.5) backing design.md Decision 4's applyRepositoryOverride
// helper: NotFound means "use the global row" (never an error), a found row
// resolves through the registered codec's Apply, and an unrecognized feature
// name is a defensive no-op rather than a crash.
func TestApplyRepositoryOverrideResolvesRowPresenceBoundary(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	base := ports.ScanSettings{Enabled: true, MaxConcurrency: 4}

	t.Run("NotFound returns settings unchanged", func(t *testing.T) {
		got, err := service.applyRepositoryOverride(context.Background(), "tenant-a", "library/alpine", trivyFeatureName, base)
		if err != nil {
			t.Fatalf("applyRepositoryOverride() error = %v", err)
		}
		if got != base {
			t.Fatalf("applyRepositoryOverride() = %#v, want unchanged %#v", got, base)
		}
	})

	t.Run("found row applies codec.Apply", func(t *testing.T) {
		payload := marshalOverride(t, ports.TrivyOverride{Enabled: false, IgnoreFilePath: "/etc/regixtry/ignore/alpine.trivyignore"})
		if err := service.metadata.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/alpine", trivyFeatureName, payload); err != nil {
			t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
		}
		got, err := service.applyRepositoryOverride(context.Background(), "tenant-a", "library/alpine", trivyFeatureName, base)
		if err != nil {
			t.Fatalf("applyRepositoryOverride() error = %v", err)
		}
		want := ports.ScanSettings{Enabled: false, MaxConcurrency: 4, IgnoreFilePath: "/etc/regixtry/ignore/alpine.trivyignore"}
		if got != want {
			t.Fatalf("applyRepositoryOverride() = %#v, want %#v", got, want)
		}
	})

	t.Run("unrecognized feature name returns settings unchanged", func(t *testing.T) {
		payload := marshalOverride(t, map[string]any{"enabled": false})
		if err := service.metadata.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", "library/other", "image-signing", payload); err != nil {
			t.Fatalf("UpsertRepositoryFeatureOverride() error = %v", err)
		}
		got, err := service.applyRepositoryOverride(context.Background(), "tenant-a", "library/other", "image-signing", base)
		if err != nil {
			t.Fatalf("applyRepositoryOverride() error = %v", err)
		}
		if got != base {
			t.Fatalf("applyRepositoryOverride() = %#v, want unchanged %#v", got, base)
		}
	})
}

// seedTrivyOverride stores a trivy repository override row directly, the
// same shape SetRepositoryOverride will use once Phase 7 wires the HTTP
// resource — Phase 4/5 wiring is exercised without going through HTTP.
func seedTrivyOverride(t *testing.T, service *Service, repository string, override ports.TrivyOverride) {
	t.Helper()
	payload := marshalOverride(t, override)
	if err := service.metadata.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", repository, trivyFeatureName, payload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(trivy) error = %v", err)
	}
}

// seedGitleaksOverride mirrors seedTrivyOverride for the gitleaks feature.
func seedGitleaksOverride(t *testing.T, service *Service, repository string, override ports.GitleaksOverride) {
	t.Helper()
	payload := marshalOverride(t, override)
	if err := service.metadata.UpsertRepositoryFeatureOverride(context.Background(), "tenant-a", repository, gitleaksFeatureName, payload); err != nil {
		t.Fatalf("UpsertRepositoryFeatureOverride(gitleaks) error = %v", err)
	}
}

// seedManagedGitleaksSettings seeds the global gitleaks scan_settings row and
// a Ready feature_runtime_state row, mirroring the shape
// TestServiceExecuteScanRunAlsoRunsGitleaksSecretLegAlongsideTrivy already
// establishes for the two-leg scan flow.
func seedManagedGitleaksSettings(t *testing.T, service *Service, enabled bool) {
	t.Helper()
	if err := service.metadata.UpsertScanSettings(context.Background(), "tenant-a", gitleaksFeatureName, ports.ScanSettings{
		Enabled:        enabled,
		Interval:       time.Hour,
		Timeout:        time.Minute,
		MaxConcurrency: 1,
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertScanSettings(gitleaks) error = %v", err)
	}
	if err := service.metadata.UpsertFeatureRuntimeState(context.Background(), "tenant-a", gitleaksFeatureName, ports.FeatureRuntimeState{
		Status:           ports.FeatureRuntimeStatusReady,
		ActiveVersion:    "8.27.0",
		ActiveBinaryPath: "/var/lib/regixtry/features/gitleaks/bin/active/gitleaks",
		CacheDir:         "/var/lib/regixtry/features/gitleaks/gitleaks-cache",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertFeatureRuntimeState(gitleaks) error = %v", err)
	}
}

// TestExecuteSecretScanLegSkipsWithDisablingOverride is the Phase 5 RED test
// (tasks.md 5.1): a gitleaks override with Enabled: false must suppress
// SecretScanRun creation for a manual/scheduled trigger.
func TestExecuteSecretScanLegSkipsWithDisablingOverride(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedManagedGitleaksSettings(t, service, true)
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)
	seedGitleaksOverride(t, service, "library/alpine", ports.GitleaksOverride{Enabled: false})

	digest := "sha256:" + strings.Repeat("1", 64)
	service.executeSecretScanLeg(context.Background(), "tenant-a", "library/alpine", digest, ports.ScanTriggerManual)

	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("secret runner calls = %d, want 0 for an overridden-disabled repository", calls)
	}
	runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("secret scan runs = %#v, want none for an overridden-disabled repository", runs)
	}
}

// TestQueuePushScanSuppressesGitleaksLegWithDisablingOverride is the Phase 5
// RED test (tasks.md 5.2). Unlike 5.1, this exercises the *actual* push code
// path — queuePushScan -> executeScanRun -> executeSecretScanLeg — rather
// than calling executeSecretScanLeg directly, proving the corrected
// image-secret-scans/spec.md push-suppression scenario against the real
// call chain, not just the trigger-agnostic function signature.
func TestQueuePushScanSuppressesGitleaksLegWithDisablingOverride(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	// Trivy must be enabled + ready: queuePushScan only reaches
	// executeScanRun (which is what actually launches the secret-scan leg
	// goroutine) when the Trivy leg itself proceeds.
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings(trivy) error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	trivyRunner := &capturingScanRunner{result: ports.ScanResult{TrivyVersion: "0.57.1"}}
	service.SetScanRunner(trivyRunner)

	seedManagedGitleaksSettings(t, service, true)
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)
	seedGitleaksOverride(t, service, "library/alpine", ports.GitleaksOverride{Enabled: false})

	digest := "sha256:" + strings.Repeat("f", 64)
	service.queuePushScan(context.Background(), "tenant-a", "library/alpine", "latest", digest)

	waitForRunnerTargets(t, trivyRunner, 1)
	// Give the secret leg goroutine time to run before asserting suppression.
	time.Sleep(50 * time.Millisecond)
	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("secret runner calls = %d, want 0: the real push code path must suppress gitleaks for an overridden-disabled repository", calls)
	}
	runs, err := service.metadata.ListSecretScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListSecretScanRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("secret scan runs = %#v, want none persisted for an overridden-disabled repository", runs)
	}
}

// TestExecuteSecretScanLegReEnabledByOverrideWhenGlobalRowDisabled is the
// Phase 5 RED test (tasks.md 5.3), mirroring 4.4 for the secret leg: an
// Enabled: true override re-enables gitleaks even while the global row's
// Enabled is false.
func TestExecuteSecretScanLegReEnabledByOverrideWhenGlobalRowDisabled(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedGitleaksSettings(t, service, false)
	secretRunner := &capturingSecretScanRunner{}
	service.SetSecretScanRunner(secretRunner)
	seedGitleaksOverride(t, service, "library/alpine", ports.GitleaksOverride{Enabled: true})

	digest := seededManifestDigest(t, service, "library/alpine")
	service.executeSecretScanLeg(context.Background(), "tenant-a", "library/alpine", digest, ports.ScanTriggerScheduled)

	secretRunner.mu.Lock()
	calls := len(secretRunner.targets)
	secretRunner.mu.Unlock()
	if calls != 1 {
		t.Fatalf("secret runner calls = %d, want 1 (override re-enables secret scanning)", calls)
	}
}

// TestQueueScheduledScanSkipsRepositoryWithDisablingOverride is the Phase 4
// RED test (tasks.md 4.1): a Trivy override with Enabled: false for one
// repository must suppress the scheduled sweep's queueing for it, even
// though the global row's Enabled is true.
func TestQueueScheduledScanSkipsRepositoryWithDisablingOverride(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	seedManagedRuntimeState(t, service, "0.57.1")
	seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: false})

	globalSettings := ports.ScanSettings{Enabled: true, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}
	run, err := service.queueScheduledScan(context.Background(), "library/alpine", "latest", globalSettings)
	if err != nil {
		t.Fatalf("queueScheduledScan() error = %v", err)
	}
	if run != (ports.ScanRun{}) {
		t.Fatalf("queueScheduledScan() run = %#v, want zero value (repository skipped)", run)
	}

	runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("scan runs = %#v, want none queued for an overridden-disabled repository", runs)
	}
}

// TestQueuePushScanSkipsRepositoryWithDisablingOverride is the Phase 4 RED
// test (tasks.md 4.2), mirroring 4.1 for the push path.
func TestQueuePushScanSkipsRepositoryWithDisablingOverride(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: false})

	digest := "sha256:" + strings.Repeat("d", 64)
	service.queuePushScan(context.Background(), "tenant-a", "library/alpine", "latest", digest)

	if _, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/alpine", digest); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetActiveScanRunByDigest() error = %v, want ErrorCodeNotFound (no run queued for overridden-disabled repository)", err)
	}
}

// TestPublishManifestSkipsPushTriggeredScanForSubjectReferencingManifest is
// the RED test reproducing the live bug found while investigating a
// production "failed" Repository Alerts row: PublishManifest fires a push-
// triggered Trivy scan for EVERY manifest, including OCI 1.1
// subject-referencing ones (cosign signature bundles, attestations, SBOMs)
// -- which are never a real image with layers Trivy can scan, so the scan
// always fails with "exit status 1", inflating Runs and polluting
// Repository Alerts with a permanent failed row for something that can
// never succeed. A subject-referencing manifest must never queue a
// push-triggered scan at all, for its own digest -- the manifest it refers
// to (the real image) is unaffected and keeps getting scanned normally.
func TestPublishManifestSkipsPushTriggeredScanForSubjectReferencingManifest(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	trivyRunner := &capturingScanRunner{result: ports.ScanResult{TrivyVersion: "0.57.1"}}
	service.SetScanRunner(trivyRunner)

	upload, err := service.BeginUpload(context.Background(), "library/subject")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(context.Background(), "library/subject", upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	blobPayload := []byte("layer-one")
	blob, err := service.CompleteUpload(context.Background(), "library/subject", upload.ID, digestForTest(blobPayload), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	imageManifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + blob.Digest + `","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + blob.Digest + `","size":9}]}`)
	image, err := service.PublishManifest(context.Background(), "library/subject", "v1", "application/vnd.oci.image.manifest.v1+json", imageManifestPayload)
	if err != nil {
		t.Fatalf("PublishManifest(image) error = %v", err)
	}
	// The real image must still get its own push-triggered scan -- this
	// change must not suppress scanning generally, only for
	// subject-referencing manifests.
	waitForRunnerTargets(t, trivyRunner, 1)

	signaturePayload := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","subject":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"size":%d},"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":%q,"size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":%q,"size":9}]}`, image.Digest, image.Size, blob.Digest, blob.Digest))
	signature, err := service.PublishManifest(context.Background(), "library/subject", "sha256-deadbeef", "application/vnd.oci.image.manifest.v1+json", signaturePayload)
	if err != nil {
		t.Fatalf("PublishManifest(signature) error = %v", err)
	}
	service.WaitForBackgroundWork()

	// Give any (incorrectly fired) scan goroutine time to reach the runner
	// before asserting it never did.
	time.Sleep(100 * time.Millisecond)
	trivyRunner.mu.Lock()
	targetCount := len(trivyRunner.targets)
	trivyRunner.mu.Unlock()
	if targetCount != 1 {
		t.Fatalf("trivyRunner.targets = %d, want still 1 (the subject-referencing manifest must never reach the scan runner)", targetCount)
	}

	if _, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/subject", signature.Digest); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("GetActiveScanRunByDigest(signature digest) error = %v, want ErrorCodeNotFound (no scan run ever queued for it)", err)
	}
}

// TestQueueManualScanRejectsRepositoryWithDisablingOverride is the Phase 4
// RED test (tasks.md 4.3): a manual scan against a repository with a
// disabling Trivy override must return the same validation error shape as
// the existing global-disabled branch.
func TestQueueManualScanRejectsRepositoryWithDisablingOverride(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	seedRepository(t, service, context.Background(), "library/alpine")
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: false})

	if _, err := service.QueueManualScan(context.Background(), "library/alpine", "latest"); err == nil {
		t.Fatal("QueueManualScan() error = nil, want a validation error for a repository-disabled override")
	} else if !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("QueueManualScan() error = %v, want ErrorCodeValidation", err)
	}
}

// TestRepositoryOverrideReEnablesScanningWhenGlobalRowDisabled is the Phase 4
// RED test (tasks.md 4.4): an Enabled: true override re-enables scanning for
// one repository even when the global row's Enabled is false, across all
// three Trivy trigger paths (proposal's resolved question 4).
func TestRepositoryOverrideReEnablesScanningWhenGlobalRowDisabled(t *testing.T) {
	t.Parallel()

	t.Run("scheduled trigger", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		seedRepository(t, service, context.Background(), "library/alpine")
		seedManagedRuntimeState(t, service, "0.57.1")
		seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: true})

		globalDisabled := ports.ScanSettings{Enabled: false, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}
		run, err := service.queueScheduledScan(context.Background(), "library/alpine", "latest", globalDisabled)
		if err != nil {
			t.Fatalf("queueScheduledScan() error = %v", err)
		}
		if run.ID == "" {
			t.Fatalf("queueScheduledScan() run = %#v, want a queued run (override re-enables)", run)
		}
	})

	t.Run("push trigger", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: false, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
			t.Fatalf("EnsureScanSettings() error = %v", err)
		}
		seedManagedRuntimeState(t, service, "0.57.1")
		seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: true})

		digest := "sha256:" + strings.Repeat("e", 64)
		service.queuePushScan(context.Background(), "tenant-a", "library/alpine", "latest", digest)

		run, err := service.metadata.GetActiveScanRunByDigest(context.Background(), "tenant-a", "library/alpine", digest)
		if err != nil {
			t.Fatalf("GetActiveScanRunByDigest() error = %v, want a queued run (override re-enables push scanning)", err)
		}
		if run.Trigger != ports.ScanTriggerPush {
			t.Fatalf("run.Trigger = %q, want push", run.Trigger)
		}
	})

	t.Run("manual trigger", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()

		seedRepository(t, service, context.Background(), "library/alpine")
		if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: false, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
			t.Fatalf("EnsureScanSettings() error = %v", err)
		}
		seedManagedRuntimeState(t, service, "0.57.1")
		seedTrivyOverride(t, service, "library/alpine", ports.TrivyOverride{Enabled: true})

		run, err := service.QueueManualScan(context.Background(), "library/alpine", "latest")
		if err != nil {
			t.Fatalf("QueueManualScan() error = %v, want success (override re-enables manual scanning)", err)
		}
		if run.ID == "" {
			t.Fatalf("QueueManualScan() run = %#v, want a queued run", run)
		}
	})
}

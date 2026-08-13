package regixtry

import (
	"context"
	"testing"

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

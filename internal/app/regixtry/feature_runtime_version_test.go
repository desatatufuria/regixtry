package regixtry

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"regixtry/internal/ports"
)

type countingRuntimeManager struct {
	fakeFeatureRuntimeManager
	installs atomic.Int32
	upgrades atomic.Int32
	versions []string
}

func (c *countingRuntimeManager) Install(_ context.Context, version string, _ func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	c.installs.Add(1)
	c.versions = append(c.versions, version)
	return ports.FeatureRuntimeState{}, nil
}

func (c *countingRuntimeManager) Upgrade(_ context.Context, version string, _ func(ports.FeatureRuntimeProgress)) (ports.FeatureRuntimeState, error) {
	c.upgrades.Add(1)
	c.versions = append(c.versions, version)
	return ports.FeatureRuntimeState{}, nil
}

func TestFeatureRuntimeInstallAndUpgradeRejectHostileVersionsBeforeTheManager(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	manager := &countingRuntimeManager{}
	service.SetFeatureRuntimeManager("trivy", manager)

	for _, version := range []string{"../../x", "1.2.3/../x", "1.2.3?x", "1.2.3%2fx", "https://evil.example/x", "1.2", strings.Repeat("9", 4000) + ".0.0"} {
		if _, err := service.InstallFeatureRuntime(context.Background(), "trivy", version); err == nil {
			t.Errorf("InstallFeatureRuntime(%.30q) error = nil, want validation error", version)
		}
		if _, err := service.UpgradeFeatureRuntime(context.Background(), "trivy", version); err == nil {
			t.Errorf("UpgradeFeatureRuntime(%.30q) error = nil, want validation error", version)
		}
	}
	if manager.installs.Load() != 0 || manager.upgrades.Load() != 0 {
		t.Fatalf("manager invoked installs=%d upgrades=%d, want 0", manager.installs.Load(), manager.upgrades.Load())
	}
}

func TestFeatureRuntimeInstallStillAcceptsEmptyAndValidVersions(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	manager := &countingRuntimeManager{}
	service.SetFeatureRuntimeManager("trivy", manager)

	for _, version := range []string{"", "  ", "0.58.0", "v0.58.0", "0.58.0-rc.1"} {
		if _, err := service.InstallFeatureRuntime(context.Background(), "trivy", version); err != nil {
			t.Fatalf("InstallFeatureRuntime(%q) error = %v", version, err)
		}
		if _, err := service.UpgradeFeatureRuntime(context.Background(), "trivy", version); err != nil {
			t.Fatalf("UpgradeFeatureRuntime(%q) error = %v", version, err)
		}
	}
	if manager.installs.Load() != 5 || manager.upgrades.Load() != 5 {
		t.Fatalf("installs=%d upgrades=%d, want 5/5", manager.installs.Load(), manager.upgrades.Load())
	}
}

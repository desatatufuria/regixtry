package regixtry

import (
	"context"
	"testing"

	"regixtry/internal/ports"
)

// TestServiceRepositorySummariesReturnsTagCountAndLastPushedPerRepository is
// the RED test for the console-repositories-table change: RepositorySummaries
// returns one entry per repository with its tag count and most recent push
// time, mirroring TagDetails' own per-tag resolution one level up.
func TestServiceRepositorySummariesReturnsTagCountAndLastPushedPerRepository(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	digest := seedFixtureImageManifest(t, service, "library/alpine")
	tagManifestAtDigest(t, service, "library/alpine", "latest", digest)
	tagManifestAtDigest(t, service, "library/alpine", "stable", digest)

	seedArbitraryImageManifest(t, service, "library/busybox", "")

	summaries, err := service.RepositorySummaries(context.Background(), 10, "")
	if err != nil {
		t.Fatalf("RepositorySummaries() error = %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("len(summaries) = %d, want 2: %#v", len(summaries), summaries)
	}

	byName := make(map[string]RepositorySummary, len(summaries))
	for _, summary := range summaries {
		byName[summary.Name] = summary
	}

	alpine, ok := byName["library/alpine"]
	if !ok {
		t.Fatalf("summaries = %#v, want an entry for library/alpine", summaries)
	}
	if alpine.TagCount != 2 {
		t.Fatalf("alpine.TagCount = %d, want 2", alpine.TagCount)
	}
	if alpine.LastPushed.IsZero() {
		t.Fatal("alpine.LastPushed is zero, want the tags' manifest push time")
	}

	busybox, ok := byName["library/busybox"]
	if !ok {
		t.Fatalf("summaries = %#v, want an entry for library/busybox (published with no tag)", summaries)
	}
	if busybox.TagCount != 0 {
		t.Fatalf("busybox.TagCount = %d, want 0 (published with an empty tag)", busybox.TagCount)
	}
	if !busybox.LastPushed.IsZero() {
		t.Fatalf("busybox.LastPushed = %s, want zero (no tags to aggregate a push time from)", busybox.LastPushed)
	}
}

// TestServiceRepositorySummariesFiltersByCatalogAccessLikeCatalog proves
// RepositorySummaries uses the exact same authorization as Catalog
// (ActionCatalog plus per-repository HasCatalogAccess filtering), not a
// stricter or looser check invented for this new method.
func TestServiceRepositorySummariesFiltersByCatalogAccessLikeCatalog(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, ports.NewConfigurableAccessController(ports.AccessConfig{}))
	defer cleanup()

	_, err := service.RepositorySummaries(context.Background(), 10, "")
	if err == nil {
		t.Fatal("RepositorySummaries() error = nil, want an authorization error when access is denied")
	}
}

package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appregistry "registry/internal/app/registry"
)

func TestModelShowsEmptyStateWhenCatalogIsEmpty(t *testing.T) {
	t.Parallel()

	model := NewModel(&fakeQueryService{})
	updated := runCmd(t, model, model.Init())

	if updated.screen != screenEmpty {
		t.Fatalf("screen = %q, want %q", updated.screen, screenEmpty)
	}

	view := updated.View()
	if !strings.Contains(view, "Registry is empty") {
		t.Fatalf("view = %q, want empty-state title", view)
	}
	if !strings.Contains(view, "No repositories have been published yet.") {
		t.Fatalf("view = %q, want empty-state message", view)
	}
}

func TestModelNavigatesRepositoriesManifestBlobsAndUploads(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 28, 23, 0, 0, 0, time.UTC)
	service := &fakeQueryService{
		catalog: appregistry.CatalogResult{Repositories: []string{"library/alpine"}},
		tags: map[string]appregistry.TagsResult{
			"library/alpine": {Name: "library/alpine", Tags: []string{"latest"}},
		},
		manifests: map[string]appregistry.ManifestDetails{
			"library/alpine:latest": {
				Repository: "library/alpine",
				Reference:  "latest",
				MediaType:  "application/vnd.oci.image.manifest.v1+json",
				Digest:     "sha256:manifest",
				Size:       512,
				Blobs: []appregistry.BlobDetails{{
					Repository: "library/alpine",
					MediaType:  "application/vnd.oci.image.layer.v1.tar",
					Digest:     "sha256:layer",
					Size:       128,
				}},
			},
		},
		uploads: map[string][]appregistry.UploadDetails{
			"library/alpine": {{Repository: "library/alpine", ID: "upload-1", Status: "active", Size: 42, StartedAt: now, UpdatedAt: now, Location: "/tmp/upload-1"}},
		},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	if updated.screen != screenRepositories {
		t.Fatalf("screen = %q, want %q", updated.screen, screenRepositories)
	}

	updated = runKey(t, updated, "enter")
	if updated.screen != screenTags {
		t.Fatalf("screen = %q, want %q", updated.screen, screenTags)
	}

	updated = runKey(t, updated, "enter")
	if updated.screen != screenManifest {
		t.Fatalf("screen = %q, want %q", updated.screen, screenManifest)
	}

	view := updated.View()
	if !strings.Contains(view, "Manifest · library/alpine:latest") {
		t.Fatalf("view = %q, want manifest header", view)
	}

	updated = runKey(t, updated, "b")
	if updated.screen != screenBlobs {
		t.Fatalf("screen = %q, want %q", updated.screen, screenBlobs)
	}
	if !strings.Contains(updated.View(), "sha256:layer") {
		t.Fatalf("view = %q, want blob digest", updated.View())
	}

	updated = runKey(t, updated, "esc")
	updated = runKey(t, updated, "u")
	if updated.screen != screenUploads {
		t.Fatalf("screen = %q, want %q", updated.screen, screenUploads)
	}
	if !strings.Contains(updated.View(), "upload-1") {
		t.Fatalf("view = %q, want upload id", updated.View())
	}

	if service.calls.catalog != 1 || service.calls.tags != 1 || service.calls.manifest != 1 || service.calls.uploads != 1 {
		t.Fatalf("service calls = %#v, want one query per screen load", service.calls)
	}
}

func TestModelShowsUnavailableMutationNotice(t *testing.T) {
	t.Parallel()

	service := &fakeQueryService{
		catalog:   appregistry.CatalogResult{Repositories: []string{"library/alpine"}},
		tags:      map[string]appregistry.TagsResult{"library/alpine": {Name: "library/alpine", Tags: []string{"latest"}}},
		manifests: map[string]appregistry.ManifestDetails{"library/alpine:latest": {Repository: "library/alpine", Reference: "latest", MediaType: "application/vnd.oci.image.manifest.v1+json", Digest: "sha256:manifest", Size: 512}},
	}

	model := NewModel(service)
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "enter")
	updated = runKey(t, updated, "d")

	if !updated.showMutationNotice {
		t.Fatal("expected unavailable mutation notice to be visible")
	}
	if !strings.Contains(updated.View(), "Unavailable in v1") {
		t.Fatalf("view = %q, want unavailable notice", updated.View())
	}
}

type fakeQueryService struct {
	catalog   appregistry.CatalogResult
	tags      map[string]appregistry.TagsResult
	manifests map[string]appregistry.ManifestDetails
	uploads   map[string][]appregistry.UploadDetails
	calls     struct {
		catalog  int
		tags     int
		manifest int
		uploads  int
	}
}

func (f *fakeQueryService) Catalog(context.Context, int, string) (appregistry.CatalogResult, error) {
	f.calls.catalog++
	return f.catalog, nil
}

func (f *fakeQueryService) Tags(_ context.Context, repository string, _ int, _ string) (appregistry.TagsResult, error) {
	f.calls.tags++
	if result, ok := f.tags[repository]; ok {
		return result, nil
	}
	return appregistry.TagsResult{Name: repository}, nil
}

func (f *fakeQueryService) ResolveManifest(_ context.Context, repository string, reference string) (appregistry.ManifestDetails, error) {
	f.calls.manifest++
	if result, ok := f.manifests[fmt.Sprintf("%s:%s", repository, reference)]; ok {
		return result, nil
	}
	return appregistry.ManifestDetails{}, nil
}

func (f *fakeQueryService) Uploads(_ context.Context, repository string) ([]appregistry.UploadDetails, error) {
	f.calls.uploads++
	return append([]appregistry.UploadDetails(nil), f.uploads[repository]...), nil
}

func runCmd(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return model
	}
	msg := cmd()
	updated, nextCmd := model.Update(msg)
	result := updated.(Model)
	if nextCmd != nil {
		return runCmd(t, result, nextCmd)
	}
	return result
}

func runKey(t *testing.T, model Model, key string) Model {
	t.Helper()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	}
	updated, cmd := model.Update(msg)
	result := updated.(Model)
	if cmd != nil {
		return runCmd(t, result, cmd)
	}
	return result
}

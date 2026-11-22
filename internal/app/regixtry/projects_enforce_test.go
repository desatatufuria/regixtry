package regixtry

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

const enforceTenant = "tenant-a"

type enforceEnv struct {
	service  *Service
	projects *ProjectService
	store    *metadata.Store
}

// newEnforceEnv builds a real service over real stores. require turns both the
// fast service check and the authoritative store check on.
func newEnforceEnv(t *testing.T, require bool) enforceEnv {
	t.Helper()
	root := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(root, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	store, err := metadata.New(filepath.Join(root, "registry.db"), metadata.WithRequireProject(require))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	service := NewService(blobs, store, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPush: true}), ports.NewSingleTenantResolver(enforceTenant), ports.NewInlineJobRunner())
	service.SetDeleteEnabled(true)
	service.SetRequireProject(require)
	t.Cleanup(func() {
		service.WaitForBackgroundWork()
		_ = store.Close()
	})
	return enforceEnv{service: service, projects: NewProjectService(service, store), store: store}
}

func manifestJSONFor(digest string, size int) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":%d},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"%s","size":%d}]}`, digest, size, digest, size))
}

// pushImage runs the whole push (start, complete, manifest) and returns the
// first error.
func (e enforceEnv) pushImage(ctx context.Context, repository string, payload string) error {
	upload, err := e.service.BeginUpload(ctx, repository)
	if err != nil {
		return err
	}
	digest := domain.DigestFromBytes([]byte(payload)).String()
	if _, err := e.service.CompleteUpload(ctx, repository, upload.ID, digest, strings.NewReader(payload)); err != nil {
		return err
	}
	_, err = e.service.PublishManifest(ctx, repository, "latest", "application/vnd.oci.image.manifest.v1+json", manifestJSONFor(digest, len(payload)))
	return err
}

func TestRequireProjectOffKeepsTodaysBehavior(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, false)
	ctx := context.Background()

	for _, repository := range []string{"nope/app", "flat", "a/b/c"} {
		if err := env.pushImage(ctx, repository, "payload-"+repository); err != nil {
			t.Fatalf("push to %q with enforcement off error = %v, want nil", repository, err)
		}
	}
}

func TestRequireProjectRefusesMissingProjectAtUploadStart(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)

	_, err := env.service.BeginUpload(context.Background(), "odabank/payments/strike")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("BeginUpload() error = %v, want not found", err)
	}
	want := `project "odabank" does not exist; ask an administrator to create it`
	if got := err.(*domain.Error).Message; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestRequireProjectAuthorizesBeforeCheckingExistence(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	// No anonymous push and no principal: authorization fails first.
	env.service.access = ports.NewConfigurableAccessController(ports.AccessConfig{})

	_, missing := env.service.BeginUpload(context.Background(), "odabank/app")
	if err := env.projects.CreateProject(adminContext(), "odabank"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	_, existing := env.service.BeginUpload(context.Background(), "odabank/app")

	if missing == nil || existing == nil || missing.Error() != existing.Error() {
		t.Fatalf("denials differ: missing project %v, existing project %v", missing, existing)
	}
	if domain.IsCode(missing, domain.ErrorCodeNotFound) {
		t.Fatalf("unauthorized caller learned the project is missing: %v", missing)
	}
}

func TestRequireProjectAllowsPushToExistingProjectOnly(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	ctx := context.Background()
	if err := env.projects.CreateProject(adminContext(), "odabank"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	if err := env.pushImage(ctx, "odabank/payments/strike", "ok"); err != nil {
		t.Fatalf("push into existing project error = %v", err)
	}
	// "odabank" must not cover "odabank2/...".
	if _, err := env.service.BeginUpload(ctx, "odabank2/x"); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("BeginUpload(odabank2/x) error = %v, want not found", err)
	}
}

func TestRequireProjectFlatNamesOnlyWhenRepositoryExists(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	blobs, _ := fsblob.New(filepath.Join(root, "blobs"))
	dbPath := filepath.Join(root, "registry.db")

	// Legacy: a flat repository created while enforcement was off.
	legacyStore, _ := metadata.New(dbPath)
	legacy := NewService(blobs, legacyStore, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPush: true}), ports.NewSingleTenantResolver(enforceTenant), ports.NewInlineJobRunner())
	if err := (enforceEnv{service: legacy, store: legacyStore}).pushImage(context.Background(), "miapp", "legacy"); err != nil {
		t.Fatalf("legacy push error = %v", err)
	}
	legacy.WaitForBackgroundWork()
	_ = legacyStore.Close()

	store, err := metadata.New(dbPath, metadata.WithRequireProject(true))
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer store.Close()
	service := NewService(blobs, store, ports.NewConfigurableAccessController(ports.AccessConfig{AllowAnonymousPush: true}), ports.NewSingleTenantResolver(enforceTenant), ports.NewInlineJobRunner())
	service.SetRequireProject(true)
	defer service.WaitForBackgroundWork()
	env := enforceEnv{service: service, store: store}
	ctx := context.Background()

	if err := env.pushImage(ctx, "miapp", "legacy-again"); err != nil {
		t.Fatalf("push to existing flat repository error = %v, want nil", err)
	}
	_, err = service.BeginUpload(ctx, "otraapp")
	if !domain.IsCode(err, domain.ErrorCodeInvalidRepository) {
		t.Fatalf("BeginUpload(new flat) error = %v, want invalid repository", err)
	}
	want := `repository "otraapp" has no project; use <project>/otraapp (flat repository names are no longer created)`
	if got := err.(*domain.Error).Message; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

// An upload opened while enforcement was off is refused at the authoritative
// store check when its project does not exist (blob link and manifest).
func TestRequireProjectStoreChecksAreAuthoritative(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	ctx := context.Background()

	repository := domain.RepositoryRef{Name: "ghost/app"}
	digest := domain.DigestFromBytes([]byte("x"))
	if err := env.store.LinkBlob(ctx, enforceTenant, repository, digest); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("LinkBlob() error = %v, want not found", err)
	}
	if exists, _ := env.store.RepositoryExists(ctx, enforceTenant, "ghost/app"); exists {
		t.Fatalf("repository row created for a missing project")
	}
	if linked, _ := env.store.IsBlobLinked(ctx, enforceTenant, repository, digest); linked {
		t.Fatalf("blob linked for a missing project")
	}
}

func TestDeleteProjectIsRefusedWhileAnUploadIsOpenAndAfterItsRepositoryExists(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	ctx := context.Background()
	if err := env.projects.CreateProject(adminContext(), "odabank"); err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}

	upload, err := env.service.BeginUpload(ctx, "odabank/app")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if err := env.projects.DeleteProject(adminContext(), "odabank"); !domain.IsCode(err, domain.ErrorCodeConflict) {
		t.Fatalf("DeleteProject() with open upload error = %v, want conflict", err)
	}

	payload := "layer"
	digest := domain.DigestFromBytes([]byte(payload)).String()
	if _, err := env.service.CompleteUpload(ctx, "odabank/app", upload.ID, digest, strings.NewReader(payload)); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	if _, err := env.service.PublishManifest(ctx, "odabank/app", "latest", "application/vnd.oci.image.manifest.v1+json", manifestJSONFor(digest, len(payload))); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	if err := env.projects.DeleteProject(adminContext(), "odabank"); !domain.IsCode(err, domain.ErrorCodeConflict) {
		t.Fatalf("DeleteProject() with repository error = %v, want conflict", err)
	}
}

// Parallel pushes against project deletes: either the push completes and the
// project survives, or the delete wins and the push is refused; never a
// repository or blob link without its project.
func TestRequireProjectPushRacingProjectDelete(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		project := fmt.Sprintf("p%d", i)
		repository := project + "/app"
		if err := env.projects.CreateProject(adminContext(), project); err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}

		var wg sync.WaitGroup
		var pushErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			pushErr = env.pushImage(ctx, repository, "payload-"+project)
		}()
		go func() {
			defer wg.Done()
			_ = env.projects.DeleteProject(adminContext(), project)
		}()
		wg.Wait()

		projectExists, err := env.projects.ProjectExists(adminContext(), project)
		if err != nil {
			t.Fatalf("ProjectExists() error = %v", err)
		}
		repoExists, _ := env.store.RepositoryExists(ctx, enforceTenant, repository)
		digest, _ := domain.ParseDigest(domain.DigestFromBytes([]byte("payload-" + project)).String())
		linked, _ := env.store.IsBlobLinked(ctx, enforceTenant, domain.RepositoryRef{Name: repository}, digest)

		if !projectExists && (repoExists || linked) {
			t.Fatalf("iteration %d: project gone but repository=%v blobLinked=%v remain", i, repoExists, linked)
		}
		if pushErr == nil && !projectExists {
			t.Fatalf("iteration %d: push succeeded but the project was deleted", i)
		}
		if pushErr != nil && !domain.IsCode(pushErr, domain.ErrorCodeNotFound) && !domain.IsCode(pushErr, domain.ErrorCodeConflict) {
			t.Fatalf("iteration %d: push error = %v, want nil or not found", i, pushErr)
		}
	}
}

func countUploadsOfMissingProjects(t *testing.T, env enforceEnv, projects []string) int {
	t.Helper()
	open, err := env.store.ListUploads(context.Background(), enforceTenant, nil)
	if err != nil {
		t.Fatalf("ListUploads() error = %v", err)
	}
	orphans := 0
	for _, upload := range open {
		project, _ := domain.ProjectOf(upload.Repository)
		if exists, _ := env.store.ProjectExists(context.Background(), enforceTenant, project); !exists {
			orphans++
		}
	}
	return orphans
}

func TestRequireProjectSaveUploadIsRefusedForAMissingProject(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	repository, _ := domain.ParseRepositoryRef("ghost/app")
	state := domain.UploadState{ID: "u1", Repository: repository, Status: domain.UploadStatusActive, StartedAt: time.Now(), UpdatedAt: time.Now(), Location: "/x"}

	if err := env.store.SaveUpload(context.Background(), enforceTenant, state); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("SaveUpload() error = %v, want not found", err)
	}
	if open, _ := env.store.ListUploads(context.Background(), enforceTenant, nil); len(open) != 0 {
		t.Fatalf("%d upload rows saved for a missing project", len(open))
	}
}

// Parallel BeginUpload against project delete/create cycles: no upload row of
// a missing project survives, and DeleteProject is never blocked by one.
func TestRequireProjectBeginUploadRacingProjectCycles(t *testing.T) {
	t.Parallel()
	env := newEnforceEnv(t, true)
	ctx := context.Background()

	for round := 0; round < 25; round++ {
		project := fmt.Sprintf("c%d", round)
		if err := env.projects.CreateProject(adminContext(), project); err != nil {
			t.Fatalf("CreateProject() error = %v", err)
		}
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = env.service.BeginUpload(ctx, project+"/app")
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = env.projects.DeleteProject(adminContext(), project)
		}()
		wg.Wait()

		if n := countUploadsOfMissingProjects(t, env, nil); n != 0 {
			t.Fatalf("round %d: %d upload rows of a missing project", round, n)
		}
		// Whatever survived must be deletable once its uploads are cancelled.
		open, _ := env.store.ListUploads(ctx, enforceTenant, nil)
		for _, upload := range open {
			_ = env.service.CancelUpload(ctx, upload.Repository.String(), upload.ID)
		}
		if exists, _ := env.projects.ProjectExists(adminContext(), project); exists {
			if err := env.projects.DeleteProject(adminContext(), project); err != nil {
				t.Fatalf("round %d: DeleteProject() blocked: %v", round, err)
			}
		}
	}
}

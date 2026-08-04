package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appregistry "registry/internal/app/registry"
	domainauth "registry/internal/domain/auth"
	registrydomain "registry/internal/domain/registry"
	"registry/internal/ports"
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

func TestModelAssignsGrantViaAdminWorkflow(t *testing.T) {
	t.Parallel()

	authService := newFakeAdminAuthService()
	dev := authService.addUser("user-1", "developer", false, true, "change-me-now")

	model := NewModel(&fakeQueryService{catalog: appregistry.CatalogResult{Repositories: []string{"team/app"}}}, WithAuthAdministration(authService, domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}))
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "a")
	if updated.adminUsers.Items[updated.adminUsers.Selected].ID != dev.ID {
		t.Fatalf("selected user = %q, want %q", updated.adminUsers.Items[updated.adminUsers.Selected].ID, dev.ID)
	}
	updated = runKey(t, updated, "g")
	updated = runKey(t, updated, "n")
	updated = runText(t, updated, "team/app")
	updated = runKey(t, updated, "tab")
	updated = runBackspace(t, updated, len(string(domainauth.RepoRoleReader)))
	updated = runText(t, updated, "repo-writer")
	updated = runKey(t, updated, "enter")

	grants := authService.grants[dev.ID]
	if len(grants) != 1 {
		t.Fatalf("len(grants) = %d, want 1", len(grants))
	}
	if got := grants[0].Repository.String(); got != "team/app" {
		t.Fatalf("grant repository = %q, want team/app", got)
	}
	if grants[0].Role != domainauth.RepoRoleWriter {
		t.Fatalf("grant role = %q, want %q", grants[0].Role, domainauth.RepoRoleWriter)
	}
	if updated.screen != screenAdminGrants {
		t.Fatalf("screen = %q, want %q", updated.screen, screenAdminGrants)
	}
	view := updated.View()
	if !strings.Contains(view, "team/app") || !strings.Contains(view, "repo-writer") {
		t.Fatalf("view = %q, want assigned grant details", view)
	}
}

func TestModelResetsPasswordViaAdminWorkflow(t *testing.T) {
	t.Parallel()

	authService := newFakeAdminAuthService()
	dev := authService.addUser("user-1", "developer", false, true, "old-password")

	model := NewModel(&fakeQueryService{catalog: appregistry.CatalogResult{Repositories: []string{"team/app"}}}, WithAuthAdministration(authService, domainauth.Principal{UserID: "admin-1", Username: "admin", IsAdmin: true}))
	updated := runCmd(t, model, model.Init())
	updated = runKey(t, updated, "a")
	updated = runKey(t, updated, "p")
	updated = runText(t, updated, "new-password")
	updated = runKey(t, updated, "enter")

	if got := authService.passwords[dev.ID]; got != "new-password" {
		t.Fatalf("password = %q, want %q", got, "new-password")
	}
	if !strings.Contains(updated.View(), "Password reset for developer.") {
		t.Fatalf("view = %q, want password reset status", updated.View())
	}
}

func TestModelRejectsNonAdminTokenManagementPaths(t *testing.T) {
	t.Parallel()

	authService := newFakeAdminAuthService()
	dev := authService.addUser("user-1", "developer", false, true, "change-me-now")
	token := authService.addToken(dev.ID, "ci")

	actor := domainauth.Principal{UserID: dev.ID, Username: dev.Username, IsAdmin: false}
	base := NewModel(&fakeQueryService{}, WithAuthAdministration(authService, actor))

	t.Run("list tokens screen rejects non-admin actor", func(t *testing.T) {
		model := base
		model.screen = screenAdminUsers
		model.adminUsers.Items = []domainauth.User{dev}
		updated := runKey(t, model, "o")
		if updated.screen != screenError {
			t.Fatalf("screen = %q, want %q", updated.screen, screenError)
		}
		if !domainauth.IsCode(updated.err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("error = %v, want forbidden", updated.err)
		}
	})

	t.Run("revoke token rejects non-admin actor", func(t *testing.T) {
		model := base
		model.screen = screenAdminTokens
		model.adminTokens = AdminTokensModel{User: dev, Items: []domainauth.Token{token}}
		updated := runKey(t, model, "x")
		if updated.screen != screenError {
			t.Fatalf("screen = %q, want %q", updated.screen, screenError)
		}
		if !domainauth.IsCode(updated.err, domainauth.ErrorCodeForbidden) {
			t.Fatalf("error = %v, want forbidden", updated.err)
		}
	})
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

type fakeAdminAuthService struct {
	users     []domainauth.User
	passwords map[string]string
	grants    map[string][]domainauth.RepoGrant
	tokens    map[string][]domainauth.Token
	now       time.Time
}

func newFakeAdminAuthService() *fakeAdminAuthService {
	return &fakeAdminAuthService{
		passwords: make(map[string]string),
		grants:    make(map[string][]domainauth.RepoGrant),
		tokens:    make(map[string][]domainauth.Token),
		now:       time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC),
	}
}

func (f *fakeAdminAuthService) addUser(id, username string, isAdmin, enabled bool, password string) domainauth.User {
	user := domainauth.User{ID: id, Username: username, PasswordHash: "hash", IsAdmin: isAdmin, Enabled: enabled, CreatedAt: f.now, UpdatedAt: f.now}
	f.users = append(f.users, user)
	f.passwords[id] = password
	f.sortUsers()
	return user
}

func (f *fakeAdminAuthService) addToken(userID, name string) domainauth.Token {
	token := domainauth.Token{ID: "token-1", UserID: userID, Kind: domainauth.TokenKindAdminCredential, Name: name, Accessor: "act_token1", SecretHash: "secret", CreatedAt: f.now, ExpiresAt: f.now.Add(24 * time.Hour)}
	f.tokens[userID] = append(f.tokens[userID], token)
	return token
}

func (f *fakeAdminAuthService) EnsureBootstrapAdmin(context.Context) error { return nil }
func (f *fakeAdminAuthService) BootstrapAdmin(context.Context, ports.BootstrapAdminInput) (ports.BootstrapAdminResult, error) {
	return ports.BootstrapAdminResult{}, nil
}
func (f *fakeAdminAuthService) ListUsers(_ context.Context, actor domainauth.Principal) ([]domainauth.User, error) {
	if err := requireAdminTest(actor); err != nil {
		return nil, err
	}
	users := append([]domainauth.User(nil), f.users...)
	return users, nil
}
func (f *fakeAdminAuthService) CreateUser(_ context.Context, actor domainauth.Principal, input ports.CreateUserInput) (domainauth.User, error) {
	if err := requireAdminTest(actor); err != nil {
		return domainauth.User{}, err
	}
	user := f.addUser(fmt.Sprintf("user-%d", len(f.users)+1), strings.TrimSpace(input.Username), input.IsAdmin, input.Enabled, input.Password)
	return user, nil
}
func (f *fakeAdminAuthService) UpdateUser(_ context.Context, actor domainauth.Principal, input ports.UpdateUserInput) (domainauth.User, error) {
	if err := requireAdminTest(actor); err != nil {
		return domainauth.User{}, err
	}
	for index := range f.users {
		if f.users[index].ID == input.UserID {
			f.users[index].Username = strings.TrimSpace(input.Username)
			f.users[index].IsAdmin = input.IsAdmin
			f.sortUsers()
			return f.mustUser(input.UserID), nil
		}
	}
	return domainauth.User{}, domainauth.NewNotFoundError("user", input.UserID)
}
func (f *fakeAdminAuthService) SetUserEnabled(_ context.Context, actor domainauth.Principal, userID string, enabled bool) (domainauth.User, error) {
	if err := requireAdminTest(actor); err != nil {
		return domainauth.User{}, err
	}
	for index := range f.users {
		if f.users[index].ID == userID {
			f.users[index].Enabled = enabled
			return f.users[index], nil
		}
	}
	return domainauth.User{}, domainauth.NewNotFoundError("user", userID)
}
func (f *fakeAdminAuthService) DeleteUser(_ context.Context, actor domainauth.Principal, userID string) error {
	if err := requireAdminTest(actor); err != nil {
		return err
	}
	for index := range f.users {
		if f.users[index].ID == userID {
			f.users = append(f.users[:index], f.users[index+1:]...)
			delete(f.passwords, userID)
			delete(f.grants, userID)
			delete(f.tokens, userID)
			return nil
		}
	}
	return domainauth.NewNotFoundError("user", userID)
}
func (f *fakeAdminAuthService) LoginWithPassword(context.Context, string, string) (ports.LoginResult, error) {
	return ports.LoginResult{}, nil
}
func (f *fakeAdminAuthService) LoginWithPreissuedToken(context.Context, string, string) (ports.LoginResult, error) {
	return ports.LoginResult{}, nil
}
func (f *fakeAdminAuthService) VerifyAccessToken(context.Context, string) (domainauth.Principal, error) {
	return domainauth.Principal{}, nil
}
func (f *fakeAdminAuthService) CreateAdminToken(_ context.Context, actor domainauth.Principal, input ports.CreateAdminTokenInput) (ports.CreatedAdminToken, error) {
	if err := requireAdminTest(actor); err != nil {
		return ports.CreatedAdminToken{}, err
	}
	token := domainauth.Token{ID: fmt.Sprintf("token-%d", len(f.tokens[input.UserID])+1), UserID: input.UserID, Kind: domainauth.TokenKindAdminCredential, Name: input.Name, Accessor: fmt.Sprintf("act_%d", len(f.tokens[input.UserID])+1), SecretHash: "secret", CreatedAt: f.now, ExpiresAt: f.now.Add(24 * time.Hour)}
	f.tokens[input.UserID] = append(f.tokens[input.UserID], token)
	return ports.CreatedAdminToken{Token: token, Accessor: token.Accessor, Plaintext: "plain-secret", TargetUser: f.mustUser(input.UserID)}, nil
}
func (f *fakeAdminAuthService) ListRepoGrants(_ context.Context, actor domainauth.Principal, userID string) ([]domainauth.RepoGrant, error) {
	if err := requireAdminTest(actor); err != nil {
		return nil, err
	}
	grants := append([]domainauth.RepoGrant(nil), f.grants[userID]...)
	sort.Slice(grants, func(i, j int) bool { return grants[i].Repository.String() < grants[j].Repository.String() })
	return grants, nil
}
func (f *fakeAdminAuthService) ListAdminTokens(_ context.Context, actor domainauth.Principal, userID string) ([]domainauth.Token, error) {
	if err := requireAdminTest(actor); err != nil {
		return nil, err
	}
	return append([]domainauth.Token(nil), f.tokens[userID]...), nil
}
func (f *fakeAdminAuthService) RevokeAdminToken(_ context.Context, actor domainauth.Principal, accessor string) error {
	if err := requireAdminTest(actor); err != nil {
		return err
	}
	for userID, tokens := range f.tokens {
		for index := range tokens {
			if tokens[index].Accessor == accessor {
				revokedAt := f.now
				tokens[index].RevokedAt = &revokedAt
				f.tokens[userID] = tokens
				return nil
			}
		}
	}
	return domainauth.NewNotFoundError("token", accessor)
}
func (f *fakeAdminAuthService) ResetPassword(_ context.Context, actor domainauth.Principal, userID string, newPassword string) error {
	if err := requireAdminTest(actor); err != nil {
		return err
	}
	f.passwords[userID] = newPassword
	return nil
}
func (f *fakeAdminAuthService) PutRepoGrant(_ context.Context, actor domainauth.Principal, userID string, repository string, role domainauth.RepoRole) (domainauth.RepoGrant, error) {
	if err := requireAdminTest(actor); err != nil {
		return domainauth.RepoGrant{}, err
	}
	grant := domainauth.RepoGrant{UserID: userID, Repository: registrydomain.MustParseRepositoryRef(repository), Role: role, CreatedAt: f.now, UpdatedAt: f.now}
	grants := f.grants[userID]
	for index := range grants {
		if grants[index].Repository.String() == repository {
			grants[index] = grant
			f.grants[userID] = grants
			return grant, nil
		}
	}
	f.grants[userID] = append(grants, grant)
	return grant, nil
}
func (f *fakeAdminAuthService) DeleteRepoGrant(_ context.Context, actor domainauth.Principal, userID string, repository string) error {
	if err := requireAdminTest(actor); err != nil {
		return err
	}
	grants := f.grants[userID]
	for index := range grants {
		if grants[index].Repository.String() == repository {
			f.grants[userID] = append(grants[:index], grants[index+1:]...)
			return nil
		}
	}
	return domainauth.NewNotFoundError("grant", repository)
}

func (f *fakeAdminAuthService) mustUser(userID string) domainauth.User {
	for _, user := range f.users {
		if user.ID == userID {
			return user
		}
	}
	panic("user not found")
}

func (f *fakeAdminAuthService) sortUsers() {
	sort.Slice(f.users, func(i, j int) bool { return f.users[i].Username < f.users[j].Username })
}

func requireAdminTest(actor domainauth.Principal) error {
	if !actor.IsAdmin {
		return domainauth.NewForbiddenError("administrator privileges are required")
	}
	return nil
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

func runText(t *testing.T, model Model, value string) Model {
	t.Helper()
	updated := model
	for _, r := range value {
		updated = runKey(t, updated, string(r))
	}
	return updated
}

func runBackspace(t *testing.T, model Model, count int) Model {
	t.Helper()
	updated := model
	for i := 0; i < count; i++ {
		updated = runKey(t, updated, "backspace")
	}
	return updated
}

package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appregistry "registry/internal/app/registry"
	domainauth "registry/internal/domain/auth"
	"registry/internal/ports"
)

type QueryService interface {
	Catalog(ctx context.Context, limit int, after string) (appregistry.CatalogResult, error)
	Tags(ctx context.Context, repositoryName string, limit int, after string) (appregistry.TagsResult, error)
	ResolveManifest(ctx context.Context, repositoryName string, reference string) (appregistry.ManifestDetails, error)
	Uploads(ctx context.Context, repositoryName string) ([]appregistry.UploadDetails, error)
}

type RepositoriesModel struct {
	Items    []string
	Selected int
}

type TagsModel struct {
	Repository string
	Items      []string
	Selected   int
}

type ManifestModel struct {
	Details appregistry.ManifestDetails
}

type BlobsModel struct {
	Items    []appregistry.BlobDetails
	Selected int
}

type UploadsModel struct {
	Repository string
	Items      []appregistry.UploadDetails
}

type EmptyStateModel struct {
	Title   string
	Message string
}

type MutationUnavailableModel struct {
	Action string
	Reason string
}

type screen string

const (
	screenLoading      screen = "loading"
	screenRepositories screen = "repositories"
	screenTags         screen = "tags"
	screenManifest     screen = "manifest"
	screenBlobs        screen = "blobs"
	screenUploads      screen = "uploads"
	screenAdminUsers   screen = "admin-users"
	screenAdminGrants  screen = "admin-grants"
	screenAdminTokens  screen = "admin-tokens"
	screenEmpty        screen = "empty"
	screenError        screen = "error"
)

type Model struct {
	ctx         context.Context
	service     QueryService
	authService ports.AuthService
	actor       domainauth.Principal
	screen      screen
	loadingText string
	err         error

	repositories RepositoriesModel
	tags         TagsModel
	manifest     ManifestModel
	blobs        BlobsModel
	uploads      UploadsModel
	adminUsers   AdminUsersModel
	adminGrants  AdminGrantsModel
	adminTokens  AdminTokensModel
	empty        EmptyStateModel
	mutation     MutationUnavailableModel
	form         *adminFormModel
	status       string

	showMutationNotice bool
	lastRepository     string
	lastTag            string
}

type catalogLoadedMsg struct {
	result appregistry.CatalogResult
	err    error
}

type tagsLoadedMsg struct {
	repository string
	result     appregistry.TagsResult
	err        error
}

type manifestLoadedMsg struct {
	repository string
	tag        string
	manifest   appregistry.ManifestDetails
	uploads    []appregistry.UploadDetails
	err        error
}

type adminUsersLoadedMsg struct {
	users []domainauth.User
	err   error
}

type adminGrantsLoadedMsg struct {
	user   domainauth.User
	grants []domainauth.RepoGrant
	err    error
}

type adminTokensLoadedMsg struct {
	user   domainauth.User
	tokens []domainauth.Token
	err    error
}

type adminUserMutationMsg struct {
	users          []domainauth.User
	selectedUserID string
	status         string
	err            error
}

type adminGrantMutationMsg struct {
	user               domainauth.User
	grants             []domainauth.RepoGrant
	selectedRepository string
	status             string
	err                error
}

type adminTokenMutationMsg struct {
	user             domainauth.User
	tokens           []domainauth.Token
	selectedAccessor string
	issuedSecret     string
	status           string
	err              error
}

func NewModel(service QueryService, options ...Option) Model {
	m := Model{
		ctx:         context.Background(),
		service:     service,
		screen:      screenLoading,
		loadingText: "Loading repositories...",
		empty: EmptyStateModel{
			Title:   "Registry is empty",
			Message: "No repositories have been published yet.",
		},
		mutation: MutationUnavailableModel{
			Action: "delete",
			Reason: "Mutations such as deletion and retention controls are unavailable in v1.",
		},
	}
	for _, option := range options {
		option(&m)
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return m.loadCatalogCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.form != nil {
			return m.updateFormKey(msg)
		}
		if handled, next, cmd := m.updateAdminKey(msg); handled {
			return next, cmd
		}
		return m.updateKey(msg)
	case catalogLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}

		m.repositories = RepositoriesModel{Items: append([]string(nil), msg.result.Repositories...)}
		if len(m.repositories.Items) == 0 {
			m.screen = screenEmpty
			return m, nil
		}

		m.screen = screenRepositories
		m.loadingText = ""
		return m, nil
	case tagsLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}

		m.tags = TagsModel{Repository: msg.repository, Items: append([]string(nil), msg.result.Tags...)}
		m.lastRepository = msg.repository
		m.showMutationNotice = false
		m.status = ""
		if len(m.tags.Items) == 0 {
			m.screen = screenEmpty
			m.empty = EmptyStateModel{
				Title:   "Repository has no tags",
				Message: fmt.Sprintf("%s has no published tags yet.", msg.repository),
			}
			return m, nil
		}

		m.screen = screenTags
		return m, nil
	case manifestLoadedMsg:
		if msg.err != nil {
			m.screen = screenError
			m.err = msg.err
			return m, nil
		}

		m.lastRepository = msg.repository
		m.lastTag = msg.tag
		m.manifest = ManifestModel{Details: msg.manifest}
		m.blobs = BlobsModel{Items: append([]appregistry.BlobDetails(nil), msg.manifest.Blobs...)}
		sort.Slice(msg.uploads, func(i, j int) bool {
			return msg.uploads[i].StartedAt.Before(msg.uploads[j].StartedAt)
		})
		m.uploads = UploadsModel{Repository: msg.repository, Items: append([]appregistry.UploadDetails(nil), msg.uploads...)}
		m.showMutationNotice = false
		m.status = ""
		m.screen = screenManifest
		return m, nil
	case adminUsersLoadedMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.adminUsers.Items = append([]domainauth.User(nil), msg.users...)
		m.adminUsers.Selected = boundedIndex(m.adminUsers.Selected, len(m.adminUsers.Items))
		m.screen = screenAdminUsers
		m.loadingText = ""
		m.status = ""
		return m, nil
	case adminGrantsLoadedMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.adminGrants.User = msg.user
		m.adminGrants.Items = append([]domainauth.RepoGrant(nil), msg.grants...)
		m.adminGrants.Selected = boundedIndex(m.adminGrants.Selected, len(m.adminGrants.Items))
		m.screen = screenAdminGrants
		m.loadingText = ""
		m.status = ""
		return m, nil
	case adminTokensLoadedMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.adminTokens.User = msg.user
		m.adminTokens.Items = append([]domainauth.Token(nil), msg.tokens...)
		m.adminTokens.Selected = boundedIndex(m.adminTokens.Selected, len(m.adminTokens.Items))
		m.adminTokens.IssuedSecret = ""
		m.screen = screenAdminTokens
		m.loadingText = ""
		m.status = ""
		return m, nil
	case adminUserMutationMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.form = nil
		m.adminUsers.Items = append([]domainauth.User(nil), msg.users...)
		m.adminUsers.Selected = selectedUserIndex(m.adminUsers.Items, msg.selectedUserID)
		m.status = msg.status
		m.screen = screenAdminUsers
		return m, nil
	case adminGrantMutationMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.form = nil
		m.adminGrants.User = msg.user
		m.adminGrants.Items = append([]domainauth.RepoGrant(nil), msg.grants...)
		m.adminGrants.Selected = selectedGrantIndex(m.adminGrants.Items, msg.selectedRepository)
		m.status = msg.status
		m.screen = screenAdminGrants
		return m, nil
	case adminTokenMutationMsg:
		if msg.err != nil {
			return m.fail(msg.err), nil
		}
		m.form = nil
		m.adminTokens.User = msg.user
		m.adminTokens.Items = append([]domainauth.Token(nil), msg.tokens...)
		m.adminTokens.Selected = selectedTokenIndex(m.adminTokens.Items, msg.selectedAccessor)
		m.adminTokens.IssuedSecret = msg.issuedSecret
		m.status = msg.status
		m.screen = screenAdminTokens
		return m, nil
	}

	return m, nil
}

func (m Model) View() string {
	var body strings.Builder
	body.WriteString("Registry Console\n\n")

	switch m.screen {
	case screenLoading:
		body.WriteString(m.loadingText)
	case screenRepositories:
		body.WriteString("Repositories\n")
		body.WriteString(renderList(m.repositories.Items, m.repositories.Selected))
		body.WriteString("\n\n")
		body.WriteString("Enter: open tags")
		if m.authService != nil {
			body.WriteString(" · a: admin")
		}
		body.WriteString(" · q: quit")
	case screenTags:
		body.WriteString(fmt.Sprintf("Tags · %s\n", m.tags.Repository))
		body.WriteString(renderList(m.tags.Items, m.tags.Selected))
		body.WriteString("\n\nEnter: inspect manifest · esc: back · q: quit")
	case screenManifest:
		body.WriteString(renderManifest(m.manifest.Details))
		body.WriteString("\n\n")
		body.WriteString("b: blobs · u: uploads · d: unsupported delete · esc: back · q: quit")
	case screenBlobs:
		body.WriteString(renderBlobs(m.blobs))
		body.WriteString("\n\nesc: back · q: quit")
	case screenUploads:
		body.WriteString(renderUploads(m.uploads))
		body.WriteString("\n\nesc: back · q: quit")
	case screenAdminUsers:
		body.WriteString(renderAdminUsers(m.adminUsers))
		body.WriteString("\n\nn: create · e: edit · !: enable/disable · p: reset password · g: grants · o: tokens · x: delete · r: refresh · esc: back")
	case screenAdminGrants:
		body.WriteString(renderAdminGrants(m.adminGrants))
		body.WriteString("\n\nn: assign grant · x: remove grant · esc: back")
	case screenAdminTokens:
		body.WriteString(renderAdminTokens(m.adminTokens))
		body.WriteString("\n\nn: create token · x: revoke token · esc: back")
	case screenEmpty:
		body.WriteString(m.empty.Title)
		body.WriteString("\n")
		body.WriteString(m.empty.Message)
		body.WriteString("\n\nesc: back · q: quit")
	case screenError:
		body.WriteString("Error\n")
		body.WriteString(m.err.Error())
		body.WriteString("\n\nq: quit")
	}

	if m.showMutationNotice {
		body.WriteString("\n\nUnavailable in v1\n")
		body.WriteString(fmt.Sprintf("%s: %s", strings.Title(m.mutation.Action), m.mutation.Reason))
	}
	if m.status != "" {
		body.WriteString("\n\nStatus\n")
		body.WriteString(m.status)
	}
	if m.form != nil {
		body.WriteString("\n\n")
		body.WriteString(renderAdminForm(*m.form))
	}

	return body.String()
}

func (m Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		m.moveSelection(-1)
		return m, nil
	case "down", "j":
		m.moveSelection(1)
		return m, nil
	case "enter":
		switch m.screen {
		case screenRepositories:
			if repository, ok := m.selectedRepository(); ok {
				m.screen = screenLoading
				m.loadingText = fmt.Sprintf("Loading tags for %s...", repository)
				return m, m.loadTagsCmd(repository)
			}
		case screenTags:
			if tag, ok := m.selectedTag(); ok {
				m.screen = screenLoading
				m.loadingText = fmt.Sprintf("Loading manifest %s:%s...", m.tags.Repository, tag)
				return m, m.loadManifestCmd(m.tags.Repository, tag)
			}
		}
	case "esc", "backspace":
		m.showMutationNotice = false
		m.status = ""
		switch m.screen {
		case screenTags, screenEmpty:
			if m.lastRepository != "" {
				m.screen = screenRepositories
				return m, nil
			}
		case screenManifest:
			m.screen = screenTags
			return m, nil
		case screenBlobs, screenUploads:
			m.screen = screenManifest
			return m, nil
		}
	case "b":
		if m.screen == screenManifest {
			m.screen = screenBlobs
			m.showMutationNotice = false
		}
		return m, nil
	case "u":
		if m.screen == screenManifest {
			m.screen = screenUploads
			m.showMutationNotice = false
		}
		return m, nil
	case "d", "x":
		if m.screen == screenManifest || m.screen == screenBlobs || m.screen == screenUploads {
			m.showMutationNotice = true
		}
		return m, nil
	}

	return m, nil
}

func (m Model) updateFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.form = nil
		m.status = ""
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		m.form.selected = boundedIndex(m.form.selected+1, len(m.form.fields))
		return m, nil
	case tea.KeyShiftTab, tea.KeyUp:
		m.form.selected = boundedIndex(m.form.selected-1, len(m.form.fields))
		return m, nil
	case tea.KeyBackspace:
		field := &m.form.fields[m.form.selected]
		if len(field.Value) > 0 {
			field.Value = field.Value[:len(field.Value)-1]
		}
		return m, nil
	case tea.KeyEnter:
		m.status = "Submitting admin action..."
		return m, m.submitAdminFormCmd()
	default:
		if len(msg.Runes) > 0 {
			m.form.fields[m.form.selected].Value += string(msg.Runes)
		}
		return m, nil
	}
}

func (m Model) moveSelection(delta int) {
	switch m.screen {
	case screenRepositories:
		m.repositories.Selected = boundedIndex(m.repositories.Selected+delta, len(m.repositories.Items))
	case screenTags:
		m.tags.Selected = boundedIndex(m.tags.Selected+delta, len(m.tags.Items))
	case screenBlobs:
		m.blobs.Selected = boundedIndex(m.blobs.Selected+delta, len(m.blobs.Items))
	case screenAdminUsers:
		m.adminUsers.Selected = boundedIndex(m.adminUsers.Selected+delta, len(m.adminUsers.Items))
	case screenAdminGrants:
		m.adminGrants.Selected = boundedIndex(m.adminGrants.Selected+delta, len(m.adminGrants.Items))
	case screenAdminTokens:
		m.adminTokens.Selected = boundedIndex(m.adminTokens.Selected+delta, len(m.adminTokens.Items))
	}
}

func boundedIndex(index int, size int) int {
	if size == 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= size {
		return size - 1
	}
	return index
}

func (m Model) selectedRepository() (string, bool) {
	if len(m.repositories.Items) == 0 {
		return "", false
	}
	return m.repositories.Items[m.repositories.Selected], true
}

func (m Model) selectedTag() (string, bool) {
	if len(m.tags.Items) == 0 {
		return "", false
	}
	return m.tags.Items[m.tags.Selected], true
}

func (m Model) loadCatalogCmd() tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.Catalog(m.ctx, 100, "")
		return catalogLoadedMsg{result: result, err: err}
	}
}

func (m Model) loadTagsCmd(repository string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.service.Tags(m.ctx, repository, 100, "")
		return tagsLoadedMsg{repository: repository, result: result, err: err}
	}
}

func (m Model) loadManifestCmd(repository string, tag string) tea.Cmd {
	return func() tea.Msg {
		manifest, err := m.service.ResolveManifest(m.ctx, repository, tag)
		if err != nil {
			return manifestLoadedMsg{repository: repository, tag: tag, err: err}
		}

		uploads, uploadsErr := m.service.Uploads(m.ctx, repository)
		return manifestLoadedMsg{repository: repository, tag: tag, manifest: manifest, uploads: uploads, err: uploadsErr}
	}
}

func (m Model) fail(err error) Model {
	m.form = nil
	m.screen = screenError
	m.err = err
	m.status = ""
	return m
}

func (m Model) submitAdminFormCmd() tea.Cmd {
	if m.form == nil {
		return nil
	}
	form := *m.form
	return func() tea.Msg {
		switch form.kind {
		case adminFormCreateGrant:
			return submitGrantForm(m.ctx, m, form)
		case adminFormCreateToken:
			return submitTokenForm(m.ctx, m, form)
		default:
			return submitUserForm(m.ctx, m, form)
		}
	}
}

func renderList(items []string, selected int) string {
	if len(items) == 0 {
		return "- none -"
	}

	lines := make([]string, 0, len(items))
	for index, item := range items {
		prefix := "  "
		if index == selected {
			prefix = "> "
		}
		lines = append(lines, prefix+item)
	}
	return strings.Join(lines, "\n")
}

func renderManifest(manifest appregistry.ManifestDetails) string {
	lines := []string{
		fmt.Sprintf("Manifest · %s:%s", manifest.Repository, manifest.Reference),
		fmt.Sprintf("Digest: %s", manifest.Digest),
		fmt.Sprintf("Media Type: %s", manifest.MediaType),
		fmt.Sprintf("Size: %d bytes", manifest.Size),
		fmt.Sprintf("Blobs: %d", len(manifest.Blobs)),
	}
	if len(manifest.Annotations) > 0 {
		keys := make([]string, 0, len(manifest.Annotations))
		for key := range manifest.Annotations {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("Annotation %s=%s", key, manifest.Annotations[key]))
		}
	}
	return strings.Join(lines, "\n")
}

func renderBlobs(blobs BlobsModel) string {
	lines := []string{"Blobs"}
	if len(blobs.Items) == 0 {
		return strings.Join(append(lines, "No blobs are linked to the selected manifest."), "\n")
	}
	for index, blob := range blobs.Items {
		prefix := "  "
		if index == blobs.Selected {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%s (%d bytes) [%s]", prefix, blob.Digest, blob.Size, blob.MediaType))
	}
	return strings.Join(lines, "\n")
}

func renderUploads(uploads UploadsModel) string {
	lines := []string{fmt.Sprintf("Uploads · %s", uploads.Repository)}
	if len(uploads.Items) == 0 {
		return strings.Join(append(lines, "No in-progress uploads for this repository."), "\n")
	}
	for _, upload := range uploads.Items {
		lines = append(lines, fmt.Sprintf("- %s · %s · %d bytes", upload.ID, upload.Status, upload.Size))
	}
	return strings.Join(lines, "\n")
}

func parseBoolInput(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes", "true", "1", "admin":
		return true
	default:
		return false
	}
}

func parseTTLHours(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	hours, err := time.ParseDuration(trimmed + "h")
	if err != nil {
		return 0, domainauth.NewValidationError("token ttl hours must be a whole number")
	}
	return hours, nil
}

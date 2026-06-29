package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	appregistry "registry/internal/app/registry"
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
	screenEmpty        screen = "empty"
	screenError        screen = "error"
)

type Model struct {
	ctx         context.Context
	service     QueryService
	screen      screen
	loadingText string
	err         error

	repositories RepositoriesModel
	tags         TagsModel
	manifest     ManifestModel
	blobs        BlobsModel
	uploads      UploadsModel
	empty        EmptyStateModel
	mutation     MutationUnavailableModel

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

func NewModel(service QueryService) Model {
	return Model{
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
}

func (m Model) Init() tea.Cmd {
	return m.loadCatalogCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
		m.screen = screenManifest
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
		body.WriteString("\n\nEnter: open tags · q: quit")
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

func (m *Model) moveSelection(delta int) {
	switch m.screen {
	case screenRepositories:
		m.repositories.Selected = boundedIndex(m.repositories.Selected+delta, len(m.repositories.Items))
	case screenTags:
		m.tags.Selected = boundedIndex(m.tags.Selected+delta, len(m.tags.Items))
	case screenBlobs:
		m.blobs.Selected = boundedIndex(m.blobs.Selected+delta, len(m.blobs.Items))
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

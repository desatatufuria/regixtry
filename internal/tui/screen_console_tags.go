package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	appregixtry "regixtry/internal/app/regixtry"
)

// consoleTagsScreen is "Console > Repositories > Tags" (the Tags board): the
// tags of one repository, loaded on mount.
type consoleTagsScreen struct {
	gen        uint64
	repository string
	items      []appregixtry.TagDetails
	loaded     bool
	err        string
	sortMode   sortMode
	selected   int

	confirm    confirmPrompt
	shared     *consoleShared
	sync       consoleSync
	notice     string
	noticeFail bool
}

func newConsoleTagsScreen(repository string, mode sortMode) consoleTagsScreen {
	return consoleTagsScreen{gen: consoleGenerations.Add(1), repository: repository, sortMode: mode, shared: newConsoleShared()}
}

func (s consoleTagsScreen) ID() screen { return screenTags }

func (s consoleTagsScreen) Init(env screenEnv) tea.Cmd {
	return consoleTagsCmd(env, s.gen, s.repository, consoleStamp{})
}

func (s consoleTagsScreen) selectedTag() (appregixtry.TagDetails, bool) {
	if len(s.items) == 0 {
		return appregixtry.TagDetails{}, false
	}
	return s.items[boundedIndex(s.selected, len(s.items))], true
}

// ---- messages ----

type consoleTagsMsg struct {
	gen        uint64
	stamp      consoleStamp
	repository string
	result     []appregixtry.TagDetails
	err        error
}

type tagDeletedMsg struct {
	gen        uint64
	repository string
	tag        string
	err        error
}

func consoleTagsCmd(env screenEnv, gen uint64, repository string, stamp consoleStamp) tea.Cmd {
	service := env.Service
	return func() tea.Msg {
		if service == nil {
			return consoleTagsMsg{gen: gen, stamp: stamp, repository: repository, err: errConsoleUnavailable}
		}
		result, err := service.TagDetails(context.Background(), repository, 100, "")
		return consoleTagsMsg{gen: gen, stamp: stamp, repository: repository, result: result, err: err}
	}
}

func (s consoleTagsScreen) Update(env screenEnv, msg tea.Msg) (adminScreen, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case keyMsg:
		return s.updateKey(env, typed)
	case consoleTagsMsg:
		if typed.gen != s.gen {
			return s, nil, false
		}
		return s.applyTags(typed), nil, false
	case tagDeletedMsg:
		if typed.gen != s.gen {
			// A late result of an earlier visit: the tag is gone all the same.
			if typed.err == nil && typed.repository == s.repository {
				s.items = slices.DeleteFunc(slices.Clone(s.items), func(t appregixtry.TagDetails) bool { return t.Name == typed.tag })
				s.selected = boundedIndex(s.selected, len(s.items))
			}
			return s, nil, false
		}
		return s.applyDeleted(env, typed)
	}
	return s, nil, false
}

func (s consoleTagsScreen) applyTags(msg consoleTagsMsg) consoleTagsScreen {
	sync, ok, hide := s.sync.accept(msg.stamp, s.shared)
	if !ok {
		return s
	}
	s.sync = sync
	if msg.err != nil {
		if !s.loaded {
			s.err = msg.err.Error()
			return s
		}
		s.notice, s.noticeFail = "Could not refresh tags: "+msg.err.Error(), true
		return s
	}
	keep := ""
	if current, ok := s.selectedTag(); ok {
		keep = current.Name
	}
	items := slices.DeleteFunc(slices.Clone(msg.result), func(t appregixtry.TagDetails) bool { return hide && s.shared.isDeleted(tagKey(s.repository, t.Name)) })
	sortTagItems(items, s.sortMode)
	s.items, s.loaded, s.err = items, true, ""
	s.selected = boundedIndex(s.selected, len(items))
	if i := slices.IndexFunc(items, func(t appregixtry.TagDetails) bool { return t.Name == keep }); i >= 0 {
		s.selected = i
	}
	return s
}

func (s consoleTagsScreen) applyDeleted(env screenEnv, msg tagDeletedMsg) (adminScreen, tea.Cmd, bool) {
	s.confirm = confirmPrompt{}
	s.shared.setBusy(false)
	if msg.err != nil {
		s.notice, s.noticeFail = msg.err.Error(), true
		return s, nil, false
	}
	s.shared.markDeleted(tagKey(msg.repository, msg.tag))
	s.items = slices.DeleteFunc(slices.Clone(s.items), func(t appregixtry.TagDetails) bool { return t.Name == msg.tag })
	s.selected = boundedIndex(s.selected, len(s.items))
	s.notice, s.noticeFail = fmt.Sprintf("Tag %q deleted. Refreshing tags...", msg.tag), false
	sync, stamp := s.sync.launch(s.shared)
	s.sync = sync
	return s, consoleTagsCmd(env, s.gen, s.repository, stamp), false
}

func (s consoleTagsScreen) updateKey(env screenEnv, msg keyMsg) (adminScreen, tea.Cmd, bool) {
	if s.confirm.Active() {
		next, cmd, _ := s.confirm.update(env, msg)
		s.confirm = next
		if cmd != nil {
			s.shared.setBusy(true)
		}
		return s, cmd, true
	}
	current, has := s.selectedTag()
	if cursor, moved := consoleMove(msg, s.selected, len(s.items)); moved {
		s.selected = cursor
		return s, nil, true
	}
	switch {
	case isBackKey(msg):
		return s, navigate(screenRepositories), true
	case isTabKey(msg):
		// The parent opens the admin panel whenever an admin API exists.
		if env.Client == nil {
			s.notice, s.noticeFail = consoleUnavailableNotice, true
			return s, nil, true
		}
		return s, nil, false
	case isEnterKey(msg):
		if has {
			return s, consoleAsk(openManifestMsg{repository: s.repository, tag: current.Name}), true
		}
	case isCommandRune(msg, 's'):
		s.sortMode = s.sortMode.next()
		s.selected = 0
		items := slices.Clone(s.items)
		sortTagItems(items, s.sortMode)
		s.items = items
	case isCommandRune(msg, 'd'):
		switch {
		case s.shared.isBusy():
			// A delete is already on its way.
		case !has:
			s.notice, s.noticeFail = "No tag selected to delete.", true
		default:
			s.confirm = s.deleteConfirm(current)
			s.notice = ""
		}
	}
	return s, nil, true
}

// deleteConfirm builds the confirmation of deleting one tag. What the service
// does (Service.DeleteManifest with a tag name, which is never a digest): it
// removes that tag only; the manifest, its other tags and the blobs stay. The
// repository and tag are captured here, so the command deletes exactly what
// the dialog names whatever the selection or the list does meanwhile.
func (s consoleTagsScreen) deleteConfirm(tag appregixtry.TagDetails) confirmPrompt {
	gen, repository, name := s.gen, s.repository, tag.Name
	return newConfirmPrompt(
		"Delete tag "+name,
		fmt.Sprintf("The tag is removed from %s. Its manifest stays, untagged, with the signatures and referrers attached to it.", repository),
		"delete",
		fmt.Sprintf("Deleting tag %s...", name),
		func(env screenEnv) tea.Cmd { return tagDeleteCmd(env, gen, repository, name) },
	).withTarget(repository+":"+name, tagDetail(tag)).withButtonLabel("Delete").
		withConsequence("Pulls by this tag stop working; pulls by digest keep working. Its other tags and the blobs stay. This cannot be undone.")
}

func tagDeleteCmd(env screenEnv, gen uint64, repository, tag string) tea.Cmd {
	service := env.Service
	return func() tea.Msg {
		if service == nil {
			return tagDeletedMsg{gen: gen, repository: repository, tag: tag, err: errConsoleUnavailable}
		}
		_, err := service.DeleteManifest(context.Background(), repository, tag)
		return tagDeletedMsg{gen: gen, repository: repository, tag: tag, err: err}
	}
}

func tagDetail(tag appregixtry.TagDetails) string {
	return "pushed " + formatTagCreatedAt(tag.CreatedAt) + " by " + tagPushedByLabel(tag)
}

// tagPushedByLabel renders a tag's pusher, never blank: a legacy manifest, one
// pushed without a username resolver, or a pusher since deleted all have "".
func tagPushedByLabel(tag appregixtry.TagDetails) string {
	if strings.TrimSpace(tag.PushedBy) == "" {
		return "—"
	}
	return tag.PushedBy
}

// formatTagCreatedAt renders a tag's manifest creation time, never blank.
func formatTagCreatedAt(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// tagSignedLabel is the SIGNED column: "n/a" when the signing policy is off
// (a verdict means nothing when nothing is enforced), "yes" for a verified
// signature, "no" for every other state while signing is on.
func tagSignedLabel(tag appregixtry.TagDetails) string {
	if !tag.SigningEnabled {
		return "n/a"
	}
	if tag.SignatureState == appregixtry.SignatureStatusVerified {
		return "yes"
	}
	return "no"
}

// ---- view ----

func (s consoleTagsScreen) Keys() screenKeys {
	return screenKeys{short: []key.Binding{
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑↓", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "manifest")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete tag")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "admin")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	}}
}

func (s consoleTagsScreen) View(theme adminTheme, env screenEnv) screenFrame {
	width, height := env.Layout.Width, env.Layout.Height
	bodyW, bodyH := consoleBodySize(width, height)

	spec := consoleTableSpec{
		columns: []consoleColumn{{title: "TAG"}, {title: "CREATED", width: 16}, {title: "PUSHED BY", width: 16}, {title: "SIGNED", width: 6}},
		cursor:  s.selected,
	}
	summary := ""
	switch {
	case s.err != "":
		spec.message, spec.messageStyle = "Could not load tags: "+s.err, theme.error
	case !s.loaded:
		spec.message, spec.messageStyle = "Loading tags...", theme.muted
	case len(s.items) == 0:
		spec.message, spec.messageStyle = s.repository+" has no published tags yet.", theme.muted
		summary = s.repository + ", 0 tags"
	default:
		summary = s.repository + ", " + countOf(len(s.items), "tag", "tags") + ", sort: " + s.sortMode.label()
	}
	for _, t := range s.items {
		signed := theme.text
		switch tagSignedLabel(t) {
		case "yes":
			signed = theme.success
		case "no":
			signed = theme.error
		case "n/a":
			signed = theme.muted
		}
		spec.rows = append(spec.rows, []consoleCell{
			{t.Name, theme.text}, {formatTagCreatedAt(t.CreatedAt), theme.text}, {tagPushedByLabel(t), theme.text}, {tagSignedLabel(t), signed},
		})
	}
	status := consoleStatusLine(theme, s.confirm, s.notice, s.noticeFail, bodyW)
	body := consoleBody(theme, bodyW, bodyH, "Tags", summary, spec, status,
		"SIGNED: yes = signature verified, no = signing is on but no valid signature, n/a = signing is off.")

	crumbs := []string{"Console", "Repositories", s.repository, "Tags"}
	frame := screenFrame{Context: strings.Join(crumbs, " › "), Full: true, Body: renderConsoleFrame(theme, env, crumbs, body, s.Keys().short)}
	if s.confirm.Active() {
		frame.Overlay = s.confirm.view(theme)
	}
	return frame
}

func (s consoleTagsScreen) dialogOpen() bool { return s.confirm.Active() }

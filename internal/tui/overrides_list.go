package tui

import (
	"slices"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"regixtry/internal/ports"
)

// overridesPageRows is how far PgUp and PgDn move the selection.
const overridesPageRows = 10

// overridesScanLimit is how many repositories' latest scan the Trivy list reads.
const overridesScanLimit = 100

// overridesList is what the three per-repository override screens share: the
// repositories (the catalog plus every stored override), which of them
// overrides the feature, the selected one and the editor that opens on it.
// Every method returns the new value; none mutates its receiver.
//
// The field name editor is read by Model (migratedScreenCapturesTextInput): do
// not rename it.
type overridesList struct {
	// gen identifies the screen instance: every result it asks for carries it,
	// and a result with another gen is dropped.
	gen     uint64
	feature string
	// overrides is the stored list, the source the rows are built from.
	overrides []ports.RepositoryOverrideDetails
	// scans and scanOrder are the latest scan of each repository, in the
	// server's severity-first order (Trivy only); scansErr says why they are
	// missing.
	scans     map[string]ports.RepositoryScanSummary
	scanOrder []string
	scansErr  string
	rows      []featureOverrideRow
	cursor    int
	// changes counts the saves and clears the screen took: a list read that
	// started before the last one is older than the rows and is dropped.
	changes uint64
	// loaded is true once the overrides were read; reading while a read is in
	// flight, err the failure of the last one.
	loaded  bool
	reading bool
	err     string
	// note is the outcome of the last change ("Override saved.").
	note   string
	editor overrideEditor
}

func newOverridesList(feature string) overridesList {
	return overridesList{gen: securityGenerations.Add(1), feature: feature}
}

// selectedRow is the repository under the cursor.
func (l overridesList) selectedRow() (featureOverrideRow, bool) {
	if len(l.rows) == 0 {
		return featureOverrideRow{}, false
	}
	return l.rows[boundedIndex(l.cursor, len(l.rows))], true
}

// rebuild recomputes the rows from the stored overrides, keeping the cursor on
// the same repository.
func (l overridesList) rebuild(env screenEnv) overridesList {
	selected, _ := l.selectedRow()
	l.rows = orderedOverrideRows(l.scanOrder, env.KnownRepositories, l.overrides)
	l.cursor = boundedIndex(l.cursor, len(l.rows))
	if i := slices.IndexFunc(l.rows, func(r featureOverrideRow) bool { return r.Repository == selected.Repository }); i >= 0 {
		l.cursor = i
	}
	return l
}

// loadCmd reads the stored overrides, remembering how many changes the screen
// had taken when the read started.
func (l overridesList) loadCmd(env screenEnv) tea.Cmd {
	return loadOverrideListCmd(env, l.gen, l.changes, l.feature)
}

// applyOverrides takes a loaded overrides list. A read that started before the
// last save or clear would show the old state over the new one: it is dropped
// and the list is read again.
func (l overridesList) applyOverrides(env screenEnv, msg overrideListMsg) (overridesList, tea.Cmd) {
	if msg.err == nil && msg.seq != l.changes {
		return l, l.loadCmd(env)
	}
	l.reading = false
	if msg.err != nil {
		l.err = msg.err.Error()
		return l, nil
	}
	l.err = ""
	l.overrides = msg.overrides
	l.loaded = true
	return l.rebuild(env), nil
}

// applySummaries takes the loaded scan summaries.
func (l overridesList) applySummaries(env screenEnv, msg overrideSummariesMsg) overridesList {
	l.scans, l.scanOrder, l.scansErr = nil, nil, ""
	if msg.err != nil {
		l.scansErr = msg.err.Error()
		return l
	}
	l.scans = make(map[string]ports.RepositoryScanSummary, len(msg.summaries))
	for _, summary := range msg.summaries {
		repository := summary.Run.Repository
		if _, dup := l.scans[repository]; dup {
			continue
		}
		l.scans[repository] = summary
		l.scanOrder = append(l.scanOrder, repository)
	}
	return l
}

// applySaved reflects a successful save or clear onto the row of the
// repository it was for, whether or not its editor is still open.
func (l overridesList) applySaved(env screenEnv, msg overrideSavedMsg) overridesList {
	if msg.feature != l.feature || msg.err != nil {
		return l
	}
	l.changes++
	rest := make([]ports.RepositoryOverrideDetails, 0, len(l.overrides)+1)
	for _, stored := range l.overrides {
		if stored.Repository != msg.repository {
			rest = append(rest, stored)
		}
	}
	l.overrides = rest
	if msg.exists {
		saved := msg.override
		saved.Repository = msg.repository
		l.overrides = append(l.overrides, saved)
		l.note = "Override saved for " + msg.repository + "."
	} else {
		l.note = "Override cleared for " + msg.repository + "."
	}
	return l.rebuild(env)
}

// applyEditorMsg routes the results an open editor waits for, and the save
// results the rows need whether or not the editor is open. handled is false
// for any other message.
func (l overridesList) applyEditorMsg(env screenEnv, msg tea.Msg) (overridesList, tea.Cmd, bool) {
	switch typed := msg.(type) {
	case overrideLoadedMsg:
		next, cmd := l.editor.applyLoaded(env, typed)
		l.editor = next
		return l, cmd, true
	case overrideSavedMsg:
		next, cmd := l.editor.applySaved(env, typed)
		l.editor = next
		return l.applySaved(env, typed), cmd, true
	case overrideGlobalMsg:
		l.editor = l.editor.applyGlobal(typed)
		return l, nil, true
	case overrideKeyUsageMsg:
		l.editor = l.editor.applyKeyUsage(typed)
		return l, nil, true
	}
	return l, nil, false
}

// updateEditorKey gives a key to the open editor.
func (l overridesList) updateEditorKey(env screenEnv, msg keyMsg) (overridesList, tea.Cmd) {
	next, cmd, _ := l.editor.update(env, msg)
	l.editor = next
	return l, cmd
}

// moveKey moves the selection. handled is false for any other key.
func (l overridesList) moveKey(msg keyMsg) (overridesList, bool) {
	last := len(l.rows) - 1
	switch {
	case isMoveUpKey(msg):
		l.cursor = max(l.cursor-1, 0)
	case isMoveDownKey(msg):
		l.cursor = min(l.cursor+1, max(last, 0))
	case isPgUpKey(msg):
		l.cursor = max(l.cursor-overridesPageRows, 0)
	case isPgDnKey(msg):
		l.cursor = min(l.cursor+overridesPageRows, max(last, 0))
	case isHomeKey(msg):
		l.cursor = 0
	case isEndKey(msg):
		l.cursor = max(last, 0)
	default:
		return l, false
	}
	l.cursor = boundedIndex(l.cursor, len(l.rows))
	l.note = ""
	return l, true
}

// openEditor opens the editor on the selected repository. The editor is bound
// to that repository and feature for good: a later reload or move of the
// selection does not change what it edits.
func (l overridesList) openEditor(env screenEnv) (overridesList, tea.Cmd) {
	row, ok := l.selectedRow()
	if !ok {
		return l, nil
	}
	l.editor = newOverrideEditor(l.gen, l.feature, row.Repository)
	l.note = ""
	return l, l.editor.Init(env)
}

// orderedOverrideRows is the repository list of one feature: the repositories
// in scanned first (the order the scans came in), then the catalog, then any
// repository that only has a stored override. A repository appears once.
func orderedOverrideRows(scanned, known []string, overrides []ports.RepositoryOverrideDetails) []featureOverrideRow {
	byRepository := make(map[string]ports.RepositoryOverrideDetails, len(overrides))
	for _, override := range overrides {
		byRepository[override.Repository] = override
	}
	seen := make(map[string]bool, len(scanned)+len(known)+len(overrides))
	rows := make([]featureOverrideRow, 0, len(scanned)+len(known)+len(overrides))
	add := func(repository string) {
		if seen[repository] {
			return
		}
		seen[repository] = true
		detail, has := byRepository[repository]
		rows = append(rows, featureOverrideRow{Repository: repository, HasOverride: has, Detail: detail})
	}
	for _, repository := range scanned {
		add(repository)
	}
	for _, repository := range known {
		add(repository)
	}
	for _, override := range overrides {
		add(override.Repository)
	}
	return rows
}

// overridesConfirmKeys is the key hint while the editor asks to clear.
var overridesConfirmKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / enter", "clear override")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}}

// hints is the footer of the open editor.
func (l overridesList) editorHints() screenKeys {
	if l.editor.confirm.Active() {
		return overridesConfirmKeys
	}
	return l.editor.keysHint()
}

package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"regixtry/internal/ports"
)

// Shared drawing of the three Security configuration screens (Image scanning,
// Secret scanning, Signing policy) and their modals: the approved "Image
// scanning", "Secret scanning", "Signing policy" and "Config modal" boards.

const (
	securityPanelGap = 2
	// securityOverridesMinRows is the least the overrides panel is given: its
	// border (2), title, header, one row and the line that counts the rest.
	securityOverridesMinRows = 6
	// securityModalWidth is the board's 420px modal at about 8.5px a column.
	securityModalWidth = 52
	// securitySigningModalWidth leaves room for fingerprints and identities.
	securitySigningModalWidth = 72
	// securityModalChrome is the modal's border plus two columns of padding on
	// each side.
	securityModalChrome = 6
)

// securityModalKeys is the key hint of an open config or policy modal.
var securityModalKeys = screenKeys{short: []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "toggle")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}}

// securityTitleLine is the screen title on the left and a one-line description
// on the right.
func securityTitleLine(theme adminTheme, title, subtitle string, width int) string {
	right := theme.muted.Render(subtitle)
	left := theme.subheading.MaxWidth(max(width-lipgloss.Width(right)-2, 1)).Render(title)
	if lipgloss.Width(left)+lipgloss.Width(right)+2 > width {
		return fitText(left, width)
	}
	return left + strings.Repeat(" ", width-lipgloss.Width(left)-lipgloss.Width(right)) + right
}

// securityPanel is a bordered card of exactly height rows and width columns
// whose first line is its muted title.
func securityPanel(theme adminTheme, title string, width, height int, lines []string) string {
	return userPanel(theme, width, height, append([]string{userSectionTitle(theme, title)}, lines...))
}

// securityRow is "label" on the left and "value" on the right of width columns.
func securityRow(theme adminTheme, label, value string, width int) string {
	left := theme.muted.Render(label)
	room := max(width-lipgloss.Width(left)-2, 1)
	value = fitText(value, room)
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(value), 1)
	return left + strings.Repeat(" ", gap) + value
}

// securityFlag renders a switch the way the boards do: "[x] enabled" in green,
// "[ ] disabled" in amber.
func securityFlag(theme adminTheme, on bool, onText, offText string) string {
	if on {
		return theme.success.UnsetBold().Render("[x] " + onText)
	}
	return theme.warning.UnsetBold().Render("[ ] " + offText)
}

// securityNote is wrapped muted text, one entry per line.
func securityNote(theme adminTheme, text string, width int) []string {
	var lines []string
	for _, line := range wrapText(text, max(width, 1)) {
		lines = append(lines, theme.muted.Render(line))
	}
	return lines
}

// securityPageFields maps label to value for one section of a feature page.
func securityPageFields(page ports.FeaturePage, sectionID string) map[string]string {
	fields := map[string]string{}
	for _, section := range page.Sections {
		if section.ID != sectionID {
			continue
		}
		for _, field := range section.Fields {
			fields[field.Label] = strings.TrimSpace(field.Value)
		}
	}
	return fields
}

// securityStatusLine is the one-line state of the feature behind a screen: is
// it enabled and, when it has a managed runtime, how that runtime is doing.
// Everything comes from the feature page; nothing is invented.
func securityStatusLine(theme adminTheme, page ports.FeaturePage, width int) string {
	state := theme.warning.UnsetBold().Render("Feature disabled")
	if page.Summary.Enabled {
		state = theme.success.UnsetBold().Render("Feature enabled")
	}
	parts := []string{state}
	if runtime := securityPageFields(page, "runtime"); len(runtime) > 0 {
		known := func(label string) string {
			value := runtime[label]
			if value == "" || strings.EqualFold(value, "unknown") {
				return ""
			}
			return value
		}
		if status := known("Status"); status != "" {
			health := known("Health")
			text := "Runtime " + status
			if health != "" && health != status {
				text += ", " + health
			}
			parts = append(parts, theme.muted.Render(text))
		}
		if version := known("Version"); version != "" {
			parts = append(parts, theme.muted.Render("version "+version))
		}
		if update := known("Update Status"); update != "" {
			text := "update " + update
			if latest := known("Latest Version"); latest != "" && update == "available" {
				text += " (" + latest + ")"
			}
			parts = append(parts, theme.muted.Render(text))
		}
		if runtime["Rollback Available"] == "true" {
			parts = append(parts, theme.muted.Render("rollback available"))
		}
	}
	return fitText(strings.Join(parts, theme.muted.Render("  ·  ")), width)
}

// securityShell draws body inside the application shell.
func securityShell(theme adminTheme, env screenEnv, crumb string, active shellNavID, body []string, hints screenKeys) string {
	return renderShell(theme, shellSpec{
		Breadcrumb: shellCrumb("Security", crumb),
		Operator:   env.Session.Username,
		Active:     active,
		Body:       strings.Join(body, "\n"),
		Hints:      hints.short,
		Width:      env.Layout.Width,
		Height:     env.Layout.Height,
	})
}

// securityNotice is the line above the cards: the failure, progress or outcome
// of the last action, else nothing.
func securityNotice(theme adminTheme, err, submitting, note string, width int) string {
	switch {
	case err != "":
		return theme.error.UnsetBold().Render(fitText(err, width))
	case submitting != "":
		return theme.warning.UnsetBold().Render(fitText(submitting, width))
	case note != "":
		return theme.success.UnsetBold().Render(fitText(note, width))
	}
	return ""
}

// securityLoadingBody is what a screen shows before its feature page arrived,
// or when it could not be read.
func securityLoadingBody(theme adminTheme, err string, width int) []string {
	if err != "" {
		lines := []string{theme.error.UnsetBold().Render(fitText("Could not read this feature: "+err, width))}
		return append(lines, theme.muted.Render("Press r to try again."))
	}
	return []string{theme.muted.Render("Loading…")}
}

// ---- per-repository overrides panel ----

// securityOverrides is the read-only list of one feature's per-repository
// overrides shown on its screen. Editing happens on the override screens.
type securityOverrides struct {
	loaded bool
	err    string
	rows   []featureOverrideRow
}

func (o securityOverrides) apply(env screenEnv, msg securityOverridesMsg) securityOverrides {
	if msg.err != nil {
		o.err = msg.err.Error()
		return o
	}
	rows := mergeFeatureOverrideRows(env.KnownRepositories, msg.overrides)
	// Overrides first: they are the point of the list and the inherited rows of
	// a large catalog must not push them off the panel.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].HasOverride && !rows[j].HasOverride })
	return securityOverrides{loaded: true, rows: rows}
}

const (
	securityColStatus  = 9
	securityColEnabled = 7
	securityColDetail  = 20
	securityColUpdated = 11
	securityColGap     = 2
)

// securityOverridesPanel draws the overrides card in exactly height rows.
func securityOverridesPanel(theme adminTheme, env screenEnv, feature string, o securityOverrides, width, height int) string {
	inner := max(width-4, 1)
	capacity := max(height-4, 1) // border (2), title and header
	repoW := inner - (securityColStatus + securityColEnabled + securityColDetail + securityColUpdated + 4*securityColGap)
	showDetail := repoW >= 12
	if !showDetail {
		repoW = inner - (securityColStatus + securityColEnabled + securityColUpdated + 3*securityColGap)
	}
	repoW = max(repoW, 1)

	line := func(repo, status, enabled, detail, updated string) string {
		gap := strings.Repeat(" ", securityColGap)
		text := padText(repo, repoW) + gap + padText(status, securityColStatus) + gap + padText(enabled, securityColEnabled)
		if showDetail {
			text += gap + padText(detail, securityColDetail)
		}
		return text + gap + padText(updated, securityColUpdated)
	}

	lines := []string{theme.muted.Bold(true).Render(line("REPOSITORY", "STATUS", "ENABLED", "DETAIL", "UPDATED"))}
	switch {
	case o.err != "" && len(o.rows) == 0:
		lines = append(lines, theme.error.UnsetBold().Render(fitText("Could not read the overrides: "+o.err, inner)))
	case !o.loaded:
		lines = append(lines, theme.muted.Render("Loading…"))
	case len(o.rows) == 0:
		lines = append(lines, theme.muted.Render("No repositories yet."))
	default:
		shown := o.rows
		more := 0
		if len(shown) > capacity {
			more = len(shown) - (capacity - 1)
			shown = shown[:capacity-1]
		}
		now := env.now()
		for _, row := range shown {
			lines = append(lines, securityOverrideLine(theme, feature, row, now, line))
		}
		if more > 0 {
			lines = append(lines, theme.muted.Render(fmt.Sprintf("+%d more, o opens the list", more)))
		}
	}
	return securityPanel(theme, "PER-REPOSITORY OVERRIDES", width, height, lines)
}

func securityOverrideLine(theme adminTheme, feature string, row featureOverrideRow, now time.Time, line func(repo, status, enabled, detail, updated string) string) string {
	if !row.HasOverride {
		return theme.muted.Render(line(row.Repository, "inherited", "—", securityInheritedDetail(feature), "-"))
	}
	enabled := "yes"
	text := line(row.Repository, "override", enabled, securityOverrideDetail(feature, row.Detail), securityOverrideDate(row.Detail.UpdatedAt, now))
	if !row.Detail.Enabled {
		text = line(row.Repository, "override", "no", securityOverrideDetail(feature, row.Detail), securityOverrideDate(row.Detail.UpdatedAt, now))
		return theme.warning.UnsetBold().Render(text)
	}
	return theme.text.Render(text)
}

func securityInheritedDetail(feature string) string {
	if feature == signingFeatureName {
		return "global policy"
	}
	return "global settings"
}

// securityOverrideDetail names what an override sets, from the fields the
// stored override really carries.
func securityOverrideDetail(feature string, detail ports.RepositoryOverrideDetails) string {
	switch feature {
	case signingFeatureName:
		if !detail.Enabled {
			return "not enforced"
		}
		return fmt.Sprintf("%s, %s", securityCount(len(detail.TrustedPublicKeys), "key", "keys"), securityCount(len(detail.TrustedIdentities), "identity", "identities"))
	case gitleaksFeatureName:
		if !detail.Enabled {
			return "scanning off"
		}
		if detail.ConfigPath != "" {
			return "config path set"
		}
		return "default config"
	default:
		if !detail.Enabled {
			return "scanning off"
		}
		switch {
		case detail.IgnoreFilePath != "" && detail.IgnorePolicyPath != "":
			return "ignore file and policy"
		case detail.IgnoreFilePath != "":
			return "ignore file set"
		case detail.IgnorePolicyPath != "":
			return "ignore policy set"
		}
		return "scanning on"
	}
}

// securityOverrideDate is "14 Nov" this year, "14 Nov 2025" otherwise.
func securityOverrideDate(at, now time.Time) string {
	if at.IsZero() {
		return "-"
	}
	at = at.UTC()
	if at.Year() == now.UTC().Year() {
		return at.Format("2 Jan")
	}
	return at.Format("2 Jan 2006")
}

// securityTopHeight is the height of the cards above the overrides panel: what
// they need, but never so much that the overrides panel falls under its
// minimum.
func securityTopHeight(natural, available int) int {
	return max(min(natural, available-securityOverridesMinRows), 3)
}

// ---- modals ----

// securityModalSpec is one modal: a title, the lines of its body and the state
// of its buttons.
type securityModalSpec struct {
	title    string
	subtitle string
	body     []string
	// focusLine is the body line that must stay visible when the body is taller
	// than the terminal allows.
	focusLine int
	err       string
	// warning is a question the operator must answer with the next key.
	warning string
	// note is the outcome of the last save when the modal stays open.
	note   string
	saving bool
	// noSave leaves out the Save button: nothing can be saved yet.
	noSave bool
	hint   string
	width  int
}

func securityToggleLine(theme adminTheme, label string, on, focused bool) string {
	box := "[ ]"
	if on {
		box = "[x]"
	}
	style := theme.text
	if focused {
		style = theme.accent
	}
	return style.Render(box + " " + label)
}

// securityChoiceField is a field whose value is picked, not typed.
func securityChoiceField(theme adminTheme, label, value string, focused bool, width int) []string {
	style := theme.input
	if focused {
		style = theme.inputFocus
	}
	return []string{theme.muted.Render(label), style.Width(width).Render(fitText(value, max(width-2, 1)))}
}

func securityModal(theme adminTheme, env screenEnv, spec securityModalSpec) string {
	// A bare screenEnv{} (render-only tests) carries no terminal size: assume
	// the smallest supported one.
	termW, termH := max(env.Layout.Width, minViewportWidth), max(env.Layout.Height, minViewportHeight)
	width := min(spec.width, max(termW-4, 20))
	inner := max(width-securityModalChrome, 1)

	head := []string{theme.subheading.Render(fitText(spec.title, inner))}
	head = append(head, securityNote(theme, spec.subtitle, inner)...)

	var tail []string
	if spec.err != "" {
		for _, line := range wrapText(spec.err, inner) {
			tail = append(tail, theme.error.UnsetBold().Render(line))
		}
	}
	if spec.warning != "" {
		for _, line := range wrapText(spec.warning, inner) {
			tail = append(tail, theme.warning.UnsetBold().Render(line))
		}
	}
	if spec.note != "" {
		tail = append(tail, theme.success.UnsetBold().Render(spec.note))
	}
	save := "Save  enter"
	if spec.saving {
		save = "Saving…"
	}
	buttons := theme.pill.Render(save) + "  " + theme.muted.Render("Cancel  esc")
	if spec.noSave {
		buttons = theme.muted.Render("Cancel  esc")
	}
	tail = append(tail, "", buttons)
	tail = append(tail, securityNote(theme, spec.hint, inner)...)

	var body []string
	for _, line := range spec.body {
		body = append(body, strings.Split(line, "\n")...)
	}
	room := max(termH-2-2-len(head)-len(tail), 1) // terminal margin, border
	if len(body) > room {
		start, end := grantWindow(spec.focusLine, len(body), room)
		body = body[start:end]
	}

	lines := append(append(head, body...), tail...)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(theme.accent.GetForeground()).
		Padding(0, 2).Width(width).
		Render(strings.Join(fitLinesTo(lines, inner), "\n"))
}

// fitLinesTo clips every line to width columns.
func fitLinesTo(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = fitText(line, width)
	}
	return out
}

// securityCount is "1 key" or "2 keys".
func securityCount(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

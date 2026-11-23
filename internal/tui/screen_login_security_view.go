package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

const (
	// loginWideBodyWidth is the content width from which the settings and the
	// waiting accounts sit in a left column and the failures on the right
	// (the 200x40 board); narrower terminals stack everything (150x24).
	loginWideBodyWidth = 150
	// loginTilesBodyHeight is the content height from which the summary strip
	// is four boxed tiles instead of one line.
	loginTilesBodyHeight = 30
	loginLeftColumnWidth = 60
	loginColumnGap       = 2
	loginTileRows        = 4
	// loginSettingsRows is the settings panel's inner rows: three rows of two
	// settings when stacked, one setting a row in the left column.
	loginSettingsRowsStacked = 3
	loginSettingsRowsWide    = 6
	// Fixed columns of the two tables.
	loginColFails    = 6
	loginColTimeLeft = 11
	loginColSeen     = 12
	loginColEndpoint = 8
	loginColReason   = 12
	loginColCount    = 6
	loginColGap      = 2
	loginMarkerWidth = 2
	loginIPMaxWidth  = 39 // a full IPv6 address
	loginMinTableRow = 3
)

const loginSettingsHint = "Set by service flags. Read-only here."

const (
	loginLogBanner = "Backoff is in log mode: failures are counted but nobody is delayed. Start the service with -login-backoff-mode=enforce to delay repeated failures."
	loginOffBanner = "Backoff is off: failures are counted and nobody is delayed."
)

// loginRect is a panel's place in the content area, in cells from its origin.
type loginRect struct{ x, y, w, h int }

// loginLayout is the screen's geometry for a terminal size. View and the mouse
// both read it, so a click lands on the row that was drawn.
type loginLayout struct {
	wide, tiles  bool
	bodyW, bodyH int
	banner       []string
	head         int
	settings     loginRect
	waiting      loginRect
	failures     loginRect
}

func (s loginSecurityScreen) bannerText() string {
	switch s.backoff.Mode {
	case loginModeLog:
		return loginLogBanner
	case loginModeOff:
		return loginOffBanner
	}
	return ""
}

func (s loginSecurityScreen) hasNotice() bool {
	return s.loaded && !s.forbidden && (s.notice != "" || s.moreErr != "" || s.busyText() != "")
}

// ready reports that the panels (not a loading or error message) are shown.
func (s loginSecurityScreen) ready() bool { return s.loaded && !s.forbidden }

func (s loginSecurityScreen) layout(env screenEnv) loginLayout {
	bodyW, bodyH := shellContentSize(env.Layout.Width, env.Layout.Height)
	l := loginLayout{bodyW: bodyW, bodyH: bodyH, wide: bodyW >= loginWideBodyWidth, tiles: bodyH >= loginTilesBodyHeight}
	l.head = 1
	if s.ready() {
		if s.hasNotice() {
			l.head++
		}
		if text := s.bannerText(); text != "" {
			l.banner = wrapText(text, max(bodyW-2, 1))
			l.head += len(l.banner)
		}
	}
	if l.tiles {
		l.head += loginTileRows
	} else {
		l.head++
	}
	if !s.ready() {
		return l
	}
	rest := max(bodyH-l.head, 0)
	if l.wide {
		// One more row than settings: the read-only hint is a body line here,
		// the 60-cell border has no room for it next to the title.
		settingsH := 2 + loginSettingsRowsWide + 1
		rightW := max(bodyW-loginLeftColumnWidth-loginColumnGap, 1)
		l.settings = loginRect{0, l.head, loginLeftColumnWidth, settingsH}
		l.waiting = loginRect{0, l.head + settingsH, loginLeftColumnWidth, max(rest-settingsH, loginMinTableRow+1)}
		l.failures = loginRect{loginLeftColumnWidth + loginColumnGap, l.head, rightW, max(rest, loginMinTableRow+1)}
		return l
	}
	settingsH := 2 + loginSettingsRowsStacked
	remaining := max(rest-settingsH, 0)
	natural := 2 + 1 + max(len(s.backoff.Accounts), 1)
	waitingH := min(natural, max(remaining/3, loginMinTableRow+1))
	failuresH := max(remaining-waitingH, loginMinTableRow+1)
	l.settings = loginRect{0, l.head, bodyW, settingsH}
	l.waiting = loginRect{0, l.head + settingsH, bodyW, waitingH}
	l.failures = loginRect{0, l.head + settingsH + waitingH, bodyW, failuresH}
	return l
}

// failureDataRows is how many failure rows the failures panel shows at once:
// its height without the border, the column header and the filter row.
func (s loginSecurityScreen) failureDataRows(l loginLayout) int {
	rows := l.failures.h - 2 - 1
	if s.filtering {
		rows--
	}
	return max(rows, 0)
}

func (s loginSecurityScreen) waitingDataRows(l loginLayout) int { return max(l.waiting.h-2-1, 0) }

// ---- View ----

func (s loginSecurityScreen) View(theme adminTheme, env screenEnv) screenFrame {
	l := s.layout(env)
	lines := []string{s.titleLine(theme, l.bodyW)}
	if s.ready() {
		if s.hasNotice() {
			lines = append(lines, s.noticeLine(theme, l.bodyW))
		}
		for _, row := range l.banner {
			lines = append(lines, lipgloss.NewStyle().Foreground(theme.warning.GetForeground()).
				Background(lipgloss.Color(palettePanelHex)).Width(l.bodyW).Render(" "+row))
		}
	}
	lines = append(lines, s.stripLines(theme, l)...)
	switch {
	case s.forbidden:
		lines = append(lines, "", theme.error.UnsetBold().Render("Only global administrators can see login security."), theme.muted.Render("Ask a global administrator to open this screen."))
	case !s.loaded && s.err != "":
		lines = append(lines, "", theme.error.UnsetBold().Render(fitText(s.err, l.bodyW)))
	case !s.loaded:
		lines = append(lines, "", theme.muted.Render("Loading login security…"))
	default:
		lines = append(lines, s.panels(theme, l, env.now()))
	}
	frame := screenFrame{
		Context: "Login security",
		Full:    true,
		Body:    securityShell(theme, env, "Login security", shellNavLoginSecurity, lines, s.Keys()),
	}
	if overlay := s.overlay(theme); overlay != "" {
		frame.Overlay = overlay
	}
	return frame
}

func (s loginSecurityScreen) titleLine(theme adminTheme, width int) string {
	right := ""
	if s.ready() {
		style := theme.muted
		switch s.backoff.Mode {
		case loginModeEnforce:
			style = theme.success.UnsetBold()
		case loginModeLog:
			style = theme.warning.UnsetBold()
		}
		right = theme.muted.Render("backoff mode: ") + style.Render(sanitizeFieldText(s.backoff.Mode, false))
	}
	return spreadText(theme.subheading.Render("Login security"), right, width)
}

func (s loginSecurityScreen) noticeLine(theme adminTheme, width int) string {
	switch {
	case s.busyText() != "":
		return theme.warning.UnsetBold().Render(fitText(s.busyText(), width))
	case s.moreErr != "":
		return theme.error.UnsetBold().Render(fitText(s.moreErr, width))
	case s.noticeFailed:
		return theme.error.UnsetBold().Render(fitText(s.notice, width))
	}
	return theme.success.UnsetBold().Render(fitText(s.notice, width))
}

// ---- summary strip ----

type loginStat struct {
	label string
	value string
	style lipgloss.Style
	note  string
}

func (s loginSecurityScreen) stats(theme adminTheme) []loginStat {
	if !s.ready() {
		return []loginStat{
			{"FAILED LOGINS, 15 MIN", "…", theme.muted, ""},
			{"DISTINCT IPS", "…", theme.muted, ""},
			{"DISTINCT ACCOUNTS", "…", theme.muted, ""},
			{"ACCOUNTS WAITING", "…", theme.muted, ""},
		}
	}
	value := func(n int64, warn bool) (string, lipgloss.Style) {
		style := theme.text.Bold(true)
		switch {
		case n == 0:
			style = theme.muted
		case warn:
			style = theme.warning
		}
		return fmt.Sprint(n), style
	}
	failures, failuresStyle := value(s.summary.Failures, loginFailuresWarn(s.summary.Failures))
	ips, ipsStyle := value(int64(s.summary.DistinctIPs), false)
	accounts, accountsStyle := value(int64(s.summary.DistinctAccounts), false)
	waiting, waitingStyle := value(int64(s.summary.AccountsInBackoff), loginWaitingWarn(s.summary.AccountsInBackoff, s.backoff.Mode))
	stat := loginStat{"ACCOUNTS WAITING", waiting, waitingStyle, ""}
	if s.backoff.Mode == loginModeLog || s.backoff.Mode == loginModeOff {
		stat.style, stat.note = theme.muted, "log only, nobody is delayed"
		if s.backoff.Mode == loginModeOff {
			stat.note = "off, nobody is delayed"
		}
	}
	return []loginStat{
		{"FAILED LOGINS, 15 MIN", failures, failuresStyle, ""},
		{"DISTINCT IPS", ips, ipsStyle, ""},
		{"DISTINCT ACCOUNTS", accounts, accountsStyle, ""},
		stat,
	}
}

func (s loginSecurityScreen) stripLines(theme adminTheme, l loginLayout) []string {
	stats := s.stats(theme)
	if !l.tiles {
		parts := make([]string, 0, len(stats))
		for _, stat := range stats {
			part := theme.muted.Bold(true).Render(stat.label) + " " + stat.style.Render(stat.value)
			if stat.note != "" {
				part += theme.muted.Render(" (" + stat.note + ")")
			}
			parts = append(parts, part)
		}
		return []string{fitText(strings.Join(parts, theme.muted.Render("   ")), l.bodyW)}
	}
	const gap = 2
	available := max(l.bodyW-gap*(len(stats)-1), len(stats))
	tiles := make([]string, 0, 2*len(stats))
	for i, stat := range stats {
		width := available / len(stats)
		if i < available%len(stats) {
			width++
		}
		if i > 0 {
			tiles = append(tiles, strings.Repeat(" ", gap))
		}
		value := stat.style.Render(stat.value)
		if stat.note != "" {
			value += theme.muted.Render("  " + stat.note)
		}
		tiles = append(tiles, userPanel(theme, width, loginTileRows, []string{theme.muted.Bold(true).Render(stat.label), value}))
	}
	return []string{lipgloss.JoinHorizontal(lipgloss.Top, tiles...)}
}

// ---- panels ----

func (s loginSecurityScreen) panels(theme adminTheme, l loginLayout, now time.Time) string {
	settings := s.settingsPanel(theme, l)
	waiting := s.waitingPanel(theme, l)
	failures := s.failuresPanel(theme, l, now)
	if l.wide {
		left := lipgloss.JoinVertical(lipgloss.Left, settings, waiting)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", loginColumnGap), failures)
	}
	return lipgloss.JoinVertical(lipgloss.Left, settings, waiting, failures)
}

// loginPanelBox draws a bordered panel exactly w by h cells with its title and
// a hint in the top border and a note in the bottom one. The focused panel's
// border and title take the accent colour.
func loginPanelBox(theme adminTheme, title, hint, bottom string, w, h int, focused bool, body []string) string {
	w, h = max(w, 6), max(h, 2)
	border := lipgloss.NewStyle().Foreground(theme.borderColor)
	titleStyle := theme.muted.Bold(true)
	if focused {
		border = lipgloss.NewStyle().Foreground(theme.accent.GetForeground())
		titleStyle = theme.accent
	}
	// edge draws a border row: "╭─ LEFT ───────── RIGHT ─╮". The right label is
	// dropped when it does not fit next to the left one.
	edge := func(corner, closer, leftLabel, rightLabel string) string {
		room := w - 2
		var out string
		switch {
		case leftLabel == "" && rightLabel == "":
			out = border.Render(strings.Repeat("─", room))
		default:
			leftLabel = fitText(leftLabel, max(room-4, 1))
			lw := lipgloss.Width(leftLabel)
			if rightLabel != "" && room-lw-lipgloss.Width(rightLabel)-6 >= 1 {
				fill := room - lw - lipgloss.Width(rightLabel) - 6
				out = border.Render("─ ") + leftLabel + border.Render(" "+strings.Repeat("─", fill)+" ") + rightLabel + border.Render(" ─")
			} else {
				out = border.Render("─ ") + leftLabel + border.Render(" "+strings.Repeat("─", max(room-lw-3, 0)))
			}
		}
		return border.Render(corner) + out + border.Render(closer)
	}
	lines := []string{edge("╭", "╮", titleStyle.Render(title), muteIf(theme, hint))}
	inner := max(w-4, 1)
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		lines = append(lines, border.Render("│")+" "+padText(line, inner)+" "+border.Render("│"))
	}
	lines = append(lines, edge("╰", "╯", muteIf(theme, bottom), ""))
	return strings.Join(lines, "\n")
}

func (s loginSecurityScreen) settingsPanel(theme adminTheme, l loginLayout) string {
	b := s.backoff
	mode := theme.muted.Render(sanitizeFieldText(b.Mode, false))
	switch b.Mode {
	case loginModeEnforce:
		mode = theme.success.UnsetBold().Render(b.Mode)
	case loginModeLog:
		mode = theme.warning.UnsetBold().Render(b.Mode)
	}
	proxies := theme.muted.Render("configured: no")
	if b.TrustedProxiesConfigured {
		proxies = theme.success.UnsetBold().Render("configured: yes")
	}
	rows := [][2]string{
		{"Backoff mode", mode},
		{"Failure threshold", fmt.Sprintf("%d failures per account", b.Threshold)},
		{"Base delay", loginDelayText(b.BaseSeconds)},
		{"Max delay", loginDelayText(b.MaxSeconds)},
		{"Concurrency limit", fmt.Sprintf("%d logins checked at once", b.MaxConcurrency)},
		{"Trusted proxies", proxies},
	}
	inner := max(l.settings.w-4, 1)
	hint := loginSettingsHint
	var body []string
	if l.wide {
		for _, row := range rows {
			body = append(body, securityRow(theme, row[0], row[1], inner))
		}
		body = append(body, theme.muted.Render(loginSettingsHint))
		hint = ""
	} else {
		colW := max((inner-loginColGap)/2, 1)
		for i := 0; i < loginSettingsRowsStacked; i++ {
			left := padText(securityRow(theme, rows[i][0], rows[i][1], colW), colW)
			right := securityRow(theme, rows[i+loginSettingsRowsStacked][0], rows[i+loginSettingsRowsStacked][1], max(inner-colW-loginColGap, 1))
			body = append(body, left+strings.Repeat(" ", loginColGap)+right)
		}
	}
	return loginPanelBox(theme, "EFFECTIVE SETTINGS", hint, "", l.settings.w, l.settings.h, false, body)
}

// waitingHeader is the TIME LEFT column's title: in log mode nobody is delayed,
// so the accounts only WOULD WAIT.
func (s loginSecurityScreen) waitingHeader() string {
	if s.backoff.Mode == loginModeEnforce {
		return "TIME LEFT"
	}
	return "WOULD WAIT"
}

func (s loginSecurityScreen) waitingPanel(theme adminTheme, l loginLayout) string {
	inner := max(l.waiting.w-4, 1)
	nameW := max(inner-loginMarkerWidth-loginColGap-loginColFails-loginColGap-loginColTimeLeft, 8)
	gap := strings.Repeat(" ", loginColGap)
	header := theme.muted.Bold(true).Render(strings.Repeat(" ", loginMarkerWidth) + padText("ACCOUNT", nameW) + gap +
		fmt.Sprintf("%*s", loginColFails, "FAILS") + gap + padText(s.waitingHeader(), loginColTimeLeft))
	body := []string{header}
	rows := s.backoff.Accounts
	if len(rows) == 0 {
		body = append(body, theme.muted.Render("Nobody is waiting."))
	}
	focused := s.focus == loginPanelWaiting
	start, end := grantWindow(boundedIndex(s.selWaiting, len(rows)), len(rows), s.waitingDataRows(l))
	for i := start; i < end; i++ {
		account := rows[i]
		plain := strings.Repeat(" ", loginMarkerWidth) + padText(loginAccountName(account.Account), nameW) + gap +
			fmt.Sprintf("%*d", loginColFails, account.Failures) + gap + padText(loginDelayText(account.RemainingSeconds), loginColTimeLeft)
		switch {
		case focused && i == boundedIndex(s.selWaiting, len(rows)):
			body = append(body, theme.selected.UnsetPadding().Render("▸ "+plain[loginMarkerWidth:]))
		case s.backoff.Mode != loginModeEnforce:
			body = append(body, theme.muted.Render(plain))
		default:
			body = append(body, theme.text.Render(plain))
		}
	}
	hint := s.waitingHint(focused)
	bottom := ""
	if l.wide {
		bottom = "Time left grows up to the max delay."
	}
	return loginPanelBox(theme, "ACCOUNTS WAITING", hint, bottom, l.waiting.w, l.waiting.h, focused, body)
}

func (s loginSecurityScreen) failuresPanel(theme adminTheme, l loginLayout, now time.Time) string {
	inner := max(l.failures.w-4, 1)
	cols := loginFailureColumns(inner)
	gap := strings.Repeat(" ", loginColGap)
	var body []string
	if s.filtering {
		body = append(body, theme.muted.Render("filter: ")+theme.text.Render(s.filterDraft)+theme.accent.Render("▏"))
	}
	body = append(body, theme.muted.Bold(true).Render(strings.Repeat(" ", loginMarkerWidth)+padText("LAST SEEN", cols.seen)+gap+padText("ENDPOINT", loginColEndpoint)+gap+
		padText("REASON", loginColReason)+gap+padText("ACCOUNT", cols.account)+gap+padText("IP", cols.ip)+gap+fmt.Sprintf("%*s", loginColCount, "COUNT")))

	rows := s.visibleFailures()
	dataRows := s.failureDataRows(l)
	focused := s.focus == loginPanelFailures
	selected := boundedIndex(s.selFail, len(rows))
	start, end := grantWindow(selected, len(rows), dataRows)
	switch {
	case len(rows) == 0 && strings.TrimSpace(s.filter) != "":
		body = append(body, theme.muted.Render("No failures match the filter."))
	case len(rows) == 0:
		body = append(body, theme.muted.Render("No failed logins in the last 24 hours."))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		plain := strings.Repeat(" ", loginMarkerWidth) +
			padText(loginFailureTime(row.LastSeen, now), cols.seen) + gap +
			padText(loginEndpointLabel(row.Endpoint), loginColEndpoint) + gap +
			padText(loginReasonLabel(row.Reason), loginColReason) + gap +
			padText(loginFailureAccount(row.Account), cols.account) + gap +
			padText(sanitizeFieldText(row.IP, false), cols.ip) + gap +
			fmt.Sprintf("%*d", loginColCount, row.Count)
		var line string
		if focused && i == selected {
			line = theme.selected.UnsetPadding().Render("▸ " + plain[loginMarkerWidth:])
		} else {
			line = theme.text.Render(plain)
		}
		body = append(body, line+" "+loginScrollCell(theme, i-start, dataRows, start, len(rows)))
	}
	return loginPanelBox(theme, "RECENT FAILURES", s.failuresHint(), s.failuresBottom(start, end, len(rows)), l.failures.w, l.failures.h, focused, body)
}

// loginFailureAccount is the account column: the name, the chip for an unknown
// username, or a dash for a bearer failure (no account).
func loginFailureAccount(account string) string {
	if account == "" {
		return "-"
	}
	return loginAccountName(account)
}

// loginFailureCols are the flexible column widths of the failures table.
type loginFailureCols struct{ seen, account, ip int }

func loginFailureColumns(inner int) loginFailureCols {
	fixed := loginMarkerWidth + loginColEndpoint + loginColReason + loginColCount + 6*loginColGap
	seen := loginColSeen
	rest := inner - fixed - seen
	ip := min(loginIPMaxWidth, max(rest*55/100, 12))
	account := max(rest-ip, 8)
	return loginFailureCols{seen: seen, account: account, ip: ip}
}

// loginScrollCell is the scrollbar cell of failure row i of a window of rows
// rows: the thumb marks where the window sits in total rows.
func loginScrollCell(theme adminTheme, i, rows, start, total int) string {
	if total <= rows || rows < 1 {
		return " "
	}
	thumb := max(rows*rows/total, 1)
	top := 0
	if total > rows {
		top = start * (rows - thumb) / (total - rows)
	}
	if i >= top && i < top+thumb {
		return theme.muted.Render("█")
	}
	return lipgloss.NewStyle().Foreground(theme.borderColor).Render("│")
}

func (s loginSecurityScreen) failuresHint() string {
	if s.filter != "" {
		return "/ filter: " + printableText(s.filter)
	}
	return "/ filter"
}

func (s loginSecurityScreen) failuresBottom(start, end, total int) string {
	if total == 0 {
		return "newest first"
	}
	more := ""
	if s.next != "" {
		more = "+"
	}
	text := fmt.Sprintf("newest first  ▾ %d-%d of %d%s", start+1, end, total, more)
	if s.loadingMore {
		text += "  loading more…"
	}
	return text
}

// ---- reset hooks (filled in by the reset unit) ----

// busyText is the progress line of a change on its way.
func (s loginSecurityScreen) busyText() string { return "" }

// waitingHint is the hint in the waiting panel's border.
func (s loginSecurityScreen) waitingHint(focused bool) string { return "" }

// overlay is the dialog in front, if any.
func (s loginSecurityScreen) overlay(theme adminTheme) string { return "" }

// muteIf renders a border label muted, or nothing for an empty one.
func muteIf(theme adminTheme, text string) string {
	if text == "" {
		return ""
	}
	return theme.muted.Render(text)
}

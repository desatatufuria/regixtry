package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"regixtry/internal/ports"
)

// trivySweep is the LAST SWEEP card's data: what the latest scheduled sweep
// did, read from the "sweep" section of the Trivy feature page (the same
// object the status JSON carries). A server without it has no sweep to show.
type trivySweep struct {
	finished    time.Time // zero while unknown
	visited     string
	unchanged   string
	queued      string
	errors      int
	dbRefresh   string // "ok", "never" or "failed: <reason>"
	dbRefreshed time.Time
	dbChanged   bool
}

// trivySweepFromPage reads the sweep section; ok is false when the page has
// none (no sweep has run yet, or an older server).
func trivySweepFromPage(page ports.FeaturePage) (trivySweep, bool) {
	found := false
	for _, section := range page.Sections {
		if section.ID == "sweep" {
			found = true
		}
	}
	if !found {
		return trivySweep{}, false
	}
	fields := securityPageFields(page, "sweep")
	parse := func(text string) time.Time {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(text))
		if err != nil {
			return time.Time{}
		}
		return at
	}
	errCount, _ := strconv.Atoi(fields["Errors"])
	changed, _ := strconv.ParseBool(fields["DB Changed"])
	return trivySweep{
		finished:    parse(fields["Finished"]),
		visited:     adminFirstNonEmpty(fields["Visited"], "—"),
		unchanged:   adminFirstNonEmpty(fields["Skipped Unchanged"], "—"),
		queued:      adminFirstNonEmpty(fields["Queued"], "—"),
		errors:      errCount,
		dbRefresh:   adminFirstNonEmpty(fields["DB Refresh"], "never"),
		dbRefreshed: parse(fields["DB Refreshed At"]),
		dbChanged:   changed,
	}, true
}

// trivyNextSweep estimates the next scheduled sweep from the last one and the
// configured interval, in hours or days.
func trivyNextSweep(sweep trivySweep, config map[string]string, now time.Time) string {
	if scheduled, _ := strconv.ParseBool(config["Schedule Enabled"]); !scheduled {
		return "scheduled scans off"
	}
	interval, err := time.ParseDuration(strings.TrimSpace(config["Interval"]))
	if err != nil || interval <= 0 || sweep.finished.IsZero() {
		return ""
	}
	left := sweep.finished.Add(interval).Sub(now)
	if left <= 0 {
		return "next sweep due"
	}
	return "next sweep in about " + trivyHoursDays(left)
}

// trivyHoursDays words a span in hours or days, never seconds: "<1 h", "5 h",
// "3 d".
func trivyHoursDays(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "<1 h"
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d/time.Hour))
	}
	return fmt.Sprintf("%d d", int(d/(24*time.Hour)))
}

// sweepCard is the LAST SWEEP panel and its height: a title and two lines, or
// one line when there is no sweep to show.
func (s trivyConfigScreen) sweepCard(theme adminTheme, env screenEnv, width int) (string, int) {
	sweep, ok := trivySweepFromPage(s.page)
	if !ok {
		return securityPanel(theme, "LAST SWEEP", width, 4, []string{theme.muted.Render("no sweep yet")}), 4
	}
	now := env.now()
	first := theme.text.Render(sweep.finishedText(now)+" · "+sweep.countsText()) + theme.muted.Render(" · ") + sweep.errorsText(theme)
	second := sweep.refreshText(theme, now)
	if sweep.dbChanged {
		second += theme.muted.Render(" · DB changed, digests rescanned")
	} else {
		second += theme.muted.Render(" · DB unchanged, nothing rescanned")
	}
	if next := trivyNextSweep(sweep, securityPageFields(s.page, "config"), now); next != "" {
		second += theme.muted.Render(" · " + next)
	}
	return securityPanel(theme, "LAST SWEEP", width, 5, []string{first, second}), 5
}

// sweepSummary is the card squeezed into one muted line.
func (s trivyConfigScreen) sweepSummary(theme adminTheme, env screenEnv) string {
	sweep, ok := trivySweepFromPage(s.page)
	if !ok {
		return "last sweep: no sweep yet"
	}
	text := fmt.Sprintf("last sweep %s: %s visited, %s unchanged, %s queued",
		strings.TrimPrefix(sweep.finishedText(env.now()), "finished "), sweep.visited, sweep.unchanged, sweep.queued)
	if sweep.errors > 0 {
		text += ", " + plural(sweep.errors, "error")
	}
	switch {
	case strings.HasPrefix(sweep.dbRefresh, "failed"):
		text += ", DB refresh failed, results may be stale"
	case sweep.dbRefresh == "never":
		text += ", DB not refreshed"
	default:
		text += ", DB ok"
	}
	return text
}

func (t trivySweep) finishedText(now time.Time) string {
	if t.finished.IsZero() {
		return "finished time unknown"
	}
	return "finished " + scanAge(t.finished, now)
}

func (t trivySweep) countsText() string {
	return fmt.Sprintf("visited %s · unchanged %s · queued %s", t.visited, t.unchanged, t.queued)
}

func (t trivySweep) errorsText(theme adminTheme) string {
	if t.errors > 0 {
		return theme.warning.Render(plural(t.errors, "error"))
	}
	return theme.muted.Render("no errors")
}

// refreshPlain words the once-per-sweep vulnerability DB download.
func (t trivySweep) refreshPlain(now time.Time) string {
	switch {
	case strings.HasPrefix(t.dbRefresh, "failed"):
		text := "DB refresh failed, results may be stale"
		if reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t.dbRefresh, "failed"), ":")); reason != "" {
			text += " (" + reason + ")"
		}
		return text
	case t.dbRefresh == "never":
		return "DB not refreshed"
	case !t.dbRefreshed.IsZero():
		return "DB refresh ok " + scanAge(t.dbRefreshed, now)
	}
	return "DB refresh ok"
}

func (t trivySweep) refreshText(theme adminTheme, now time.Time) string {
	text := t.refreshPlain(now)
	switch {
	case strings.HasPrefix(t.dbRefresh, "failed"):
		return theme.error.Render(text)
	case t.dbRefresh == "never":
		return theme.muted.Render(text)
	}
	return theme.success.Render(text)
}

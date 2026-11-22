package tui

import (
	"sort"

	appregixtry "regixtry/internal/app/regixtry"
)

// sortMode is the shared client-side sort cycle of the Tags and the Projects
// screens: "by name ascending -> by date descending", then back. Sorting is
// entirely client-side: both screens fetch their full page in one call
// (limit 100, no cursor), so there is no keyset-pagination cursor tied to
// ORDER BY to break.
type sortMode int

const (
	sortByNameAsc sortMode = iota
	sortByDateDesc
)

// next cycles name-ascending -> date-descending -> back to name-ascending.
func (s sortMode) next() sortMode {
	if s == sortByDateDesc {
		return sortByNameAsc
	}
	return sortByDateDesc
}

// label is the short, discoverable descriptor shown in each screen's own
// subheading (e.g. "Tags (sort: name)") -- this codebase's established
// text-only convention, no icons/glyphs (tagSignedLabel/scanPolicyBadge's
// own precedent).
func (s sortMode) label() string {
	if s == sortByDateDesc {
		return "date"
	}
	return "name"
}

// sortTagItems sorts tags in place by mode -- by Name ascending, or by
// CreatedAt descending (most recently pushed first). Stable so tags sharing
// an identical CreatedAt (e.g. multiple tags on the same digest) keep a
// deterministic relative order across repeated sorts.
func sortTagItems(tags []appregixtry.TagDetails, mode sortMode) {
	switch mode {
	case sortByDateDesc:
		sort.SliceStable(tags, func(i, j int) bool { return tags[j].CreatedAt.Before(tags[i].CreatedAt) })
	default:
		sort.SliceStable(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	}
}

// sortProjectItems mirrors sortTagItems exactly, for the Projects screen:
// by Name ascending, or by aggregate LastPushed descending (most recently
// pushed project first).
func sortProjectItems(projects []projectSummary, mode sortMode) {
	switch mode {
	case sortByDateDesc:
		sort.SliceStable(projects, func(i, j int) bool { return projects[j].LastPushed.Before(projects[i].LastPushed) })
	default:
		sort.SliceStable(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	}
}

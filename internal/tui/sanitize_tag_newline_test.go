package tui

import (
	"strings"
	"testing"

	appregixtry "regixtry/internal/app/regixtry"

	"github.com/charmbracelet/x/ansi"
)

// A pusher can store a tag name with interior newlines. The sanitizer keeps
// newlines and the width helpers count them as zero width, so a raw name used
// to split its table row and push the other rows, the bottom border and the
// footnote out of the frame.
const newlineTag = "a\n\n\n\n\n\n\n\n\n\n\n\nINJECTED"

const tagsFootnote = "SIGNED: yes = signature verified"

func renderTagsFrame(t *testing.T, items []appregixtry.TagDetails, width, height int) string {
	t.Helper()
	env := consoleEnvFor(nil, width, height)
	return consoleRender(tagsScreenWith(items, sortByNameAsc), env)
}

func TestConsoleTagsBoardKeepsOneRowPerTagWhateverTheName(t *testing.T) {
	for _, size := range consoleSizes {
		clean := renderTagsFrame(t, tagsFixture(), size.w, size.h)
		hostileItems := append(tagsFixture(), appregixtry.TagDetails{Name: newlineTag, CreatedAt: consoleTime, PushedBy: "mallory"})
		got := renderTagsFrame(t, hostileItems, size.w, size.h)

		if gotLines, wantLines := strings.Count(got, "\n"), strings.Count(clean, "\n"); gotLines != wantLines {
			t.Fatalf("%dx%d: frame has %d line breaks, want %d (the same as without the hostile tag)", size.w, size.h, gotLines, wantLines)
		}
		if !strings.Contains(got, tagsFootnote) {
			t.Fatalf("%dx%d: footnote pushed out of the frame:\n%s", size.w, size.h, got)
		}
		// The three ordinary tags sort after "a..." and must stay visible.
		for _, name := range []string{"latest", "1.0", "0.9"} {
			if !strings.Contains(got, name) {
				t.Fatalf("%dx%d: tag %q pushed out of the frame:\n%s", size.w, size.h, name, got)
			}
		}
		// The hostile name is one row: its pieces share a line with its columns.
		var rowLine string
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "INJECTED") {
				rowLine = line
			}
		}
		if rowLine == "" || !strings.Contains(rowLine, "mallory") {
			t.Fatalf("%dx%d: hostile row is not drawn on a single line with its columns:\n%s", size.w, size.h, got)
		}
	}
}

func TestConsoleTagsDeleteDialogNeutralisesNewlinesButDeletesExactName(t *testing.T) {
	s := tagsScreenWith([]appregixtry.TagDetails{{Name: newlineTag, CreatedAt: consoleTime}}, sortByNameAsc)
	prompt := s.deleteConfirm(s.items[0])

	for _, shown := range []string{prompt.title, prompt.target, prompt.submitting} {
		if strings.ContainsAny(shown, "\r\n") {
			t.Fatalf("dialog text carries a line break: %q", shown)
		}
		if !strings.Contains(shown, "INJECTED") {
			t.Fatalf("dialog text lost the tag name: %q", shown)
		}
	}
	view := ansi.Strip(prompt.view(newAdminTheme()))
	if strings.Contains(view, "a\n\n") {
		t.Fatalf("rendered dialog still carries the raw newlines:\n%s", view)
	}

	// The command keeps the exact stored name: sanitizing is display only.
	service := newConsoleService()
	env := consoleEnvFor(service, 150, 24)
	msg, ok := prompt.onConfirm(env)().(tagDeletedMsg)
	if !ok || msg.tag != newlineTag || len(service.deletedTags) != 1 || !strings.HasSuffix(service.deletedTags[0], newlineTag) {
		t.Fatalf("delete command targets %q / %q, want the exact stored name %q", msg.tag, service.deletedTags, newlineTag)
	}
}

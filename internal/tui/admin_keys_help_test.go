package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

func TestShortHelpViewFormat(t *testing.T) {
	t.Parallel()

	binding := func(label string, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(label), key.WithHelp(label, desc))
	}
	disabled := func(label string, desc string) key.Binding {
		return key.NewBinding(key.WithKeys(label), key.WithHelp(label, desc), key.WithDisabled())
	}

	tests := []struct {
		name string
		keys screenKeys
		want string
	}{
		{name: "no bindings renders nothing", keys: screenKeys{}, want: ""},
		{name: "single binding has no separator", keys: screenKeys{short: []key.Binding{binding("Esc", "cancel")}}, want: "Esc: cancel"},
		{
			name: "bindings join with a pipe separator",
			keys: screenKeys{short: []key.Binding{binding("Enter", "save"), binding("Tab", "next field"), binding("Esc", "cancel")}},
			want: "Enter: save | Tab: next field | Esc: cancel",
		},
		{
			name: "disabled bindings are skipped without stray separators",
			keys: screenKeys{short: []key.Binding{disabled("A", "first"), binding("B", "second"), disabled("C", "third"), binding("D", "fourth"), disabled("E", "last")}},
			want: "B: second | D: fourth",
		},
		{
			name: "only disabled bindings renders nothing",
			keys: screenKeys{short: []key.Binding{disabled("A", "first"), disabled("B", "second")}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := shortHelpView(newAdminTheme(), tt.keys); got != tt.want {
				t.Fatalf("shortHelpView() = %q, want %q", got, tt.want)
			}
		})
	}
}

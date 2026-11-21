package tui

import (
	"testing"

	"regixtry/internal/ports"
)

func TestFeatureActionForKeyCharacterization(t *testing.T) {
	page := ports.FeaturePage{Actions: []ports.FeatureAction{
		{ID: "enable"}, {ID: "disable"}, {ID: "install-runtime"},
		{ID: "upgrade-runtime"}, {ID: "rollback-runtime"},
	}}
	tests := []struct {
		name   string
		msg    keyMsg
		wantID string
	}{
		{"e enables", testKeyRunes("e"), "enable"},
		{"x disables", testKeyRunes("x"), "disable"},
		{"i installs", testKeyRunes("i"), "install-runtime"},
		{"u upgrades", testKeyRunes("u"), "upgrade-runtime"},
		{"b rolls back", testKeyRunes("b"), "rollback-runtime"},
		{"upper-case E enables", testKeyRunes("E"), "enable"},
		{"upper-case X disables", testKeyRunes("X"), "disable"},
		{"multi-rune input uses the first rune", testKeyRunes("ex"), "enable"},
		{"multi-rune input with unmapped first rune", testKeyRunes("zx"), ""},
		{"unmapped rune", testKeyRunes("z"), ""},
		{"empty text", testKeyRunes(""), ""},
		{"space", testKeySpace(), ""},
		{"enter", testKeyEnter(), ""},
		{"esc", testKeyEsc(), ""},
		{"tab", testKeyTab(), ""},
		{"arrow up", testKeyUp(), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := featureActionForKey(tt.msg, page)
			if ok != (tt.wantID != "") || action.ID != tt.wantID {
				t.Fatalf("featureActionForKey = (%q, %v), want (%q, %v)", action.ID, ok, tt.wantID, tt.wantID != "")
			}
		})
	}

	t.Run("mapped key without matching page action", func(t *testing.T) {
		action, ok := featureActionForKey(testKeyRunes("e"), ports.FeaturePage{})
		if ok || action.ID != "" {
			t.Fatalf("featureActionForKey = (%q, %v), want no action", action.ID, ok)
		}
	})
}

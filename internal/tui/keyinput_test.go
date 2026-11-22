package tui

import "testing"

func TestKeyText(t *testing.T) {
	tests := []struct {
		name string
		msg  keyMsg
		want string
	}{
		{"typed text", testKeyRunes("abc"), "abc"},
		{"space", testKeySpace(), " "},
		{"multi-byte text", testKeyRunes("ñé"), "ñé"},
		{"enter has no text", testKeyEnter(), ""},
		{"backspace has no text", testKeyBackspace(), ""},
		{"ctrl key has no text", testKeyCtrl('c'), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyText(tt.msg); got != tt.want {
				t.Fatalf("keyText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsTextKey(t *testing.T) {
	tests := []struct {
		name string
		msg  keyMsg
		want bool
	}{
		{"typed text", testKeyRunes("a"), true},
		{"space", testKeySpace(), true},
		{"enter", testKeyEnter(), false},
		{"tab", testKeyTab(), false},
		{"arrow", testKeyUp(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTextKey(tt.msg); got != tt.want {
				t.Fatalf("isTextKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSpaceKey(t *testing.T) {
	tests := []struct {
		name string
		msg  keyMsg
		want bool
	}{
		{"space", testKeySpace(), true},
		{"letter", testKeyRunes("a"), false},
		{"two characters", testKeyRunes("  "), false},
		{"enter", testKeyEnter(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSpaceKey(tt.msg); got != tt.want {
				t.Fatalf("isSpaceKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKeyString(t *testing.T) {
	tests := []struct {
		name string
		msg  keyMsg
		want string
	}{
		{"ctrl+c", testKeyCtrl('c'), "ctrl+c"},
		{"letter", testKeyRunes("q"), "q"},
		{"tab", testKeyTab(), "tab"},
		{"enter", testKeyEnter(), "enter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyString(tt.msg); got != tt.want {
				t.Fatalf("keyString() = %q, want %q", got, tt.want)
			}
		})
	}
}

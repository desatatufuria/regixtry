package tui

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg is the only place production code names the Bubble Tea key message
// type, so a library upgrade changes this file alone.
type keyMsg = tea.KeyMsg

func isMoveUpKey(msg keyMsg) bool {
	return msg.Type == tea.KeyUp || msg.String() == "up" || isRuneKey(msg, 'k')
}

func isMoveDownKey(msg keyMsg) bool {
	return msg.Type == tea.KeyDown || msg.String() == "down" || isRuneKey(msg, 'j')
}

func isEnterKey(msg keyMsg) bool {
	return msg.Type == tea.KeyEnter || msg.String() == "enter"
}

func isTabKey(msg keyMsg) bool {
	return msg.Type == tea.KeyTab || msg.String() == "tab"
}

func isShiftTabKey(msg keyMsg) bool {
	return msg.Type == tea.KeyShiftTab || msg.String() == "shift+tab"
}

// isMoveLeftKey/isMoveRightKey back the scan history modal's Left/Right
// history paging (spec.md "Scan Execution History Navigation"). Deliberately
// no 'h'/'l' vim-style rune aliases (unlike isMoveUpKey/isMoveDownKey's 'k'/
// 'j'): 'l' is already the admin logout key.
func isMoveLeftKey(msg keyMsg) bool {
	return msg.Type == tea.KeyLeft || msg.String() == "left"
}

func isMoveRightKey(msg keyMsg) bool {
	return msg.Type == tea.KeyRight || msg.String() == "right"
}

func isEscKey(msg keyMsg) bool {
	return msg.Type == tea.KeyEsc || msg.String() == "esc"
}

func isBackspaceKey(msg keyMsg) bool {
	return msg.Type == tea.KeyBackspace || msg.String() == "backspace"
}

func isBackKey(msg keyMsg) bool {
	return isEscKey(msg) || isBackspaceKey(msg)
}

func isPgUpKey(msg keyMsg) bool {
	return msg.Type == tea.KeyPgUp || msg.String() == "pgup"
}

func isPgDnKey(msg keyMsg) bool {
	return msg.Type == tea.KeyPgDown || msg.String() == "pgdown"
}

func isHomeKey(msg keyMsg) bool {
	return msg.Type == tea.KeyHome || msg.String() == "home"
}

func isEndKey(msg keyMsg) bool {
	return msg.Type == tea.KeyEnd || msg.String() == "end"
}

func isRuneKey(msg keyMsg, candidates ...rune) bool {
	if len(msg.Runes) != 1 {
		return false
	}
	r := unicode.ToLower(msg.Runes[0])
	for _, candidate := range candidates {
		if r == unicode.ToLower(candidate) {
			return true
		}
	}
	return false
}

func isSpaceKey(msg keyMsg) bool {
	return isRuneKey(msg, ' ')
}

// isTextKey reports whether msg carries typed text, even if that text is empty.
func isTextKey(msg keyMsg) bool {
	return msg.Type == tea.KeyRunes
}

// keyText returns the typed text of a text-input key and "" for any other key.
func keyText(msg keyMsg) string {
	if msg.Type != tea.KeyRunes {
		return ""
	}
	return string(msg.Runes)
}

// keyString returns the library's canonical key name, such as "ctrl+c".
func keyString(msg keyMsg) string {
	return msg.String()
}

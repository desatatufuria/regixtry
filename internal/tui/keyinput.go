package tui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// keyMsg is the only place production code names the Bubble Tea key message
// type, so a library upgrade changes this file alone.
type keyMsg = tea.KeyPressMsg

func isMoveUpKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyUp) || keyString(msg) == "up" || isRuneKey(msg, 'k')
}

func isMoveDownKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyDown) || keyString(msg) == "down" || isRuneKey(msg, 'j')
}

func isEnterKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyEnter) || keyString(msg) == "enter"
}

func isTabKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyTab) || keyString(msg) == "tab"
}

func isShiftTabKey(msg keyMsg) bool {
	return keyString(msg) == "shift+tab"
}

// isMoveLeftKey/isMoveRightKey back the scan history modal's Left/Right
// history paging (spec.md "Scan Execution History Navigation"). Deliberately
// no 'h'/'l' vim-style rune aliases (unlike isMoveUpKey/isMoveDownKey's 'k'/
// 'j'): 'l' is already the admin logout key.
func isMoveLeftKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyLeft) || keyString(msg) == "left"
}

func isMoveRightKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyRight) || keyString(msg) == "right"
}

func isEscKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyEscape) || keyString(msg) == "esc"
}

func isBackspaceKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyBackspace) || keyString(msg) == "backspace"
}

func isBackKey(msg keyMsg) bool {
	return isEscKey(msg) || isBackspaceKey(msg)
}

func isPgUpKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyPgUp) || keyString(msg) == "pgup"
}

func isPgDnKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyPgDown) || keyString(msg) == "pgdown"
}

func isHomeKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyHome) || keyString(msg) == "home"
}

func isEndKey(msg keyMsg) bool {
	return isCode(msg, tea.KeyEnd) || keyString(msg) == "end"
}

func isRuneKey(msg keyMsg, candidates ...rune) bool {
	runeText := []rune(keyText(msg))
	if len(runeText) != 1 {
		return false
	}
	r := unicode.ToLower(runeText[0])
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

// isTextKey reports whether keyText(msg) is non-empty.
func isTextKey(msg keyMsg) bool {
	return keyText(msg) != ""
}

// keyText returns the typed text of a text-input key and "" for any other key.
// Alt+<printable> counts as that character, as it did under Bubble Tea v1
// (where it was a runes key carrying an Alt flag).
func keyText(msg keyMsg) string {
	if msg.Text != "" {
		return msg.Text
	}
	if msg.Mod == tea.ModAlt && unicode.IsPrint(msg.Code) {
		return string(msg.Code)
	}
	return ""
}

// isCode matches a special key by code, ignoring Alt (as Bubble Tea v1 did)
// but not Shift, Ctrl or other modifiers, which v1 reported as distinct keys.
func isCode(msg keyMsg, code rune) bool {
	return msg.Code == code && msg.Mod&^tea.ModAlt == 0
}

// keyString returns the library's canonical key name, such as "ctrl+c".
// A synthetic paste key (Code 0, Text set) stringifies as "[text]", as a v1
// paste did, so pasted text never matches a key name.
func keyString(msg keyMsg) string {
	if msg.Code == 0 && msg.Text != "" {
		return "[" + msg.Text + "]"
	}
	return msg.String()
}

// keyName lets key.Matches see the paste-safe string from keyString.
type keyName string

func (k keyName) String() string { return string(k) }

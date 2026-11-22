package tui

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// controlReplacement is what a control character becomes on screen: a visible
// middle dot, so a hostile string reads as damaged text instead of acting on
// the operator's terminal.
const controlReplacement = '·'

// sanitizeFrame makes text safe to write to the operator's terminal while
// keeping the renderer's own styling. Registry data (repository, tag, user and
// robot names, scan findings, manifest metadata, server error messages) is
// controlled by lower-trust principals and reaches the screen through many
// builders, so this runs at the shared render chokepoints (the shell body
// and the final frame) rather than per field.
//
//   - C0 controls (ESC, BEL, BS, CR, NUL...), DEL, C1 controls (0x80-0x9f,
//     including the single-byte CSI 0x9b) and invalid UTF-8 become '·'.
//   - Format characters (bidi overrides and isolates, zero-width characters,
//     BOM, tag characters) are dropped: they have no glyph to show.
//   - Tabs become four spaces; newlines survive (the frame is multi-line).
//   - The only escape sequences kept are well-formed SGR styling (ESC [ params
//     m) that cannot hide or blink text: lipgloss emits exactly that, and
//     nothing else, for our own styles. Anything else (cursor moves, screen
//     clears, OSC titles, clipboard writes and hyperlinks, DCS...) loses its
//     ESC and its payload is shown as plain text.
//
// Printable Unicode (multibyte, emoji, combining marks, wide CJK) is untouched.
func sanitizeFrame(text string) string {
	return sanitizeText(text, true, true)
}

// sanitizeDisplay is sanitizeFrame for a single plain string: no styling
// survives and newlines are replaced too.
func sanitizeDisplay(text string) string {
	return sanitizeText(text, false, false)
}

func sanitizeText(text string, keepSGR, keepNewline bool) string {
	if isCleanText(text, keepSGR, keepNewline) {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if keepSGR && text[i] == 0x1b {
			if n := safeSGRLength(text[i:]); n > 0 {
				out.WriteString(text[i : i+n])
				i += n
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		switch {
		case r == '\n' && keepNewline:
			out.WriteByte('\n')
		case r == '\t':
			out.WriteString("    ")
		case r == utf8.RuneError && size <= 1:
			out.WriteRune(controlReplacement)
		case unicode.IsControl(r) || r == '\u2028' || r == '\u2029':
			out.WriteRune(controlReplacement)
		case unicode.Is(unicode.Cf, r):
			// dropped
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// isCleanText is the allocation-free fast path: normal registry data has
// nothing to change.
func isCleanText(text string, keepSGR, keepNewline bool) bool {
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '\n' && keepNewline:
			case c == 0x1b && keepSGR && safeSGRLength(text[i:]) > 0:
				i += safeSGRLength(text[i:])
				continue
			case c < 0x20 || c == 0x7f:
				return false
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if (r == utf8.RuneError && size <= 1) || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || unicode.Is(unicode.Cf, r) {
			return false
		}
		i += size
	}
	return true
}

// safeSGRLength returns the byte length of the SGR sequence at the start of
// text (ESC [ digits/;/: m) when its attributes are safe, or 0. Conceal (8)
// and blink (5, 6) are refused: a pusher must not be able to hide or flash
// text in an admin's view. Extended colours (38/48/58 ; 5 ; n and ; 2 ; r ; g ; b)
// keep their sub-parameters out of that check.
func safeSGRLength(text string) int {
	if len(text) < 3 || text[0] != 0x1b || text[1] != '[' {
		return 0
	}
	const maxParams = 64
	end := 2
	for end < len(text) && end < maxParams+3 {
		c := text[end]
		if c == 'm' {
			break
		}
		if (c < '0' || c > '9') && c != ';' && c != ':' {
			return 0
		}
		end++
	}
	if end >= len(text) || text[end] != 'm' {
		return 0
	}
	tokens := strings.FieldsFunc(text[2:end], func(r rune) bool { return r == ';' || r == ':' })
	for i := 0; i < len(tokens); i++ {
		n, err := strconv.Atoi(tokens[i])
		if err != nil {
			return 0
		}
		switch n {
		case 5, 6, 8:
			return 0
		case 38, 48, 58:
			if i+1 < len(tokens) {
				switch tokens[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return end + 1
}

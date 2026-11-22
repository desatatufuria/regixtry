package tui

import (
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestSanitizeDisplay(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain ascii", "acme/api:v1.2.3", "acme/api:v1.2.3"},
		{"multibyte", "café ünï", "café ünï"},
		{"emoji", "ship 🚀 it 👨\u200d👩\u200d👧", "ship 🚀 it 👨👩👧"},
		{"combining marks", "éä", "éä"},
		{"wide cjk", "仓库/测试", "仓库/测试"},
		{"clear screen", "a\x1b[2Jb", "a·[2Jb"},
		{"osc title", "a\x1b]0;pwned\x07b", "a·]0;pwned·b"},
		{"osc 52 clipboard", "\x1b]52;c;aGk=\x1b\\", "·]52;c;aGk=·\\"},
		{"osc 8 hyperlink", "\x1b]8;;http://evil\x07x\x1b]8;;\x07", "·]8;;http://evil·x·]8;;·"},
		{"carriage return", "good\rBAD", "good·BAD"},
		{"backspace", "abc\b\b\bxyz", "abc···xyz"},
		{"c1 csi", "a\u009b2Jb", "a·2Jb"},
		{"nul", "a\x00b", "a·b"},
		{"del", "a\x7fb", "a·b"},
		{"bidi override", "evil\u202egpj.exe", "evilgpj.exe"},
		{"bidi isolate", "a\u2066b\u2069", "ab"},
		{"zero width", "a\u200bb\u200cc\ufeffd", "abcd"},
		{"newline replaced", "a\nb", "a·b"},
		{"tab expanded", "a\tb", "a    b"},
		{"line separator", "a\u2028b", "a·b"},
		{"invalid utf8", "a\xffb", "a·b"},
		{"sgr injection is not kept in plain mode", "\x1b[31mred", "·[31mred"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDisplay(tt.in); got != tt.want {
				t.Errorf("sanitizeDisplay(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeFrameKeepsOwnStylingOnly(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"reset and bold", "\x1b[1mhi\x1b[0m\n", "\x1b[1mhi\x1b[0m\n"},
		{"empty reset", "\x1b[mx", "\x1b[mx"},
		{"truecolor", "\x1b[38;2;1;2;3mx", "\x1b[38;2;1;2;3mx"},
		{"256 colour", "\x1b[48;5;200mx", "\x1b[48;5;200mx"},
		{"conceal refused", "\x1b[8mhidden", "·[8mhidden"},
		{"blink refused", "\x1b[5mx", "·[5mx"},
		{"cursor move refused", "\x1b[10;10Hx", "·[10;10Hx"},
		{"osc refused", "\x1b]0;t\x07", "·]0;t·"},
		{"newline kept", "a\nb", "a\nb"},
		{"cr dropped to dot", "a\r\nb", "a·\nb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFrame(tt.in); got != tt.want {
				t.Errorf("sanitizeFrame(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeWidthMatchesRenderedText(t *testing.T) {
	in := "a\x1b[2J\x1b]0;t\x07b\u202e仓"
	out := sanitizeDisplay(in)
	if ansi.StringWidth(out) != utf8.RuneCountInString(out)+1 { // 仓 is two columns
		t.Fatalf("width of %q = %d, runes %d", out, ansi.StringWidth(out), utf8.RuneCountInString(out))
	}
}

func TestSanitizeNeverEmitsControlRunes(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []rune{0, 7, 8, 9, 10, 13, 0x1b, 0x7f, 0x80, 0x9b, 0x9f, '[', ']', ';', 'm', 'J', '8', '5', '3',
		'a', 'é', '仓', '🚀', 0x200b, 0x200d, 0x202e, 0x2066, 0xfeff, 0x2028, 0xe0041, 0x301}
	deadline := time.Now().Add(2 * time.Second)
	for n := 0; n < 20000 && time.Now().Before(deadline); n++ {
		var b strings.Builder
		for range rng.Intn(24) {
			if rng.Intn(10) == 0 {
				b.WriteByte(byte(rng.Intn(256)))
				continue
			}
			b.WriteRune(alphabet[rng.Intn(len(alphabet))])
		}
		in := b.String()
		for _, out := range []string{sanitizeDisplay(in), ansi.Strip(sanitizeFrame(in))} {
			if !utf8.ValidString(out) {
				t.Fatalf("invalid utf8 from %q", in)
			}
			for _, r := range out {
				if r == '\n' && out != sanitizeDisplay(in) {
					continue
				}
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
					t.Fatalf("rune %U survived from %q", r, in)
				}
			}
		}
		if f := sanitizeFrame(in); strings.ContainsRune(strings.ReplaceAll(ansi.Strip(f), "\n", ""), 0x1b) {
			t.Fatalf("raw ESC survived from %q", in)
		}
	}
}

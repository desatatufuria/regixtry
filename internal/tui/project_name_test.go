package tui

import (
	"math/rand"
	"strings"
	"testing"

	regixtrydomain "regixtry/internal/domain/regixtry"
)

// TestProjectNameCheckAgreesWithTheDomain pins the mirror to the server's own
// validation: a name is valid here exactly when ParseProjectName accepts it.
func TestProjectNameCheckAgreesWithTheDomain(t *testing.T) {
	t.Parallel()
	names := []string{"", "a", "odabank", "odabank2", "a-b", "a.b", "a_b", "a--b", "a..b", "-a", "a-", ".a", "Odabank", "oda bank", "oda/bank", "/odabank",
		"odabank/", "ñandú", "a\x00b", strings.Repeat("a", 64), strings.Repeat("a", 65), "blobs", "manifests", "tags", "referrers", "grants",
		"repository-overrides", "members", "BLOBS", "blobs2", "9lives", "a\nb", " odabank", "odabank "}
	rng := rand.New(rand.NewSource(7))
	alphabet := []rune("abcxyz019._-/ AZ\t")
	for range 4000 {
		runes := make([]rune, rng.Intn(10))
		for i := range runes {
			runes[i] = alphabet[rng.Intn(len(alphabet))]
		}
		names = append(names, string(runes))
	}
	for _, name := range names {
		_, err := regixtrydomain.ParseProjectName(name)
		if valid := checkProjectName(name) == projectNameValid; valid != (err == nil) {
			t.Fatalf("name %q: valid here = %v, domain error = %v", name, valid, err)
		}
	}
}

func TestProjectNameMessagesFollowTheBoardPrecedence(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, want string }{
		{"", "Enter a project name."},
		{strings.Repeat("a", 65), "At most 64 characters (65 now)."},
		{strings.Repeat("A/", 40), "At most 64 characters (80 now)."}, // length before slash
		{"oda/bank", "A project name has no slash. Repositories go inside the project."},
		{"Oda/Bank", "A project name has no slash. Repositories go inside the project."}, // slash before charset
		{"Odabank", "Use lowercase letters, digits and . _ - only; start and end with a letter or digit."},
		{"-oda", "Use lowercase letters, digits and . _ - only; start and end with a letter or digit."},
		{"blobs", "blobs is reserved by the registry API. Pick another name."},
		{"repository-overrides", "repository-overrides is reserved by the registry API. Pick another name."},
		{"odabank", ""},
		{strings.Repeat("a", 64), ""},
	}
	for _, tc := range cases {
		if got := projectNameMessage(tc.name); got != tc.want {
			t.Errorf("projectNameMessage(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

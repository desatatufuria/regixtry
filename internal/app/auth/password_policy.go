package auth

import (
	_ "embed"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed common_passwords.txt
var commonPasswordsFile string

// commonPasswords is the embedded blocklist of the most common passwords,
// lowercase. It is applied wherever a password is set (bootstrap, create user,
// reset); passwords already stored are never re-checked.
var commonPasswords = loadCommonPasswords(commonPasswordsFile)

func loadCommonPasswords(raw string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, line := range strings.Split(raw, "\n") {
		line = foldForBlocklist(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = struct{}{}
	}
	return set
}

// foldForBlocklist is the form a password is matched in: NFKC-normalized (so
// fullwidth and other compatibility characters become their ASCII
// equivalents), lowercased, with zero-width/format characters and every
// Unicode space removed. It is used for the match only; the password that is
// hashed and stored is never altered.
func foldForBlocklist(password string) string {
	var b strings.Builder
	for _, r := range norm.NFKC.String(password) {
		switch {
		case unicode.IsSpace(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Zs, r):
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func isCommonPassword(password string) bool {
	_, common := commonPasswords[foldForBlocklist(password)]
	return common
}

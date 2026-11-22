package auth

import (
	_ "embed"
	"strings"
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
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = struct{}{}
	}
	return set
}

func isCommonPassword(password string) bool {
	_, common := commonPasswords[strings.ToLower(strings.TrimSpace(password))]
	return common
}

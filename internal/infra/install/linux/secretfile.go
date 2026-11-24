package linux

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const redactedPassword = "xxxxx"

// writeSecretFile writes body to path so that no reader other than the mode
// allows can ever observe it: the bytes go to a fresh O_EXCL temp file created
// with the final mode in the target directory, then rename(2) replaces the
// target atomically. Replacing (rather than truncating) also tightens a file a
// previous installer left at 0644, and never follows a symlink at path.
func writeSecretFile(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	for attempt := 0; attempt < 8; attempt++ {
		tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.%d.%d.tmp", filepath.Base(path), os.Getpid(), attempt, time.Now().UnixNano()))
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := f.Write(body)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(tmp)
			return werr
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
	return fmt.Errorf("create temporary secret file next to %s: too many collisions", path)
}

func (b *Bootstrapper) secretWriter() func(string, []byte, os.FileMode) error {
	if b.writeSecret != nil {
		return b.writeSecret
	}
	return writeSecretFile
}

// keywordPasswordAny finds every password key, however its value is written.
var keywordPasswordAny = regexp.MustCompile(`(?i)\bpassword[ \t\n\v\f\r]*=`)

const dsnSpace = " \t\n\v\f\r"

// redactKeywordValueDSN masks every password value of a keyword/value DSN. It
// reads the string with the grammar of pgx and libpq: a key runs to the next
// "=", whitespace around "=" is ignored, a value starting with a single quote
// runs to the next unescaped single quote, and any other value runs to the first
// unescaped whitespace, keeping embedded quotes as literal characters. A
// backslash escapes whatever follows it. Text outside password values is copied
// unchanged. It reports false when the string does not parse under that grammar
// or a password value is empty, so the caller can record nothing instead of a
// possibly partial secret. It also returns how many password values it masked.
func redactKeywordValueDSN(dsn string) (string, int, bool) {
	var out strings.Builder
	masked := 0
	i, n := 0, len(dsn)
	for i < n {
		eq := strings.IndexByte(dsn[i:], '=')
		if eq < 0 {
			return "", 0, false
		}
		key := strings.Trim(dsn[i:i+eq], dsnSpace)
		if strings.ContainsAny(key, dsnSpace) {
			return "", 0, false
		}
		isPassword := strings.EqualFold(key, "password")
		valStart := i + eq + 1
		for valStart < n && strings.IndexByte(dsnSpace, dsn[valStart]) >= 0 {
			valStart++
		}
		out.WriteString(dsn[i:valStart])
		if valStart == n {
			if isPassword {
				return "", 0, false
			}
			break
		}
		valEnd := valStart
		if dsn[valStart] != '\'' {
			for ; valEnd < n && strings.IndexByte(dsnSpace, dsn[valEnd]) < 0; valEnd++ {
				if dsn[valEnd] == '\\' {
					valEnd++
				}
			}
			valEnd = min(valEnd, n)
		} else {
			valEnd++
			for ; valEnd < n && dsn[valEnd] != '\''; valEnd++ {
				if dsn[valEnd] == '\\' {
					valEnd++
				}
			}
			if valEnd >= n {
				return "", 0, false
			}
			valEnd++
		}
		if isPassword {
			out.WriteString(redactedPassword)
			masked++
		} else {
			out.WriteString(dsn[valStart:valEnd])
		}
		next := valEnd
		for next < n && strings.IndexByte(dsnSpace, dsn[next]) >= 0 {
			next++
		}
		out.WriteString(dsn[valEnd:next])
		i = next
	}
	return out.String(), masked, true
}

var urlSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// isURLDSN reports whether dsn is a URL. pgx reads a string as a URL only when it
// starts with postgres:// or postgresql://; a keyword/value DSN may carry "://"
// inside a value and must not be taken for one. Any other leading scheme is
// also treated as a URL, so a userinfo password in it is still masked rather
// than left in place.
func isURLDSN(dsn string) bool {
	return urlSchemePattern.MatchString(dsn)
}

// RedactDSN masks the password of a Postgres DSN (URL or keyword/value form)
// so the remainder can be recorded in world-readable metadata.
func RedactDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return ""
	}
	if isURLDSN(trimmed) {
		u, err := url.Parse(trimmed)
		if err != nil {
			return "[redacted]"
		}
		if u.User != nil {
			if _, has := u.User.Password(); has {
				u.User = url.UserPassword(u.User.Username(), redactedPassword)
			}
		}
		q := u.Query()
		if q.Has("password") {
			q.Set("password", redactedPassword)
			u.RawQuery = q.Encode()
		}
		return u.String()
	}
	if len(keywordPasswordAny.FindAllStringIndex(trimmed, -1)) == 0 {
		return trimmed
	}
	// Record nothing rather than a partial secret when the string does not parse
	// as keyword/value pairs (an unterminated quote, text after a double-quoted
	// value that pgx reads as a bare key) or when a "password=" is left over
	// inside another key's value.
	redacted, masked, ok := redactKeywordValueDSN(trimmed)
	if !ok || masked != len(keywordPasswordAny.FindAllStringIndex(trimmed, -1)) {
		return "[redacted]"
	}
	return redacted
}

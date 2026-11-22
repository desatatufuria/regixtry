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

var (
	keywordPasswordPattern = regexp.MustCompile(`(?i)(\bpassword\s*=\s*)('(?:[^'\\]|\\.)*'|(?:\\.|[^\s\\'"])+)`)
	// keywordPasswordAny finds every password key, however its value is written.
	keywordPasswordAny = regexp.MustCompile(`(?i)\bpassword\s*=`)
)

// RedactDSN masks the password of a Postgres DSN (URL or keyword/value form)
// so the remainder can be recorded in world-readable metadata.
func RedactDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
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
	redacted := keywordPasswordPattern.ReplaceAllString(trimmed, "${1}"+redactedPassword)
	// Every password key must have been recognised and masked (a double-quoted
	// or otherwise unsupported value would leave a fragment behind); if not,
	// record nothing rather than a partial secret.
	if len(keywordPasswordAny.FindAllString(trimmed, -1)) != len(keywordPasswordPattern.FindAllString(trimmed, -1)) {
		return "[redacted]"
	}
	return redacted
}

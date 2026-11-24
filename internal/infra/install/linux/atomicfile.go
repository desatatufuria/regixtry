package linux

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// writeFileAtomic replaces path with body without ever exposing a truncated
// file: the bytes go to a fresh O_EXCL temp file in the same directory, are
// fsynced, and rename(2) swaps them in, so a crash leaves either the old file
// or the new one. The temp file is chmod-ed to mode before the rename so the
// result has the intended mode whatever the process umask is. The target is
// replaced, never written through, so a symlink at path is not followed.
func writeFileAtomic(path string, body []byte, mode os.FileMode) error {
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
			werr = f.Chmod(mode)
		}
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
		syncDir(dir)
		return nil
	}
	return fmt.Errorf("create temporary file next to %s: too many collisions", path)
}

// syncDir makes a completed rename durable. It is best effort: the rename has
// already happened, so a directory that cannot be opened or synced is not an
// error of the write.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

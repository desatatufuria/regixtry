package linux

import (
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
)

const (
	// storageDirMode is the mode of the storage root and content directory
	// setup creates: no access for "other" local users, because blob bytes
	// and metadata.db would bypass the HTTP authorization layer.
	storageDirMode os.FileMode = 0o750
	// databaseFileMode is the mode of the empty metadata.db setup creates.
	databaseFileMode os.FileMode = 0o640
	// configDirMode keeps the config and unit directories traversable.
	configDirMode os.FileMode = 0o755

	// serviceUID is the user the rendered systemd unit runs as: the unit sets
	// no User=, so systemd runs the daemon as root. Setup and upgrade rewrite
	// the unit from the same template, so this holds for every managed install.
	serviceUID uint32 = 0
)

type tightenResult int

const (
	// tightenNotNeeded: the path is absent or already has no "other" access.
	tightenNotNeeded tightenResult = iota
	// tightenedOther: the "other" bits were removed.
	tightenedOther
	// tightenSkippedServiceIsOther: the service user is neither root, the
	// owner nor in the group, so it relies on the "other" bits; removing them
	// would lock the service out. The path is left untouched.
	tightenSkippedServiceIsOther
	// tightenSkippedUnknownOwner: ownership could not be determined.
	tightenSkippedUnknownOwner
)

// tightenOtherAccess removes only the "other" permission bits (o-rwx) of an
// existing path, and only when the service user keeps access through root,
// ownership or group membership. It never changes ownership, the owner or
// group bits, or setuid/setgid/sticky.
func tightenOtherAccess(path string, uid uint32, gids []uint32, chmod func(string, os.FileMode) error) (tightenResult, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return tightenNotNeeded, nil
	}
	if err != nil {
		return tightenNotNeeded, err
	}
	if info.Mode().Perm()&0o007 == 0 {
		return tightenNotNeeded, nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return tightenSkippedUnknownOwner, nil
	}
	if !serviceKeepsAccess(stat, uid, gids) {
		return tightenSkippedServiceIsOther, nil
	}

	keep := info.Mode() & (os.ModePerm | os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	if err := chmod(path, keep&^0o007); err != nil {
		return tightenNotNeeded, err
	}
	return tightenedOther, nil
}

func serviceKeepsAccess(stat *syscall.Stat_t, uid uint32, gids []uint32) bool {
	if uid == 0 || stat.Uid == uid {
		return true
	}
	for _, gid := range gids {
		if stat.Gid == gid {
			return true
		}
	}
	return false
}

// tightenStorageAccess applies tightenOtherAccess to the storage root, the
// content path and the database file of an existing install. It never walks
// below those three paths and never fails the setup or upgrade: what it cannot
// tighten is reported as a warning with the exact command to run by hand.
// Without a chmod hook (tests and fakes) it does nothing.
func (b *Bootstrapper) tightenStorageAccess(plan BootstrapPlan) {
	if b.chmod == nil {
		return
	}
	warnf := b.warnf
	if warnf == nil {
		warnf = log.Printf
	}
	seen := map[string]bool{}
	for _, path := range []string{plan.StorageRoot, plan.ContentPath, plan.DatabasePath} {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		result, err := tightenOtherAccess(path, serviceUID, []uint32{0}, b.chmod)
		switch {
		case err != nil:
			warnf("warning: could not remove access for other users from %s: %v; run: chmod o-rwx %s", path, err, path)
		case result == tightenSkippedServiceIsOther || result == tightenSkippedUnknownOwner:
			warnf("warning: %s is accessible to other local users and was left unchanged because the service user could lose access to it; once the service owns it or shares its group, run: chmod o-rwx %s", path, path)
		}
	}
}

// WorldAccessiblePaths returns, in order, the existing paths among paths that
// other local users can read or write. Absent or unreadable paths are not
// reported.
func WorldAccessiblePaths(paths ...string) []string {
	var found []string
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o006 != 0 {
			found = append(found, path)
		}
	}
	return found
}

// WorldAccessWarning formats the one-line startup warning for the given
// world-accessible paths, or returns "" when there is nothing to report.
func WorldAccessWarning(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	command := "chmod o-rwx"
	for _, path := range paths {
		command += " " + shellQuote(path)
	}
	return fmt.Sprintf("warning: %s can be read or written by other local users, which bypasses registry authorization; restrict it (as the service owner or root, once the service user owns it or shares its group) with: %s", strings.Join(paths, ", "), command)
}

// shellQuote quotes a path for a copy-pasteable shell command.
func shellQuote(path string) string {
	if path != "" && strings.Trim(path, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-") == "" {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

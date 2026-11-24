package linux

import (
	"errors"
	"fmt"
	"os"
)

// ExistingInstallError is returned when setup would run over a location that
// already belongs to a previous install. Setup never overwrites, truncates or
// removes that data; `regixtry upgrade` is the supported way to move it.
type ExistingInstallError struct {
	Path string
	// Reason says what was found at Path ("contains data", "already exists").
	Reason string
	// HoldsData is true for a database or content store that is not empty. It
	// is false for managed files (unit, env, receipts) a previous run left.
	HoldsData bool
	StatePath string
}

func (e *ExistingInstallError) Error() string {
	if e.HoldsData {
		return fmt.Sprintf("refusing to set up over an existing install: %s %s. Setup never overwrites or removes existing data; run `regixtry upgrade` to move this install to a newer release", e.Path, e.Reason)
	}
	return fmt.Sprintf("refusing to set up over a previous install: %s %s. Run `regixtry upgrade` to move a complete install to a newer release, or `regixtry bootstrap --rollback --state-path %s` to remove an incomplete one before running setup again", e.Path, e.Reason, e.StatePath)
}

// ensureFreshInstall fails with *ExistingInstallError when the plan's database
// or content store already holds data, or when a managed unit, env file or
// receipt of a previous install is present. It only reads.
func ensureFreshInstall(plan BootstrapPlan) error {
	for _, path := range sqliteCleanupPaths(plan.DatabasePath) {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !info.Mode().IsRegular() || info.Size() > 0 {
			return &ExistingInstallError{Path: path, Reason: "contains data from a previous install", HoldsData: true, StatePath: plan.StatePath}
		}
	}

	info, err := os.Lstat(plan.ContentPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("inspect %s: %w", plan.ContentPath, err)
	case !info.IsDir():
		return &ExistingInstallError{Path: plan.ContentPath, Reason: "exists and is not a directory", HoldsData: true, StatePath: plan.StatePath}
	default:
		entries, err := os.ReadDir(plan.ContentPath)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", plan.ContentPath, err)
		}
		if len(entries) > 0 {
			return &ExistingInstallError{Path: plan.ContentPath, Reason: "contains data from a previous install", HoldsData: true, StatePath: plan.StatePath}
		}
	}

	for _, path := range []string{plan.UnitPath, plan.EnvPath, plan.StatePath, lifecycleProvenancePath(plan.StatePath)} {
		if _, err := os.Lstat(path); err == nil {
			return &ExistingInstallError{Path: path, Reason: "already exists", StatePath: plan.StatePath}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
	}

	return nil
}

// missingPaths returns the entries of paths that do not exist yet: what a setup
// run starting now would create. It is never nil, so a receipt always records
// "created by this run", even when that is an empty list.
func missingPaths(paths []string) ([]string, error) {
	missing := []string{}
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect %s: %w", path, err)
		}
		missing = append(missing, path)
	}
	return missing, nil
}

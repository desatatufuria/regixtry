package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Down tears the compose stack down (`docker compose ... down --volumes`)
// and removes the generated project directory -- the compose file, env
// file, and compose provenance file all live under proj.Dir, so removing
// it is equivalent to removing each generated file individually. This is
// the docker-mode-native teardown, separate from systemd's `uninstall`; it
// mirrors rollbackSetupFailure/rollbackWithReceipt's best-effort-cleanup,
// join-errors shape (internal/infra/install/linux/bootstrap.go): file
// removal is still attempted even when the `docker compose down` subprocess
// itself fails, so a partially-failed teardown never leaves the project
// directory behind (design.md Data Flow; tasks.md 6.6/6.7).
func (p *Provisioner) Down(ctx context.Context, proj Project) error {
	if p == nil || p.exec == nil {
		return errors.New("compose provisioner is not configured")
	}

	var errs []error
	if _, err := p.exec(ctx, "docker", composeArgs(proj, "down", "--volumes")...); err != nil {
		errs = append(errs, fmt.Errorf("compose down: %w", err))
	}

	if dir := strings.TrimSpace(proj.Dir); dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("remove compose project directory: %w", err))
		}
	}

	return errors.Join(errs...)
}

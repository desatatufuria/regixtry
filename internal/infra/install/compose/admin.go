package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// BootstrapAdmin runs the registry image's `bootstrap-admin` subcommand
// once, non-interactively, inside a throwaway container:
// `docker compose run --rm --no-deps -T regixtry bootstrap-admin -username
// <username> -password-stdin`. The admin password is piped on stdin only --
// never as an argv element and never set as an environment variable by this
// method -- so it cannot appear in a `ps`/process listing (design.md
// "Secret channel" threat; tasks.md 4.4). The container already receives
// REGISTRY_AUTH_POSTGRES_DSN from the compose file's own environment
// mapping (assets/docker-compose.yml), so bootstrap-admin's DSN flag default
// resolves it without this method needing to pass it as a second argument.
//
// The password is handed to the exec seam as a single strings.Reader over
// the caller-supplied string; no intermediate byte-slice copy is made here,
// so this method carries no buffered copy of the password beyond the one
// os/exec's own stdin pipe requires internally.
func (p *Provisioner) BootstrapAdmin(ctx context.Context, proj Project, username, password string) error {
	if p == nil || p.execStdin == nil {
		return errors.New("compose provisioner is not configured")
	}

	args := composeArgs(proj, "run", "--rm", "--no-deps", "-T", "regixtry", "bootstrap-admin", "-username", username, "-password-stdin")
	if _, err := p.execStdin(ctx, strings.NewReader(password), "docker", args...); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	return nil
}

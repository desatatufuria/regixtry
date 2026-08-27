package compose

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// postgresReadyMaxAttempts/postgresReadyPollInterval mirror the bounded
// retry shape docs/verification/scripts/docker-push-pull-smoke.sh already
// proved (`for _ in $(seq 1 30); do pg_isready ...; sleep 1; done`) --
// design.md "Postgres readiness" decision explicitly rejects an unbounded
// wait.
const (
	postgresReadyMaxAttempts  = 30
	postgresReadyPollInterval = time.Second
)

// composeArgs builds the shared "compose --project-name <p> --file <f>
// --env-file <e> <sub...>" argv prefix every docker-compose invocation in
// this package uses. Project identity and file paths are threaded as
// distinct argv elements, never string-interpolated, so an operator-
// supplied project name reaches "docker" as one literal argument
// (design.md "Subprocess argv composition" threat). Pure function: same
// inputs always produce the same argv slice.
func composeArgs(p Project, sub ...string) []string {
	args := []string{"compose", "--project-name", p.Name, "--file", p.ComposeFilePath, "--env-file", p.EnvFilePath}
	return append(args, sub...)
}

// StartDatabase brings up the bundled Postgres service and waits for it to
// report ready. It is a no-op when the project uses an external DSN --
// StartDatabase must never invoke docker at all in that case (design.md
// Data Flow: "bundled: docker compose up -d postgres   external: (skipped)").
func (p *Provisioner) StartDatabase(ctx context.Context, proj Project) error {
	if p == nil || p.exec == nil {
		return errors.New("compose provisioner is not configured")
	}
	if !proj.BundledPostgres {
		return nil
	}

	if _, err := p.exec(ctx, "docker", composeArgs(proj, "up", "-d", "postgres")...); err != nil {
		return fmt.Errorf("start bundled postgres: %w", err)
	}

	return p.waitPostgresReady(ctx, proj)
}

// waitPostgresReady polls `docker compose exec -T postgres pg_isready` a
// bounded number of times, sleeping between attempts, and fails loudly
// rather than looping forever when Postgres never becomes ready (design.md
// "Postgres readiness" decision; tasks.md 4.2).
func (p *Provisioner) waitPostgresReady(ctx context.Context, proj Project) error {
	args := composeArgs(proj, "exec", "-T", "postgres", "pg_isready", "-U", bundledPostgresUser, "-d", bundledPostgresDB)

	var lastErr error
	for attempt := 0; attempt < postgresReadyMaxAttempts; attempt++ {
		if _, err := p.exec(ctx, "docker", args...); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < postgresReadyMaxAttempts-1 {
			p.sleep(postgresReadyPollInterval)
		}
	}

	return fmt.Errorf("bundled postgres did not become ready after %d attempts: %w", postgresReadyMaxAttempts, lastErr)
}

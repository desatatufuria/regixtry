package compose

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// waitPostgresReady polls `docker compose logs postgres` a bounded number of
// times, sleeping between attempts, and fails loudly rather than looping
// forever when Postgres never becomes ready (design.md "Postgres readiness"
// decision; tasks.md 4.2).
//
// This deliberately does NOT use `pg_isready` (found live during v0.2.1-rc4
// docker-mode validation): on a FRESH volume, the official postgres image
// starts a temporary, Unix-socket-only server to run initdb, shuts it down,
// then starts the final server that actually accepts networked, password-
// authenticated connections. `pg_isready` run inside the postgres container
// can report success against that temporary server, well before the final
// one is listening -- BootstrapAdmin's very next step (connecting over the
// network with the real password) then failed deterministically on every
// fresh volume, and only succeeded once Postgres had already been running
// long enough to complete both restarts. The postgres image's own log
// output is the reliable signal: "PostgreSQL init process complete; ready
// for start up." appears only when that temporary-then-final handoff is
// happening, and "database system is ready to accept connections" appears
// once per server start -- twice on a fresh volume, once on an existing one.
func (p *Provisioner) waitPostgresReady(ctx context.Context, proj Project) error {
	args := composeArgs(proj, "logs", "--no-color", "postgres")

	var lastErr error
	for attempt := 0; attempt < postgresReadyMaxAttempts; attempt++ {
		if output, err := p.exec(ctx, "docker", args...); err != nil {
			lastErr = err
		} else if postgresLogIndicatesReady(string(output)) {
			return nil
		} else {
			lastErr = errors.New("postgres logs do not yet show the final server accepting connections")
		}
		if attempt < postgresReadyMaxAttempts-1 {
			p.sleep(postgresReadyPollInterval)
		}
	}

	return fmt.Errorf("bundled postgres did not become ready after %d attempts: %w", postgresReadyMaxAttempts, lastErr)
}

// postgresLogIndicatesReady reports whether the postgres container's log
// output shows the FINAL server -- not the temporary initdb-only server --
// is accepting connections. On a fresh volume the official image restarts
// once (logged as "PostgreSQL init process complete; ready for start up."),
// so the readiness marker must appear a second time; on an existing volume
// it never restarts, so the marker's first appearance already is the final
// server.
func postgresLogIndicatesReady(log string) bool {
	const readyMarker = "database system is ready to accept connections"
	const reinitMarker = "PostgreSQL init process complete; ready for start up."

	readyCount := strings.Count(log, readyMarker)
	if readyCount == 0 {
		return false
	}
	if strings.Contains(log, reinitMarker) {
		return readyCount >= 2
	}
	return true
}

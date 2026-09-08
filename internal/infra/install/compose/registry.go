package compose

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// registryReachableMaxAttempts/registryReachablePollInterval mirror
// waitPostgresReady's bounded retry shape (database.go) -- design.md
// "Postgres readiness" decision's rejection of an unbounded wait applies
// equally here: a registry that never becomes reachable must fail loudly,
// not hang forever.
const (
	registryReachableMaxAttempts  = 30
	registryReachablePollInterval = time.Second
)

// StartRegistry brings up the registry service via Compose. Bundled
// projects run a plain `docker compose up -d`, which also starts the
// dependent postgres service; external-DSN projects run
// `up -d --no-deps regixtry` so the bundled postgres service is never
// started (design.md "Skipping the bundled service" decision; tasks.md
// 6.1).
func (p *Provisioner) StartRegistry(ctx context.Context, proj Project) error {
	if p == nil || p.exec == nil {
		return errors.New("compose provisioner is not configured")
	}

	sub := []string{"up", "-d"}
	if !proj.BundledPostgres {
		sub = []string{"up", "-d", "--no-deps", "regixtry"}
	}

	if _, err := p.exec(ctx, "docker", composeArgs(proj, sub...)...); err != nil {
		return fmt.Errorf("start registry: %w", err)
	}

	return nil
}

// WaitReachable polls `GET <public-url>/v2/` a bounded number of times,
// sleeping between attempts, and accepts either a 200 (anonymous access) or
// a 401 (auth required but the registry is up) as reachable -- the same
// semantics `regixtry healthcheck` uses (design.md Interfaces / Contracts).
// This is a direct HTTP poll rather than a compose healthcheck: PR #1's
// apply-progress recorded that the current Dockerfile on this branch has no
// working container healthcheck primitive for the regixtry service, so
// `docker compose ps` cannot be relied on for readiness here. A registry
// that never becomes reachable returns an installation-failure error, never
// a claim that the stack is healthy (spec.md "Stack does not become
// reachable" scenario).
func (p *Provisioner) WaitReachable(ctx context.Context, proj Project) error {
	if p == nil || p.httpStatus == nil {
		return errors.New("compose provisioner is not configured")
	}

	target := strings.TrimRight(proj.PublicURL, "/") + "/v2/"

	var lastErr error
	for attempt := 0; attempt < registryReachableMaxAttempts; attempt++ {
		status, err := p.httpStatus(ctx, target)
		switch {
		case err == nil && (status == http.StatusOK || status == http.StatusUnauthorized):
			return nil
		case err != nil:
			lastErr = err
		default:
			lastErr = fmt.Errorf("unexpected status %d", status)
		}

		if attempt < registryReachableMaxAttempts-1 {
			p.sleep(registryReachablePollInterval)
		}
	}

	return fmt.Errorf("registry at %s did not become reachable after %d attempts: installation failed: %w", target, registryReachableMaxAttempts, lastErr)
}

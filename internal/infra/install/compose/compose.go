// Package compose implements the `docker` setup mode's provisioning
// backend: a sibling port to internal/infra/install/linux, reached through
// the composeRunner interface + newComposeRunner seam declared in
// cmd/regixtry/main.go (design.md "Where docker lives" decision). Widening
// installlinux.supportedMode was rejected because it would let a compose
// config reach ValidateConfig/runner.Run/systemd rollback, silently
// breaking the "daemon-sqlite unchanged" guarantee -- so this package never
// imports internal/infra/install/linux.
package compose

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"time"
)

// execRunner is the injectable subprocess seam every composeRunner method
// uses. Production wiring always builds argv slices for exec.CommandContext
// -- never a shell string -- so operator-supplied values (DSNs, project
// names, image refs) can never be shell-interpreted (design.md "Subprocess
// argv composition" threat). Tests inject a fake exec runner (see
// exec_helper_test.go) so the entire package tests without a Docker daemon.
type execRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// execStdinRunner is the injectable seam for the one subprocess invocation
// that must carry secret input on stdin rather than argv or an environment
// variable: BootstrapAdmin's `-password-stdin` (design.md "Secret channel"
// threat). Kept as a distinct type from execRunner so every method that
// never touches a secret (Preflight, StartDatabase's readiness poll) keeps
// the plain argv-only seam, and so tests can assert stdin content
// independently of argv.
type execStdinRunner func(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error)

// httpStatusFetcher is the injectable seam WaitReachable uses to poll the
// registry's public URL (design.md Data Flow: "poll GET <public-url>/v2/ ->
// 200 or 401"). Kept distinct from execRunner since this is a direct HTTP
// probe, never a subprocess -- PR #1's apply-progress recorded that the
// current Dockerfile on this branch has no working container healthcheck
// primitive, so WaitReachable cannot rely on `docker compose ps`'s health
// status and polls the URL itself instead. Tests inject a fake so the whole
// package still needs no real network call.
type httpStatusFetcher func(ctx context.Context, url string) (int, error)

// ProvisionerConfig configures a Provisioner. Exec/ExecStdin default to real
// exec.CommandContext invocations of "docker" when nil; Sleep defaults to
// time.Sleep; HTTPStatus defaults to a real http.Client GET. Tests inject
// fakes for all four so the package never needs a Docker daemon, a real
// clock, or a real network call.
type ProvisionerConfig struct {
	Exec       execRunner
	ExecStdin  execStdinRunner
	Sleep      func(time.Duration)
	HTTPStatus httpStatusFetcher
}

// Provisioner implements the composeRunner contract cmd/regixtry/main.go
// declares (design.md Interfaces / Contracts) for `docker` setup mode:
// preflight detection, compose project materialization, staged bring-up,
// one-shot admin bootstrap, reachability, provenance, teardown.
type Provisioner struct {
	exec       execRunner
	execStdin  execStdinRunner
	sleep      func(time.Duration)
	httpStatus httpStatusFetcher
}

// NewProvisioner builds a Provisioner. A nil cfg.Exec/cfg.ExecStdin falls
// back to real subprocess execution and a nil cfg.Sleep falls back to
// time.Sleep -- the same default-then-inject shape the trivy and gitleaks
// scanner runners already use in this codebase.
func NewProvisioner(cfg ProvisionerConfig) *Provisioner {
	run := cfg.Exec
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.Output()
		}
	}

	runStdin := cfg.ExecStdin
	if runStdin == nil {
		runStdin = func(ctx context.Context, stdin io.Reader, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Stdin = stdin
			return cmd.Output()
		}
	}

	sleep := cfg.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	httpStatus := cfg.HTTPStatus
	if httpStatus == nil {
		client := &http.Client{Timeout: 5 * time.Second}
		httpStatus = func(ctx context.Context, url string) (int, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return 0, err
			}
			resp, err := client.Do(req)
			if err != nil {
				return 0, err
			}
			defer resp.Body.Close()
			return resp.StatusCode, nil
		}
	}

	return &Provisioner{exec: run, execStdin: runStdin, sleep: sleep, httpStatus: httpStatus}
}

// Truthful Preflight messages (design.md Interfaces / Contracts): each
// distinct cause gets its own distinct, non-panicking message.
const (
	errDockerNotInstalled      = "docker is not installed or not on PATH"
	errComposeV2Required       = "Docker Compose v2 is required (the `docker compose` plugin was not found)"
	errDockerDaemonUnreachable = "the Docker daemon is not reachable; ensure it is running and your user can access it"
)

// Preflight maps the three distinct Docker/Compose absence causes to three
// distinct truthful messages and writes nothing to disk. `docker version`
// is probed first so a not-on-PATH binary (exec.ErrNotFound) and an
// unreachable daemon (docker ran but failed) are told apart before the
// compose plugin is probed at all -- a failing `docker version` short-
// circuits Preflight without ever invoking `docker compose version`.
func (p *Provisioner) Preflight(ctx context.Context) error {
	if p == nil || p.exec == nil {
		return errors.New("compose provisioner is not configured")
	}

	if _, err := p.exec(ctx, "docker", "version"); err != nil {
		if isDockerNotFound(err) {
			return errors.New(errDockerNotInstalled)
		}
		return errors.New(errDockerDaemonUnreachable)
	}

	if _, err := p.exec(ctx, "docker", "compose", "version"); err != nil {
		return errors.New(errComposeV2Required)
	}

	return nil
}

func isDockerNotFound(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

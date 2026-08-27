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
	"os/exec"
)

// execRunner is the injectable subprocess seam every composeRunner method
// uses. Production wiring always builds argv slices for exec.CommandContext
// -- never a shell string -- so operator-supplied values (DSNs, project
// names, image refs) can never be shell-interpreted (design.md "Subprocess
// argv composition" threat). Tests inject a fake exec runner (see
// exec_helper_test.go) so the entire package tests without a Docker daemon.
type execRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ProvisionerConfig configures a Provisioner. Exec defaults to real
// exec.CommandContext invocations of "docker" when nil; tests inject a fake.
type ProvisionerConfig struct {
	Exec execRunner
}

// Provisioner implements the composeRunner contract cmd/regixtry/main.go
// declares (design.md Interfaces / Contracts) for `docker` setup mode:
// preflight detection, compose project materialization, staged bring-up,
// one-shot admin bootstrap, reachability, provenance, teardown. Only
// Preflight and WriteProject land in this PR; StartDatabase/BootstrapAdmin
// (PR #2) and StartRegistry/WaitReachable/SaveProvenance/Down (PR #3)
// follow later in the container-setup-mode chain.
type Provisioner struct {
	exec execRunner
}

// NewProvisioner builds a Provisioner. A nil cfg.Exec falls back to real
// subprocess execution -- the same default-then-inject shape the trivy and
// gitleaks scanner runners already use in this codebase.
func NewProvisioner(cfg ProvisionerConfig) *Provisioner {
	run := cfg.Exec
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			return cmd.Output()
		}
	}
	return &Provisioner{exec: run}
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

## Exploration: unified interactive installer / deployment chooser

### Current State
The current flow already has the two backend pieces needed for a first guided install, but they are exposed as separate concepts. `install.sh` is the release-first entrypoint: it resolves a Linux release asset, verifies the checksum, installs the `registry` binary, and then unconditionally calls `registry bootstrap --mode daemon-sqlite`. The actual deployment contract lives behind `cmd/registry/main.go` and `internal/infra/install/linux/bootstrap.go`, where bootstrap parsing, validation, artifact rendering, service activation, readiness probing, and rollback are implemented for exactly one truthful mode: `daemon-sqlite` on supported Linux + systemd hosts.

The repository also already has an interactive UI stack, but it is the wrong layer for the first installer slice. `internal/tui/model.go` is a Bubble Tea operator console for repository/admin workflows after runtime exists; it is not an installation wizard and it depends on the binary already being available. That makes the current shell installer the safest place to add a first unified chooser, while keeping the bootstrap command as the execution backend.

### Affected Areas
- `install.sh` — current install entrypoint; this is where binary install and bootstrap are currently coupled, so the first chooser would live here.
- `cmd/registry/main.go` — owns `bootstrap` argument parsing and the command split between `serve`, `tui`, `bootstrap`, and `bootstrap-admin`.
- `cmd/registry/main_test.go` — already covers bootstrap parsing/runner wiring; natural place for chooser-to-flags regression coverage if CLI contracts change.
- `internal/infra/install/linux/bootstrap.go` — source of truth for supported mode validation, artifact planning, apply, readiness probe, and rollback.
- `internal/infra/install/linux/templates.go` — renders the env file and systemd unit that any guided flow would still rely on.
- `README.md` — currently documents install and bootstrap as mostly operator-oriented steps; would need a unified UX contract and deferred-mode guidance.
- `docs/verification/scripts/install-release-smoke.sh` — current deterministic verification harness for the installer; safest first interactive work should preserve or extend this instead of replacing it.
- `internal/tui/model.go` — relevant mainly as a deferred reuse target, not as the first implementation surface.
- `docker-compose.yml` — current helper runtime remains explicitly local/dev-oriented, so it should stay a deferred branch in the chooser rather than becoming a first-class install target.

### Approaches
1. **TTY shell chooser over existing installer + bootstrap** — keep `install.sh` as the entrypoint, detect an interactive terminal, and ask only high-value questions that map to existing flags/backends.
   - Pros: Reuses the already-shipped release installer and `registry bootstrap`; smallest blast radius; keeps automation possible via existing flags/env; easy to defer unsupported modes with truthful messaging.
   - Cons: Shell UX is limited; prompt logic in `curl | bash` needs careful non-interactive fallback; richer flows become awkward if too much logic accumulates.
   - Effort: Low/Medium

2. **New in-binary install wizard command** — add a new registry subcommand that owns the guided deployment chooser after the binary is installed.
   - Pros: Stronger long-term architecture; easier validation/testing in Go; can eventually unify interactive and non-interactive flows behind one code path.
   - Cons: Still needs `install.sh` to download/install the binary first; creates a two-step first-run UX before the unified flow exists; bigger first slice.
   - Effort: Medium

3. **Bubble Tea full-screen wizard** — reuse the existing TUI stack for a richer installation/deployment wizard.
   - Pros: Best guided UX and future consistency with the operator console.
   - Cons: Highest review cost; wrong first abstraction because current TUI is post-install; harder to fit into a safe `curl | bash` flow; expands testing and UX state sharply.
   - Effort: High

### Recommendation
Start with **Approach 1**, but keep the first slice narrower than “full installer wizard.”

Safest first interactive surfaces to expose:
- **Choice 1: install only vs install + bootstrap** — this directly fixes today's biggest UX coupling without inventing a new backend.
- **Choice 2: daemon-sqlite defaults review** — prompt for public URL, listen address, storage root, receipt path, and service name, then hand those values to the existing bootstrap flags.
- **Choice 3: deferred-mode routing** — let users choose “Postgres-backed” or “container-based” paths only to receive truthful guidance that those modes are deferred/manual today.

Do **not** expose these in the first interactive slice:
- Postgres DSN / `bootstrap-admin` sequencing
- Compose or container orchestration as a first-class installation mode
- Bubble Tea or a new in-binary wizard runtime

### Risks
- Interactive behavior in `install.sh` can break CI/provisioning unless TTY detection and explicit non-interactive fallback remain first-class.
- If the chooser promises container or Postgres-backed installs before release-grade backends exist, the UX becomes misleading.
- Mixing prompt UX, new deployment backends, and docs/verification rewrites in one slice would expand beyond a safe review boundary.
- Shell prompt drift from bootstrap flag defaults would create two sources of truth unless the chooser stays as a thin front-end.

### Ready for Proposal
Yes — the first proposal should target a TTY-aware chooser in `install.sh` that introduces an **install-only vs install-and-bootstrap** decision, guides only the existing `daemon-sqlite` inputs, preserves all current non-interactive flags/env overrides, and treats Postgres/container paths as explicit follow-up slices rather than supported installs.

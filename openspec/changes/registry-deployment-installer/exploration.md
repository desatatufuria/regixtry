## Exploration: registry-deployment-installer

### Current State
The repository already has a truthful release installer backend plus one truthful host bootstrap backend, but not yet a truthful deployment orchestrator. `install.sh` downloads and verifies a Linux release binary, then immediately runs `registry bootstrap --mode daemon-sqlite`. `cmd/registry/main.go` exposes the runtime and bootstrap flags, and `internal/infra/install/linux/bootstrap.go` implements exactly one supported deployment backend: Linux + systemd + `daemon-sqlite`. Postgres-backed auth is real at runtime through `-auth-postgres-dsn` / `REGISTRY_AUTH_POSTGRES_DSN`, and `bootstrap-admin` exists for first-admin creation, but no installer path currently orchestrates that auth-enabled sequence. The container path is still a local helper: `docker-compose.yml` builds from the repo `Dockerfile`, depends on an external Docker network, and requires manual bootstrap ordering, while release automation publishes tarballs and checksums only.

### Affected Areas
- `install.sh` — current release installer hard-couples install + `daemon-sqlite` bootstrap; the chooser/orchestrator starts here.
- `cmd/registry/main.go` — owns `serve`, `bootstrap`, and `bootstrap-admin` contracts; any deployment mode must map to these flags or extend them.
- `cmd/registry/main_test.go` — already covers bootstrap parsing and runner wiring; safest place for chooser contract regression tests.
- `internal/infra/install/linux/bootstrap.go` — only truthful automated deployment backend today; validates `daemon-sqlite`, host support, artifact creation, activation, readiness, and rollback.
- `internal/infra/install/linux/templates.go` — renders the env file and systemd unit used by the current host bootstrap path.
- `internal/infra/install/linux/bootstrap_test.go` — proves supported-host detection, artifact rendering, readiness success, and rollback for the current backend.
- `internal/infra/auth/postgres/store.go` — proves Postgres auth storage exists and self-bootstraps schema, but not installer orchestration.
- `README.md` — currently documents release installer, deferred scopes, and local Compose helper separately; deployment choices need one truthful operator story.
- `docs/verification/scripts/install-release-smoke.sh` — installer smoke coverage currently validates release install plus `daemon-sqlite` bootstrap via stubs; it does not yet prove interactive branching or auth/container orchestration.
- `docker-compose.yml` — current multi-service runtime is dev/local oriented and still manual.
- `Dockerfile` — container runtime exists, but it is source-build oriented rather than release-image oriented.
- `.github/workflows/release.yml` and `.goreleaser.yaml` — release pipeline publishes binary archives only, which blocks a truthful first-class container install mode.

### Approaches
1. **Thin interactive chooser over the current installer/backend** — keep `install.sh` as the entrypoint, add TTY-aware questions, and route only to already truthful backend paths.
   - Pros: Lowest blast radius; preserves automation via flags/env; fits the current shipped release + bootstrap architecture.
   - Cons: Shell UX is limited; unsupported choices must stay as guided deferrals, not real orchestration.
   - Effort: Low

2. **New in-binary deployment orchestrator command** — install the binary first, then run a Go-native guided deployment command.
   - Pros: Better long-term architecture; easier validation than complex shell prompts; future non-interactive and interactive flows can share one engine.
   - Cons: Bigger first slice; still needs `install.sh` as the download/bootstrap wrapper; more review surface now.
   - Effort: Medium

### Recommendation
Start with **Approach 1** and keep the first slice brutally narrow: add an interactive-first chooser in `install.sh` that offers **binary only** versus **binary + daemon/service**, then reuse the existing `registry bootstrap --mode daemon-sqlite` backend for the second branch.

Truthful deployment choices today:
- **Supported now:** `binary only` — truthful as a verified release install if the script stops before bootstrap.
- **Supported now:** `binary + daemon/service` — truthful only for Linux + systemd hosts through the existing `daemon-sqlite` backend.
- **Not yet first-class:** `binary + daemon with optional Postgres-backed auth` — runtime pieces exist, but installer orchestration is immature because DSN capture, secret handling, bootstrap-admin ordering, and auth-ready verification are still manual.
- **Not yet first-class:** `all-in-containers` — current Compose path is a local helper, not a release-grade deployment contract.

The safest first reviewable slice is therefore:
1. decouple install-only from install-and-bootstrap in `install.sh`,
2. add TTY-aware prompts only for the existing `daemon-sqlite` inputs,
3. preserve non-interactive flags/env as the source of truth,
4. treat Postgres-auth and container choices as explicit “coming later / manual today” guidance.

On prior direction: **fold, do not replace, the shipped `registry-installation-modes` backend direction**. Its `daemon-sqlite` contract is now the implementation foundation. What should be superseded is the older framing that stopped at a narrow bootstrap mode; this new change should absorb that backend and expand the user-facing contract into a deployment chooser/orchestrator. In practice, `registry-installation-wizard` should be folded into this new change, not pursued separately.

### Risks
- The main `openspec/specs/installation-modes/spec.md` facts are stale against the current repo, so proposal/spec work must re-baseline current behavior before extending it.
- Prompt logic in `install.sh` can break CI/provisioning unless TTY detection and explicit non-interactive fallbacks remain authoritative.
- Exposing Postgres-auth or container flows too early would over-promise because release-grade orchestration and verification are not there yet.
- Shell prompts can drift from bootstrap defaults unless the chooser remains a very thin front-end over existing flags.

### Ready for Proposal
Yes — propose `registry-deployment-installer` as an interactive-first deployment chooser whose first slice exposes only `binary only` and `binary + daemon/service`, reuses the shipped `daemon-sqlite` backend unchanged, defers Postgres-auth and container installs as guided/manual paths, and explicitly folds prior installation-mode and installer-wizard direction into one deployment-oriented change.

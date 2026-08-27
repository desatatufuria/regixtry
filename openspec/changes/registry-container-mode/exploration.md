# Exploration: registry-container-mode

## Current State

The repo has three release-relevant layers, all verified directly against current code (not re-derived from the prior related change's now-stale notes):

1. **Container build (dev-oriented)**: root `Dockerfile` is a two-stage build (`golang:1.26-bookworm` builder → `debian:bookworm-slim` runtime) that builds `./cmd/regixtry` (the actual module/binary name is `regixtry`; `cmd/registry/main.go` does not exist — the real path is `cmd/regixtry/main.go`). It installs `ca-certificates`, runs as root (no `USER` directive), has no `HEALTHCHECK`, exposes 5000, and its `CMD` is `serve -addr 0.0.0.0:5000 -storage-root /var/lib/regixtry`. `docker-compose.yml` wires a `postgres:17-alpine` service plus the app image, but depends on `networks.dtf-netwok: external: true` (a manually-created Docker network) and hardcodes Postgres credentials/DSN in plaintext — it is a local dev helper, not a shippable compose file.

2. **Release automation (no container publishing exists)**: `.goreleaser.yaml` (project `regixtry`, version 2) builds only `linux/amd64` and `linux/arm64` binaries, archives them as `.tar.gz`, and produces a sha256 checksums file — no `dockers`/`dockers_v2`/`docker_manifests` block exists. `.github/workflows/release.yml` triggers on `v*` tags, runs `go test ./...`, runs `goreleaser release --clean` with only `permissions: contents: write` (no `packages: write`), verifies exactly one amd64 archive + one arm64 archive + one checksum file, then runs `docs/verification/scripts/install-release-smoke.sh`. Nothing in the repo references `ghcr.io` or `docker.io` as a publish target — confirmed via repo-wide grep — so there is no existing self-dogfooding or GHCR precedent to build on.

3. **The installer/CLI surface has evolved past both the `registry-deployment-installer` and `regixtry-setup-cli` explorations' framing** — the most important correction from this investigation:
   - `install.sh` today is **downloader-only**: it resolves a GitHub release, verifies checksum, extracts `regixtry`, and prints next steps referencing `regixtry setup --mode binary-only`, `sudo regixtry setup --mode daemon-sqlite --public-url ...`, `upgrade`, `uninstall`. It does **not** call `bootstrap` directly anymore, contradicting the `registry-deployment-installer/exploration.md` claim that `install.sh` "immediately runs `registry bootstrap --mode daemon-sqlite`" — that described an earlier commit, since superseded by the `regixtry-setup-cli` change.
   - `cmd/regixtry/main.go` exposes: `serve`, `tui`, `bootstrap`, `bootstrap-admin`, `setup`, `feature`, `uninstall`, `upgrade`. `setup` is the operator-facing command (added by `regixtry-setup-cli`, not yet archived) supporting `-mode binary-only|daemon-sqlite`, TTY-interactive prompting via `isInteractiveTTYPair`, and **already orchestrates Postgres-auth bootstrap inline** through `bootstrapSetupAuth()` → `bootstrapAdmin()` when `-auth-postgres-dsn` is set — creating/rotating the first admin as part of `setup`, not a separate manual step.
   - This directly contradicts a documented assumption in both `registry-deployment-installer/exploration.md` ("Postgres-auth installer orchestration... deferred") and `regixtry-setup-cli/exploration.md` ("Do not pull bootstrap-admin, Postgres DSN capture... into the first slice"). Current code proves that gap has since closed. The proposal phase for `registry-container-mode` must treat auth-in-setup as already shipped, not deferred.
   - `internal/infra/install/linux/bootstrap.go` remains the sole Linux+systemd backend — not container-relevant directly, but its provenance/rollback/uninstall pattern is the shape a container-mode lifecycle story would need to parallel (or explicitly not need, since containers get lifecycle "for free" via the runtime).
   - The non-archived `openspec/changes/` tree also contains `registry-bootstrap-preflight`, `regixtry-upgrade-lifecycle`, `registry-auth-v1` (apply-progress shows `bootstrap-admin` fully implemented with Postgres store, migrations, auth-enabled fail-fast startup) — confirming a chain of already-applied-but-not-yet-archived changes that materially moved the installer/auth surface since the two changes named above were written.

## Affected Areas

| Area | Impact | Description |
|------|--------|--------------|
| `Dockerfile` | Modified | Release-grade rework: non-root user, `HEALTHCHECK`, explicit volume convention for `/var/lib/regixtry`, `EXPOSE`/`CMD` kept in sync with `serve` defaults |
| `docker-compose.yml` | Out of scope (dev-only, unchanged) | Currently a local dev helper (external network dependency, plaintext creds); not touched by the narrow first slice |
| `.goreleaser.yaml` | Modified | Add a `dockers`/`dockers_v2` (GoReleaser v2.12+, alpha) or `docker_manifests` section plus a manifest-list step for multi-arch (`linux/amd64` + `linux/arm64`) publishing via `docker buildx` |
| `.github/workflows/release.yml` | Modified | Add `permissions: packages: write` (or equivalent registry credentials) plus a login step before GoReleaser can push images; current permissions are `contents: write` only |
| `cmd/regixtry/main.go` | Reference only | `setup`, `serve`, and `bootstrap-admin` are the config surfaces a container entrypoint must map onto; `setup`'s TTY-interactive prompting is not usable non-interactively inside a container — first-admin creation needs the existing `serve -auth-postgres-dsn` + `bootstrap-admin -password-stdin` primitives, not yet packaged into one container-friendly entrypoint |
| `install.sh` / chooser UX | Out of scope (deferred to a follow-up change) | Natural place to add a third "run as a container" guidance branch alongside `setup --mode binary-only|daemon-sqlite` |
| `docs/verification/scripts/install-release-smoke.sh` | New sibling script | Pattern to mirror for a container-focused smoke script (build/pull image, run, probe `/v2/`, verify data persists via volume) |
| `README.md` | Modified | Update once a release-grade image/compose story exists so the manual `docker network create dtf-netwok` step disappears from the documented path |

## Approaches

### 1. Release-image build + publish only (no installer UX change)
Rework `Dockerfile` to be release-grade (non-root, healthcheck, volume convention), add `dockers`/`docker_manifests` to `.goreleaser.yaml` for multi-arch GHCR publishing, add `packages: write` + login to `release.yml`, add a container smoke-test script. Leave `install.sh`/`setup` chooser untouched; README documents `docker run ghcr.io/...` as a new manual-but-first-class path.

- **Pros**: Narrow, reviewable, directly closes the release-automation gap both prior explorations flagged as the blocker; no interactive UX risk; independently testable via the smoke-script pattern already established.
- **Cons**: Does not yet give the unified "install as container or system app" chooser experience; container first-admin bootstrap still requires manual `bootstrap-admin` invocation against the running container.
- **Effort**: Medium

### 2. Full chooser integration
Everything in Approach 1, plus a new `install.sh`/`regixtry setup` "container" branch that generates a ready-to-run `docker run`/compose invocation (or shells out to Docker) as a peer of `binary-only`/`daemon-sqlite`, including a non-interactive first-admin creation flag suited to container startup.

- **Pros**: Delivers exact "container or system app" parity; single entrypoint story for operators.
- **Cons**: Needs Docker-availability detection, non-interactive secret-handling design inside a container-launch flow, and a materially bigger review/test surface; premature before Approach 1's image is proven release-grade.
- **Effort**: High

### 3. Dogfooding: publish release images to a self-hosted regixtry instance (option only, not a decision)
In addition to or instead of GHCR. Carries a circular-bootstrap risk (need a running, trusted registry instance to publish the images that run the registry) that the proposal phase should flag explicitly if pursued.

- **Pros**: Strong dogfooding signal, no external registry dependency for self-hosters already running regixtry.
- **Cons**: Chicken-and-egg availability/trust problem for first-time installs; adds an uptime dependency on the project's own infrastructure for its own release pipeline.
- **Effort**: Medium-High (orthogonal to 1/2)

## Recommendation

Start with **Approach 1** as the first proposable slice: make the container image release-grade and let release automation actually publish it (multi-arch, GHCR), closing exactly the gap `registry-deployment-installer/exploration.md` identified as the reason container installs were deferred. Defer Approach 2 (full chooser/UX parity) to a follow-up change once the image is proven in the wild, and note Approach 3 (self-hosted dogfooding) as an explicit open option for the proposal to accept or reject.

## Risks

- `.github/workflows/release.yml` currently grants only `contents: write`; publishing container images requires adding `packages: write` (GHCR) or equivalent external-registry credentials — a real workflow/security-surface change, not just a goreleaser config change.
- Reworking `Dockerfile` to run non-root and add a `HEALTHCHECK` can silently break the existing `docker-compose.yml` dev workflow (bind mounts, port assumptions) unless compose is updated in lockstep or explicitly marked as a separate, still-dev-only artifact.
- `setup`'s interactive-first-admin flow (`isInteractiveTTYPair`) has no non-interactive/container-native equivalent wired end-to-end yet; a container entrypoint needs a scripted `bootstrap-admin -password-stdin`-style path, which exists as a primitive but is not yet packaged as "the container way to create the first admin."
- Registry metadata persistence is still SQLite-only regardless of container mode; only auth state can move to Postgres today. Any container-mode messaging must stay honest that this is not yet a stateless/horizontally-scalable deployment story.
- Both `registry-deployment-installer` and `regixtry-setup-cli` explorations are stale on the auth-orchestration point — `setup` already implements Postgres-auth bootstrap inline. The proposal phase must re-verify current code rather than trusting either prior exploration's characterization of what is "deferred."
- Strict TDD applies: any Go-level entrypoint/config-parsing changes need `go test ./...` coverage; a new bash smoke script should follow the `install-release-smoke.sh` pattern for the container path specifically.

## Ready for Proposal

Yes — propose `registry-container-mode` scoped narrowly to Approach 1 (release-grade `Dockerfile` + multi-arch GoReleaser Docker publishing + workflow permissions + container smoke test), explicitly deferring full installer-chooser integration (Approach 2) and self-hosted dogfooding (Approach 3), and explicitly correcting the now-stale "Postgres-auth orchestration is deferred" assumption from the two prior explorations.

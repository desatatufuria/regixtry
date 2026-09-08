# Delta for container-release-image

## Current Repository Facts

- `Dockerfile` runs as root, has no `HEALTHCHECK`; `CMD` is `serve -addr 0.0.0.0:5000 -storage-root /var/lib/regixtry`.
- `.goreleaser.yaml` builds only `linux/amd64`/`linux/arm64` binaries; no `dockers`/`docker_manifests` block exists.
- `.github/workflows/release.yml` grants only `permissions: contents: write`; no `ghcr.io` login step exists.
- No `ghcr.io`/`docker.io` reference exists anywhere in the repository.
- `docs/verification/scripts/install-release-smoke.sh` is the existing binary smoke pattern (run, probe `/v2/`).
- `bootstrap-admin -password-stdin` is an existing, already-shipped primitive for creating the first admin.
- Brand-new capability: `openspec/specs/container-release-image/` does not exist; this delta is entirely additive.
- Production today already runs Postgres-backed auth via the manual `docker compose up -d postgres` + `bootstrap-admin -auth-postgres-dsn` + `serve -auth-postgres-dsn` recipe documented in `README.md`; this requirement proves container-image parity with that already-real pattern, not new auth behavior.

## ADDED Requirements

### Requirement: Hardened Non-Root Runtime

The image MUST run as a non-root user and MUST declare a `HEALTHCHECK` reflecting real service health.

#### Scenario: Image runs healthy as non-root

- GIVEN the published image starts with no user override
- WHEN the container process runs
- THEN it MUST run as non-root and its `HEALTHCHECK` MUST report unhealthy if `serve` stops responding

### Requirement: Persistent Storage Volume Convention

The image MUST use `/var/lib/regixtry` as its persistent SQLite storage path, matching the default entrypoint's write path.

#### Scenario: Volume-mounted data survives restart

- GIVEN a host volume is mounted at `/var/lib/regixtry`
- WHEN the container is stopped and restarted on that volume
- THEN previously stored data MUST still be present

### Requirement: Default Serve Entrypoint

The default `CMD`/entrypoint MUST launch `serve` with the same default address and storage-root as the binary release, needing no extra flags to start.

#### Scenario: Image serves with no arguments

- GIVEN an operator runs the image with no command override
- WHEN the container starts
- THEN `serve` MUST bind and become reachable at its documented default address

### Requirement: Multi-Arch Release Publishing

On every tagged release running `goreleaser release --clean`, the pipeline MUST build and push `linux/amd64` and `linux/arm64` images to `ghcr.io` under the project's image name, reusing GoReleaser-built binaries, using the workflow's existing `GITHUB_TOKEN` with `packages: write`.

#### Scenario: Tagged release publishes both architectures

- GIVEN a release run triggered by a `v*` tag
- WHEN `goreleaser release --clean` completes
- THEN a multi-arch `ghcr.io` manifest for that tag MUST cover both architectures
- AND publishing MUST NOT require a new external registry credential

### Requirement: Release Tagging Contract

Images MUST be tagged `vX.Y.Z`, `vX.Y`, `vX`, and `latest`; the floating tags MUST move only on non-prerelease releases.

#### Scenario: Stable release updates floating tags

- GIVEN a non-prerelease tag `vX.Y.Z`
- WHEN publish completes
- THEN `ghcr.io` MUST expose `vX.Y.Z`, `vX.Y`, `vX`, and `latest` pointing at that build

#### Scenario: Prerelease never overwrites floating tags

- GIVEN a tag marked as a prerelease
- WHEN publish completes
- THEN only the exact `vX.Y.Z` tag MUST be published
- AND `latest`, `vX.Y`, `vX` MUST NOT be created or overwritten

### Requirement: Release Container Smoke Verification

Release verification MUST include a container smoke test, mirroring `install-release-smoke.sh`, that runs the built image and probes `/v2/` before release is considered verified.

#### Scenario: Smoke test blocks a broken image

- GIVEN a release build produces a container image
- WHEN the smoke script runs the image and probes `/v2/`
- THEN a non-responsive probe MUST fail verification and a healthy probe MUST pass it

### Requirement: Container Postgres-Backed Auth Smoke Verification

Release verification MUST also exercise the published image with Postgres-backed authentication enabled, using the same `-auth-postgres-dsn`/`REGISTRY_AUTH_POSTGRES_DSN` mechanism already supported by `serve` and `bootstrap-admin`, proving auth actually gates access inside the container rather than only that the flag is accepted.

#### Scenario: Auth-enabled smoke path succeeds

- GIVEN a running Postgres instance reachable from the container and its DSN
- WHEN `bootstrap-admin -password-stdin` creates the first admin against that DSN and the image starts `serve` with the same `-auth-postgres-dsn`
- THEN an authenticated client MUST successfully interact with `/v2/` using those credentials
- AND an anonymous client MUST be rejected

#### Scenario: Anonymous-only image still works when no DSN is supplied

- GIVEN the image starts with no `-auth-postgres-dsn`/`REGISTRY_AUTH_POSTGRES_DSN` set
- WHEN the smoke test probes `/v2/` without credentials
- THEN the probe MUST succeed, matching the existing anonymous smoke path

### Requirement: Truthful Container Deployment Boundary

Container docs MUST present `docker run ghcr.io/...` as a manual, first-class path and MUST NOT claim `install.sh`/`regixtry setup` offer an automated container-selection branch.

#### Scenario: README documents the manual path without overclaiming

- GIVEN an operator reads the container section of `README.md`
- WHEN they follow the documented steps
- THEN they MUST see `docker run ghcr.io/...` plus `bootstrap-admin -password-stdin` for first-admin creation
- AND neither the docs nor `install.sh`/`setup` MUST claim an automated container-selection option
</content>

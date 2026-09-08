# Proposal: Registry Container Mode

## Intent

`registry-deployment-installer` deferred container installs as "manual today, automated later" and nothing closed that gap. `.goreleaser.yaml` has no `dockers` block, `release.yml` grants only `contents: write`, and no `ghcr.io` reference exists in the repo. The sole container artifact is a dev-quality `Dockerfile` (root user, no `HEALTHCHECK`). Operators wanting containers must build their own image. Make the image release-grade and let release automation publish it.

## Scope

### In Scope
- Release-grade `Dockerfile`: non-root user, `HEALTHCHECK`, `/var/lib/regixtry` volume convention, `EXPOSE`/`CMD` in sync with `serve` defaults.
- Multi-arch (`linux/amd64`, `linux/arm64`) image publishing to GHCR from `.goreleaser.yaml`.
- `permissions: packages: write` plus a registry login step in `.github/workflows/release.yml`.
- Container smoke-test script mirroring `docs/verification/scripts/install-release-smoke.sh`.
- `README.md`: `docker run ghcr.io/...` as a first-class manual path, first admin via existing `bootstrap-admin -password-stdin`.

### Out of Scope
- A third `container` branch in `install.sh` / `regixtry setup` (follow-up change).
- Publishing images to a self-hosted regixtry instance (dogfooding) — future direction only.
- Making `docker-compose.yml` shippable; it stays dev-only.
- New non-interactive first-admin UX beyond documenting the existing primitive.

## Capabilities

### New Capabilities
- `container-release-image`: published-image contract — runtime hardening, persistence convention, supported architectures, tagging scheme, publish verification.

### Modified Capabilities
- None. `installation-modes` governs `install.sh`/`setup` host lifecycle (systemd activation, provenance rollback). The image has no host lifecycle and the installer does not offer it in this slice, so amending that spec would claim installer support that does not exist.

## Approach

GoReleaser stays the single release authority and already builds both architectures, so image builds reuse those binaries instead of compiling inside Docker. Publish to GHCR: the release workflow already authenticates to GitHub, so `packages: write` on `GITHUB_TOKEN` adds no external credential. Tags `vX.Y.Z`, `vX.Y`, `vX`, `latest`; floating tags move only for non-prerelease releases. Verification mirrors the binary smoke shape: run, probe `/v2/`, restart on the same volume, assert persistence.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `Dockerfile` | Modified | Non-root user, HEALTHCHECK, volume/port convention |
| `.goreleaser.yaml` | Modified | Multi-arch image build and manifest publishing |
| `.github/workflows/release.yml` | Modified | `packages: write` scope, GHCR login |
| `docs/verification/scripts/container-release-smoke.sh` | New | Run/probe/persistence smoke |
| `README.md` | Modified | Documented `docker run` path |
| `openspec/specs/container-release-image/spec.md` | New | Capability spec |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Widened workflow permissions | Med | Scope `packages: write` to the release job; no new secret |
| Non-root rework breaks dev compose | Med | Validate `docker-compose.yml` in the same slice |
| Image implies stateless/scalable deploys | Med | Docs state metadata stays SQLite-only, single instance |
| Multi-arch publish unverifiable before tagging | Med | Smoke script runs against a locally built image first |

## Rollback Plan

Remove the `dockers`/`docker_manifests` blocks and revert workflow permissions; the binary release path is untouched, so releases keep working. Published images can be deleted or deprecated in GHCR — no operator upgrade path depends on them yet.

## Dependencies

- GoReleaser v2 with `docker buildx` multi-arch support on the release runner.
- Existing `serve` flag defaults and `bootstrap-admin -password-stdin`.
- Strict TDD: no Go change is planned; if `cmd/regixtry/main.go` entrypoint/config parsing becomes necessary, it lands with `go test ./...` coverage.

## Success Criteria

- [ ] A tagged release publishes a multi-arch GHCR manifest covering amd64 and arm64.
- [ ] The image runs as a non-root user and reports healthy via `HEALTHCHECK`.
- [ ] Smoke script proves `/v2/` reachable and data survives a restart on the same volume.
- [ ] `README.md` documents `docker run` including first-admin creation.
- [ ] Binary release artifacts and the existing install smoke test are unchanged.

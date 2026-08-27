# Tasks: Registry Container Mode

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines (total) | 580-780 |
| Chained PRs recommended | Yes |
| Suggested split | PR #1 (base) → PR #2 (3a) → PR #3 (3b), Feature Branch Chain |
| Delivery strategy | auto-chain (resolved from `single-pr`; chain strategy is no longer pending) |
| Chain strategy | feature-branch-chain |
| 400-line budget risk (skill default, per-PR) | Low for PR #1/#2, Medium for PR #3 — each PR now sits well under the strict 400-line skill default |

Decision needed before apply: No
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: Medium

Re-estimate rationale (unchanged from prior amendment): `run_auth_scenario` adds ephemeral-network lifecycle, a sibling `postgres:17-alpine` container with a `pg_isready` poll loop, a `bootstrap-admin -password-stdin` invocation gating `serve` startup, a Basic→token→Bearer exchange against `/auth/token`, three assertions (401 liveness, anonymous rejection, authenticated blob PUT/HEAD), and extended cleanup ordering. Combined with shared-helper extraction, this pushed the total from 450-580 to 580-780. The chain below splits that total into three independently reviewable slices instead of accepting `size:exception`.

### Chain Overview

```text
develop
 └── feature/registry-container-mode                     ← tracker (draft, no direct merge until chain completes)
      ↑ PR #1 base: feature/registry-container-mode
      └── feature/registry-container-mode-01-image-publish
           ↑ PR #2 base: feature/registry-container-mode-01-image-publish
           └── feature/registry-container-mode-02-smoke-anonymous
                ↑ PR #3 base: feature/registry-container-mode-02-smoke-anonymous
                └── feature/registry-container-mode-03-smoke-auth
```

Only `feature/registry-container-mode` merges into `develop`, and only after PR #3 is reviewed and integrated.

### Suggested Work Units (per PR)

| PR | Branch | Base | Goal | Est. lines | Focused test command | Runtime harness | Rollback boundary |
|----|--------|------|------|-----------|----------------------|-----------------|-------------------|
| #1 | `feature/registry-container-mode-01-image-publish` | `feature/registry-container-mode` | Healthcheck subcommand, multi-stage Dockerfile, GoReleaser image publishing, CI perms/login/smoke-step wiring, anonymous README | 180-230 | `go test ./cmd/regixtry/...` | `docker build --target=release .` + `docker run`, poll `/v2/`; `docker build .` (dev, unchanged) | Revert this branch entirely; nothing outside `cmd/regixtry`, `Dockerfile`, `.goreleaser.yaml`, `.github/workflows/release.yml`, README anon section is touched |
| #2 (3a) | `feature/registry-container-mode-02-smoke-anonymous` | PR #1 branch | Smoke script shared helpers + `run_anonymous_scenario`, CI smoke-step wiring completion, anonymous local verification | 150-200 | `bash docs/verification/scripts/container-release-smoke.sh --image <local-tag>` | Anonymous smoke: multiarch inspect, non-root, health poll, blob persistence across restart | Revert `container-release-smoke.sh` (script did not exist before this PR) |
| #3 (3b) | `feature/registry-container-mode-03-smoke-auth` | PR #2 branch | `run_auth_scenario`, `--auth-postgres` flag, Postgres-auth README recipe, auth local verification | 250-350 | `bash docs/verification/scripts/container-release-smoke.sh --image <local-tag> --auth-postgres` | Auth smoke: ephemeral network, sibling Postgres, bootstrap-admin, token exchange, authenticated blob PUT/HEAD | Revert `run_auth_scenario`, `--auth-postgres` flag parsing, and the README auth-recipe subsection; `run_anonymous_scenario` from PR #2 keeps working unmodified |

Sum: 580-780 lines, matching the total re-estimate. PR #3 carries the largest budget because it is the most security-sensitive cluster (credentials via stdin, DSN wiring, ephemeral network, bootstrap-admin gating, token exchange, three assertions) and therefore needs the most reviewer attention.

---

## PR #1 (base) — targets `feature/registry-container-mode`

**Branch**: `feature/registry-container-mode-01-image-publish`
**Scope**: Healthcheck subcommand, Dockerfile, GoReleaser image publishing, CI wiring (no smoke script yet), anonymous README section.
**Chain note**: Phase 4.3 adds a CI step invoking `container-release-smoke.sh`, which does not exist until PR #2. On this branch in isolation, that CI step will fail at execution — expected in a Feature Branch Chain; it resolves once PR #2 stacks on top.

### Phase 1: Healthcheck Subcommand (Strict TDD)

- [x] 1.1 RED: table-driven tests in `cmd/regixtry/main_test.go` (or new `healthcheck_test.go`) covering `httptest` responses 200/401/500, connection-refused, timeout, plus `-url`/`-timeout` flag defaults/overrides.
- [x] 1.2 GREEN: implement `healthcheck` subcommand in `cmd/regixtry/main.go` — `-url` default `http://127.0.0.1:5000/v2/`, `-timeout` default `3s`, exit 0 on 200/401, exit 1 otherwise.
- [x] 1.3 GREEN: wire `healthcheck` into the `runWithIO` switch and the no-args subcommand list error (`main.go:168`).
- [x] 1.4 REFACTOR: `go vet ./...` and `gofmt -w .`; confirm no shared-state leaks between subcommands.

### Phase 2: Dockerfile Multi-Stage Rewrite

- [x] 2.1 Add `runtime-base` stage: `debian:bookworm-slim`, `ca-certificates`, user `65532`, owned `/var/lib/regixtry`, `VOLUME`, `EXPOSE 5000`, `USER`, exec-form `HEALTHCHECK` calling `regixtry healthcheck`, `ENTRYPOINT`/`CMD`.
- [x] 2.2 Add `release` stage `FROM runtime-base`, copying the GoReleaser-built binary via build context.
- [x] 2.3 Keep Go compile as `build` stage; re-add current dev image as `dev` stage `FROM runtime-base`, staged last so `docker build .` and `docker-compose.yml` keep today's behavior.
- [x] 2.4 Locally verify `docker build .` (implicit `dev`) and `docker build --target=release .` (stub binary) both succeed.

### Phase 3: GoReleaser Multi-Arch Image Publishing

- [x] 3.1 Add `dockers` block to `.goreleaser.yaml`: amd64/arm64 entries, `use: buildx`, `--target=release`, `--platform`, OCI labels, both arch image templates.
- [x] 3.2 Add `docker_manifests` block per design: always-pushed `{{ .Tag }}`, plus `v{{ .Major }}.{{ .Minor }}`, `v{{ .Major }}`, `latest` each `skip_push: auto`.

### Phase 4: CI Workflow — Permissions, Login, Smoke-Step Wiring

- [x] 4.1 Add `packages: write` to `permissions` in `.github/workflows/release.yml`.
- [x] 4.2 Add `docker/login-action@v3` (GHCR, `GITHUB_TOKEN`), `docker/setup-qemu-action@v3`, `docker/setup-buildx-action@v3` before "Run GoReleaser", login first.
- [x] 4.3 Add a final "Run container smoke verification" step invoking `container-release-smoke.sh` against `ghcr.io/desatatufuria/regixtry:${GITHUB_REF_NAME}` (script content lands in PR #2; this step is scaffolding only in PR #1).

### Phase 6.1a: README — Anonymous Container Usage

- [ ] 6.1a Add `## Run as a container` to `README.md` after `## Install`: anonymous `docker run ghcr.io/...` example, `bootstrap-admin -password-stdin` note for the first admin, explicit note that `install.sh`/`regixtry setup` do not offer a container branch. (Postgres-auth recipe deferred to PR #3.)

### Phase 6.2/6.4: PR #1 Verification

- [ ] 6.2 Run `go test ./...` and confirm Phase 1 healthcheck tests pass.
- [ ] 6.4 Confirm `docker-compose.yml` still builds/runs unchanged against the `dev` stage.

---

## PR #2 (3a) — targets PR #1 branch (`feature/registry-container-mode-01-image-publish`)

**Branch**: `feature/registry-container-mode-02-smoke-anonymous`
**Scope**: Smoke script shared helpers + anonymous scenario only; completes CI smoke-step wiring; anonymous local verification.

### Phase 5a: Container Smoke Script — Shared Helpers + Anonymous Scenario

- [ ] 5a.1 Create `docs/verification/scripts/container-release-smoke.sh`, sibling of `install-release-smoke.sh`; implement shared helpers used by both scenarios: `fail()`, `cleanup()` trap, `wait_healthy()`, `http_status()`, `registry_token()`.
- [ ] 5a.2 Implement `--image <ref>` and `--expect-multiarch` flag parsing (`--auth-postgres` deferred to PR #3); optional `docker buildx imagetools inspect` platform check.
- [ ] 5a.3 Implement `run_anonymous_scenario()`: named volume, run detached, assert `Config.User`/in-container `id -u` non-root, poll `State.Health.Status` to `healthy`, blob upload over `/v2/`, container destroy, re-run on same volume, `HEAD` the blob digest for 200 (persistence proof).
- [ ] 5a.4 Implement cleanup ordering for the anonymous scenario: registry container → named volume; every step `|| true` so a mid-scenario failure still tears down.
- [ ] 5a.5 Wire CLI dispatch: always call `run_anonymous_scenario` (`run_auth_scenario` dispatch added in PR #3).
- [ ] 4.4 Verify the CI smoke-verification step added in PR #1 (4.3) now resolves against `container-release-smoke.sh` and passes end-to-end on this branch, since it is stacked on PR #1 and includes the script for the first time.

### Phase 6.3a: PR #2 Verification

- [ ] 6.3a Run `container-release-smoke.sh` locally against `docker build --target=release .` without `--auth-postgres`, to prove the anonymous integration layer before relying on CI's E2E run.

---

## PR #3 (3b) — targets PR #2 branch (`feature/registry-container-mode-02-smoke-anonymous`)

**Branch**: `feature/registry-container-mode-03-smoke-auth`
**Scope**: `run_auth_scenario` added to the same smoke script file, `--auth-postgres` flag, Postgres-auth README recipe, auth-scenario local verification.

### Phase 5b: Container Smoke Script — Postgres-Auth Scenario

- [ ] 5b.1 Extend flag parsing in `container-release-smoke.sh` with `--auth-postgres`.
- [ ] 5b.2 Implement `run_auth_scenario()` (behind `--auth-postgres`): create ephemeral `regixtry-smoke-${RUN_ID}` docker network; start `postgres:17-alpine` (same pin as `docker-compose.yml`) on that network; poll `pg_isready -U registry -d regixtry_auth` before continuing.
- [ ] 5b.3 In `run_auth_scenario()`, run `docker run --rm -i <image> bootstrap-admin -auth-postgres-dsn "${DSN}" -username admin -password-stdin` (piped password, never argv) and confirm success *before* starting the serve container.
- [ ] 5b.4 In `run_auth_scenario()`, start `serve` with `-e REGISTRY_AUTH_POSTGRES_DSN="${DSN}"`, then assert: anonymous `GET /v2/` → `401` + `WWW-Authenticate`; `GET /auth/token` with Basic `admin:${ADMIN_PASSWORD}` → `200` + `token` via `registry_token()`; authenticated `Bearer <token>` blob `POST`/`PUT`/`HEAD` → `200`/`202`/`201`/`200`; the same unauthenticated `HEAD` MUST be `401`.
- [ ] 5b.5 Extend cleanup ordering to include the auth scenario: registry container → Postgres container → named volume → ephemeral network last (endpoints detached first); every step `|| true`.
- [ ] 5b.6 Extend CLI dispatch: call `run_auth_scenario` only when `--auth-postgres` is passed.

### Phase 6.1b: README — Postgres-Auth Container Recipe

- [ ] 6.1b Add a Postgres-auth recipe to the `## Run as a container` section, mirroring `## Quick start with authentication and access control` (`README.md:37-52`): same `postgres:17-alpine`, same `regixtry_auth`/`registry` DSN shape, same `bootstrap-admin -password-stdin` → `serve -auth-postgres-dsn` order, expressed with `docker network create` + `docker run` instead of `docker compose`.

### Phase 6.3b: PR #3 Verification

- [ ] 6.3b Run `container-release-smoke.sh` locally against `docker build --target=release .` with `--auth-postgres`, to prove the auth integration layer (bootstrap-admin, token exchange, authenticated access) before relying on CI's E2E run.
</content>

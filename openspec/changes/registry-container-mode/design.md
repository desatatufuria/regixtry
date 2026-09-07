# Design: Registry Container Mode

## Technical Approach

GoReleaser stays the single release authority. It already produces static `linux/amd64` and `linux/arm64` binaries, so the published image **copies** those exact artifacts instead of compiling inside Docker. The existing root `Dockerfile` becomes multi-target so the dev source-build (`docker-compose.yml`) and the release binary-copy image share one runtime definition and cannot drift. `release.yml` gains `packages: write`, a GHCR login, buildx/QEMU setup, and a post-publish container smoke gate that verifies the image both anonymously **and** with Postgres-backed auth, because Postgres auth — not anonymous mode — is what the project actually deploys. This implements `container-release-image`; `installation-modes` stays untouched because `install.sh` / `regixtry setup` still do not offer a container branch.

## Architecture Decisions

| Decision | Options | Tradeoff | Choice |
|---|---|---|---|
| Image build input | Compile in Dockerfile; copy GoReleaser binary | Compiling duplicates the build and needs emulated arm64 toolchains | Copy the already-built, already-tested GoReleaser binary |
| Dockerfile layout | Second `Dockerfile.release`; one file with BuildKit targets | Two files duplicate user/healthcheck/volume rules and silently drift | One `Dockerfile`, stages `runtime-base` → `release` → `build` → `dev` (last) |
| Healthcheck tool | `apt-get install curl`/`wget`; new `regixtry healthcheck` subcommand | curl adds ~10–15 MB plus a recurring TLS/CVE surface in an image whose own project ships Trivy scanning | Go subcommand: zero image bytes, unit-testable under strict TDD, exec-form `HEALTHCHECK` needs no shell |
| Health semantics | 200 only; 200 or 401 | `/v2/` answers `401` + `WWW-Authenticate` when auth is on (`internal/protocol/http/router.go:688-727`, `writeError`), so 200-only marks every authenticated deployment unhealthy | 200 **or** 401 = healthy; any other status, dial/TLS error, or timeout = unhealthy |
| Runtime base | Keep `debian:bookworm-slim`; distroless static-nonroot | Distroless is smaller with fewer CVEs but swaps the base for dev and release at once and removes the debug shell | Keep slim; accept that its `RUN apt-get ca-certificates` needs QEMU for arm64. Distroless is a follow-up |
| Floating tags | Template conditionals; `skip_push: auto` | A conditional that renders an empty `name_template` is invalid GoReleaser config | Separate `docker_manifests` entries; floating ones carry `skip_push: auto` (GoReleaser skips push when the version is a prerelease) |
| Publish coupling | Separate image-publish job; one `goreleaser release` run | A second job re-derives version/tag state and can diverge from the release | One run, with the GHCR login **before** GoReleaser so credential failures cost nothing |
| Auth-smoke Postgres | Reuse `docker-compose.yml`; host/CI service Postgres; sibling container on an ephemeral network | Compose is dev-only and needs the pre-existing external `dtf-netwok`; a CI service container makes local runs non-hermetic | Sibling `postgres:17-alpine` (same pin as `docker-compose.yml`) on a uniquely named ephemeral bridge network created and destroyed by the script |
| First-admin creation | `docker exec` into the running registry; one-shot container from the same image | `serve` **fails fast** when auth is on and no admin exists (`newHandler` returns `bootstrap-admin` guidance — `cmd/regixtry/main_test.go:2592-2609`), so there is never a running container to exec into | `docker run --rm -i <image> bootstrap-admin -password-stdin`, **before** the serve container starts |
| Authenticated probe | `curl -u admin:pw /v2/`; token exchange then Bearer | `authenticate` ignores any non-`Bearer` `Authorization` header (`internal/protocol/http/router.go:574-600`), so Basic on `/v2/` is silently anonymous and would return a false 401 | `GET /auth/token` with Basic credentials → `Authorization: Bearer <token>` on `/v2/` |
| Auth-smoke packaging | Second script; one script, second scenario function | Two scripts duplicate cleanup/trap/wait/http helpers and drift, and `install-release-smoke.sh` already proves the per-scenario-function shape | Extend `container-release-smoke.sh` with one `run_auth_scenario` function behind `--auth-postgres`, off by default |

Stage order is load-bearing: `dev` is last, so bare `docker build .` and the unchanged `docker-compose.yml` keep today's behavior, while GoReleaser passes `--target=release` and BuildKit never builds the Go `build` stage in its binary-only context.

## Data Flow

```text
git tag vX.Y.Z
   ↓
release.yml: go test → ghcr login → qemu+buildx → goreleaser release --clean
   ↓                                      ↓
dist/ binaries ──copied into──→ Dockerfile --target=release (amd64 | arm64)
   ↓                                      ↓
archives + checksums            :vX.Y.Z-amd64 , :vX.Y.Z-arm64
   ↓                                      ↓
verify artifacts → install smoke   docker_manifests → :vX.Y.Z
                                        (+ :vX.Y :vX :latest unless prerelease)
                                          ↓
                              container-release-smoke.sh (last workflow step)
```

Auth scenario ordering is load-bearing (`serve` refuses to start without an admin):

```text
docker network create <ephemeral>
   ↓
postgres:17-alpine ──(pg_isready poll)──→ ready
   ↓
docker run --rm -i <image> bootstrap-admin -auth-postgres-dsn … -password-stdin   ← admin must exist FIRST
   ↓
docker run -d <image> serve … -auth-postgres-dsn …   ← would exit non-zero if run before bootstrap
   ↓
GET /v2/ anonymous → 401 + WWW-Authenticate      (auth gates)
GET /auth/token (Basic admin) → token → Bearer /v2/ → 200, blob PUT/HEAD → 200   (auth grants)
   ↓
trap: rm containers → rm volume → rm network (network last: endpoints must be gone)
```

## File Changes

| File | Action | Description |
|---|---|---|
| `Dockerfile` | Modify | `runtime-base` stage (ca-certificates, system user 65532, `install -d` owned `/var/lib/regixtry`, `VOLUME`, `EXPOSE 5000`, `USER`, exec-form `HEALTHCHECK`, `ENTRYPOINT`/`CMD`); `release` stage copies the GoReleaser binary; `dev` stage stays the source build and stays last |
| `cmd/regixtry/main.go` | Modify | Add `healthcheck` subcommand to the `runWithIO` switch and to the subcommand list in the no-args error (`main.go:168`) |
| `.goreleaser.yaml` | Modify | Two `dockers` entries (`use: buildx`, `--target=release`, `--platform`, OCI labels) plus four `docker_manifests` entries |
| `.github/workflows/release.yml` | Modify | `packages: write`; `docker/login-action@v3`, `docker/setup-qemu-action@v3`, `docker/setup-buildx-action@v3` before GoReleaser; container smoke as the final step |
| `docs/verification/scripts/container-release-smoke.sh` | Create | Sibling of `install-release-smoke.sh`. Two scenarios in one file: `run_anonymous_scenario` (always) and `run_auth_scenario` (only under `--auth-postgres`), sharing `fail`/`cleanup`/`wait_healthy`/`http_status`/`registry_token` helpers. No second script |
| `README.md` | Modify | New `## Run as a container` section after `## Install`, with an anonymous `docker run` path **and** a Postgres-auth recipe mirroring the existing `## Quick start with authentication and access control` block (`README.md:37-52`) — same `postgres:17-alpine`, same `regixtry_auth`/`registry` DSN shape, same `bootstrap-admin -password-stdin` → `serve -auth-postgres-dsn` order, expressed with `docker network create` + `docker run` instead of `docker compose` |

## Interfaces / Contracts

```yaml
# .goreleaser.yaml — floating tags move only on non-prereleases
docker_manifests:
  - name_template: "ghcr.io/desatatufuria/regixtry:{{ .Tag }}"          # always
  - name_template: "ghcr.io/desatatufuria/regixtry:v{{ .Major }}.{{ .Minor }}"
    skip_push: auto
  - name_template: "ghcr.io/desatatufuria/regixtry:v{{ .Major }}"
    skip_push: auto
  - name_template: "ghcr.io/desatatufuria/regixtry:latest"
    skip_push: auto
```

`{{ .Tag }}` (not `{{ .Version }}`) keeps the leading `v`. Every entry lists the same two `-amd64`/`-arm64` `image_templates`, which the `dockers` entries always push.

```text
regixtry healthcheck [-url http://127.0.0.1:5000/v2/] [-timeout 3s]
  exit 0 → HTTP 200 or 401        exit 1 → other status, dial/TLS error, timeout

container-release-smoke.sh --image <ref> [--expect-multiarch] [--auth-postgres]
  fails with "container-release-smoke: <reason>" on stderr and a non-zero exit
  --auth-postgres also runs the Postgres-backed auth scenario; requires a pullable postgres:17-alpine
```

**Anonymous smoke sequence** (unchanged, always runs): optional `docker buildx imagetools inspect` must list both platforms; run detached on a named volume with anonymous push enabled; assert `Config.User` and in-container `id -u` are non-root; poll `State.Health.Status` to `healthy`; upload a small blob over `/v2/`; destroy the container; re-run on the same volume; `HEAD` the blob digest → 200.

**Auth smoke sequence** (`--auth-postgres`, second scenario, own container/volume/network names):

```bash
docker network create "regixtry-smoke-${RUN_ID}"
docker run -d --name "pg-${RUN_ID}" --network "regixtry-smoke-${RUN_ID}" \
  -e POSTGRES_DB=regixtry_auth -e POSTGRES_USER=registry -e POSTGRES_PASSWORD=registry \
  postgres:17-alpine
# poll: docker exec "pg-${RUN_ID}" pg_isready -U registry -d regixtry_auth
DSN="postgres://registry:registry@pg-${RUN_ID}:5432/regixtry_auth?sslmode=disable"
printf '%s\n' "${ADMIN_PASSWORD}" | docker run --rm -i --network "regixtry-smoke-${RUN_ID}" \
  "${IMAGE}" bootstrap-admin -auth-postgres-dsn "${DSN}" -username admin -password-stdin
docker run -d --name "reg-${RUN_ID}" --network "regixtry-smoke-${RUN_ID}" -p 127.0.0.1:0:5000 \
  -v "vol-${RUN_ID}:/var/lib/regixtry" -e REGISTRY_AUTH_POSTGRES_DSN="${DSN}" "${IMAGE}"
```

Three assertions, deliberately distinct:

1. **Liveness** — poll `State.Health.Status` to `healthy`. `HEALTHCHECK` treats 200 **or** 401 as healthy, so with auth on it goes healthy *via* 401. This proves the process serves; it proves nothing about auth.
2. **Auth rejects** — `GET /v2/` with no credentials MUST be `401` and MUST carry a `WWW-Authenticate` header. This is the *expected, correct* outcome, not a failure; conflating it with assertion 1 would make a broken-auth image look verified.
3. **Auth grants** — `GET /auth/token?service=<host>&scope=repository:smoke/auth:pull,push` with Basic `admin:${ADMIN_PASSWORD}` → `200` + a `token` field; then with `Authorization: Bearer <token>`: `GET /v2/` → `200`, `POST /v2/smoke/auth/blobs/uploads/` → `202`, `PUT ...?digest=sha256:<d>` → `201`, `HEAD /v2/smoke/auth/blobs/sha256:<d>` → `200`. The same `HEAD` without the header MUST be `401`.

Basic credentials on `/v2/` are *not* a shortcut: `authenticate` ignores non-`Bearer` schemes, so `curl -u` there yields a misleading 401. The token exchange is mandatory.

Cleanup trap (both scenarios): remove registry container → Postgres container → named volume → ephemeral network, in that order; `docker network rm` fails while endpoints are attached. Every step is `|| true` so a mid-scenario failure still tears down.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `healthcheck` status mapping (200, 401, 500, refused, timeout) and flag parsing | Table-driven Go tests against `httptest`, run by `go test ./...` |
| Integration | Non-root runtime, `HEALTHCHECK` reaching `healthy`, volume ownership | `container-release-smoke.sh` against a locally built `--target=release` image |
| Integration (auth) | Container/production parity: `bootstrap-admin` against a sibling Postgres, anonymous `/v2/` → 401, Bearer `/v2/` + blob push/pull → 200 | `container-release-smoke.sh --auth-postgres`, second scenario, sibling `postgres:17-alpine` on an ephemeral network. Liveness (`healthy` via 401) and "auth rejects anonymous" are asserted separately |
| E2E | Multi-arch manifest, `/v2/` reachability, restart persistence | Same script in `release.yml` against the published `ghcr.io/...:${GITHUB_REF_NAME}` |
| E2E (auth) | Same auth parity against the *published* image, not just a local build | `release.yml` passes `--auth-postgres` on the same final step; production runs Postgres auth, so a release verified only in anonymous mode verifies a configuration nobody deploys |

## Threat Matrix

| Boundary | Applicability | Design response |
|---|---|---|
| Documentation-like paths | N/A — nothing classifies files for execution | None |
| Git repository selection | N/A — no repo/cwd selection | None |
| Commit state | N/A — no commit automation | None |
| Push state | N/A — image push is GoReleaser's, not git | None |
| PR commands | N/A — no PR/VCS command composition | None |

The new shell surfaces are the smoke script's `--image` reference, the generated DSN, and the admin password. All three are passed as quoted literal arguments to `docker` (the password via stdin into `-password-stdin`, never argv), never interpolated into a shell string and never echoed.

## Migration / Rollout

No data migration. Rollout is one release: config and docs land together so the documented tags exist the moment the README claims them.

**Rollback.** `goreleaser release` runs the docker publish pipe inside the same invocation, so a push failure fails the release step non-zero and the binary release may not complete — image publishing is *not* failure-isolated from the binary release. Mitigations: GHCR login runs first (credential faults cost one cheap step), and the smoke gate runs last so container problems never mask binary verification. To roll back, delete the `dockers`/`docker_manifests` blocks and `packages: write`, then re-run `goreleaser release --clean` on the same tag; published images can be deleted in GHCR because no upgrade path depends on them yet.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Auth scenario lengthens the release job | High | Pulling `postgres:17-alpine` (~80–120 MB), `pg_isready` polling, bootstrap, and a second registry boot add roughly 60–90 s. Accepted: it runs once per tag, as the last step, after binary verification already passed |
| `container-release-smoke.sh` outgrows readability | Med | One file, one extra function, one flag — no second script. The auth scenario reuses every helper and is skipped entirely without `--auth-postgres`. Review the function's size at apply time; split only if it exceeds the anonymous scenario |
| Ephemeral network collides with a real one | Low | Names are `regixtry-smoke-${RUN_ID}`-suffixed and never `dtf-netwok`; the trap removes them. The smoke never touches the operator's compose network |
| `bootstrap-admin` password leaks into argv or logs | Med | `-password-stdin` only, piped into `docker run --rm -i`; never `-password` (which the CLI itself warns about at `cmd/regixtry/main.go:677`). The DSN carries a throwaway `registry:registry` credential |
| Postgres readiness race | Med | Poll `pg_isready -U registry -d regixtry_auth` (the exact `docker-compose.yml` healthcheck) with a bounded retry before bootstrap; `serve` would otherwise fail fast and look like an image defect |

`docker network create` needs no new workflow permission — `ubuntu-latest` runners ship a usable Docker daemon and the job already runs `docker` for buildx/login. `packages: write` stays the only permission change.

## Open Questions

- [ ] TLS deployments need `HEALTHCHECK` overridden with an `https://` `-url`; document or auto-detect later.
- [ ] Bind mounts (unlike named volumes) need a host-side `chown` to uid 65532 — README note only, for now.
- [ ] Should the auth scenario also assert a *non-admin* grant (repo-reader denied push)? Out of scope here: this slice proves container/production parity of the auth wiring, not the grant model, which `operator-admin-http-api` already covers.

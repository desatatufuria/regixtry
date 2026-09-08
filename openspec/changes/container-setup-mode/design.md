# Design: Container Setup Mode

## Technical Approach

`docker` becomes a third case in the **existing** chooser — `resolveSetupMode` (`cmd/regixtry/main.go:1785`) gains a `3) docker` option and a `"docker"` literal branch, and `runSetup` (`main.go:1268`) gains a `case "docker"` next to `daemon-sqlite`. It cannot reuse the systemd path underneath: `installlinux.ValidateMode` (`bootstrap.go:221-230`) rejects every mode but `daemon-sqlite`, and `PlanLifecycleProvenance` calls `ValidateConfig` first, so `runner.Run`, `ValidateConfig`, and `PlanLifecycleProvenance` are all unreachable for `docker` by construction. Rather than widen `supportedMode` (which would let a compose config reach the systemd bootstrapper), `docker` gets a sibling port, `internal/infra/install/compose`, reached through a `var newComposeRunner` seam mirroring `newBootstrapRunner` (`main.go:52`). That seam is what makes strict TDD possible: `go test ./...` drives the whole orchestration against a fake, and no unit test needs a Docker daemon.

Auth is always on in `docker` mode, so the case reuses `validateSetupAuthConfig` and `bootstrapSetupAuth`'s **contract** but not its in-process implementation — the admin is created by a one-shot container, the ordering `container-release-smoke.sh:300` already proved, because `serve` fails fast when auth is on and no admin exists. This implements the `installation-modes` delta; `binary-only` and `daemon-sqlite` are not touched.

## Architecture Decisions

| Decision | Options | Tradeoff | Choice |
|---|---|---|---|
| Where `docker` lives | Widen `installlinux.supportedMode`; new `compose` package | Widening lets a compose config reach `ValidateConfig`/`runner.Run`/systemd rollback, silently breaking the "daemon-sqlite unchanged" guarantee | New `internal/infra/install/compose` behind a `composeRunner` interface + `newComposeRunner` seam |
| Bundled vs external selection | New `-external-postgres-dsn`; reuse `-auth-postgres-dsn` | Two overlapping DSN flags confuse a shared parser | Reuse `-auth-postgres-dsn`: supplied = external, absent + TTY = prompt, absent + non-TTY = bundled |
| Skipping the bundled service | Compose `profiles:`; `depends_on.required:false`; two compose files; `up --no-deps regixtry` | Profiles break `depends_on`; `required:false` needs Compose ≥2.20; a merge override cannot delete a service or a `depends_on` key; two files duplicate the `regixtry` service | **One** compose file. Bundled = `up -d`; external = `up -d --no-deps regixtry`. Oldest, most portable Compose semantics |
| Compose file source of truth | Generate YAML in Go; embed one reviewed file | Generated YAML is unreviewable and untestable as text | `//go:embed` one asset; repo-root `docker-compose.yml` is a byte-identical copy, held by a drift test — same "cannot drift" rule the `Dockerfile` stages already follow |
| Bundled credential | Literal in compose; prompt; `crypto/rand` | A literal is the exact defect being removed; a prompt blocks non-interactive installs | `crypto/rand`, written to a `0600` env file, printed **once** at the end |
| Secret surface | Print path only; print path + secret | Path-only silently strips the operator's only copy of a generated secret from the terminal | Both: `0600` env file **and** one on-screen print. Never in argv, never in the compose file, never in provenance |
| Postgres readiness | `up --wait`; poll `pg_isready` | `--wait` needs v2.17+ and would also wait on `regixtry`, which cannot be healthy before bootstrap | Bounded `docker compose exec -T postgres pg_isready` poll — the `wait_postgres_ready` shape already proven |
| Admin password | Generate a second secret; require the existing flag/prompt | A second generated secret doubles the leak surface for no gain | Reuse `-admin-password` / the existing prompt; `validateSetupAuthConfig` already errors truthfully when absent |
| Docker provenance file | Reuse `regixtry-lifecycle-state.json`; separate file | `normalizeLifecycleProvenance` accepts **any** non-empty mode, so today's `uninstall` would happily run `systemctl stop` against a compose stack and never run `docker compose down` | Separate `regixtry-compose-state.json` in the project dir; today's `uninstall`/`upgrade` cannot mistake it for a systemd install |
| Image tag | `:latest`; pin to the setup binary's version | `latest` lets the container drift from the CLI that installed it | `REGIXTRY_IMAGE` pinned to the binary's own release version, `latest` only for dev builds |

## Data Flow

Ordering is load-bearing — `serve` refuses to start with auth on and no admin:

```text
regixtry setup                (or setup --mode docker)
   ↓  resolveSetupMode → "docker"
preflight: docker compose version   ── absent/old/daemon-down → truthful error, nothing written
   ↓
crypto/rand password → /etc/regixtry/compose/regixtry.env (0600)
                     + docker-compose.yml (embedded copy)
   ↓
bundled:  docker compose up -d postgres        external: (skipped)
   ↓  poll pg_isready (bounded)
docker compose run --rm --no-deps -T regixtry bootstrap-admin -password-stdin   ← admin FIRST
   ↓
bundled: docker compose up -d      external: docker compose up -d --no-deps regixtry
   ↓  poll GET <public-url>/v2/ → 200 or 401   (same semantics as `regixtry healthcheck`)
write regixtry-compose-state.json (0600)
   ↓
print: reachable + env-file path + generated password (once) + docker login + docker exec … tui
```

Any failure after `up` runs `docker compose down --volumes` and removes the generated files — the compose analogue of `rollbackSetupFailure` (`main.go:2073`).

## File Changes

| File | Action | Description |
|---|---|---|
| `cmd/regixtry/main.go` | Modify | `resolveSetupMode`: `"docker"` literal + `3) docker` menu entry + non-TTY error text; `runSetup`: `case "docker"`; `promptSetupDockerConfig` (bundled-vs-external prompt, mirroring `promptSetupDaemonConfig`); `var newComposeRunner` seam |
| `internal/infra/install/compose/compose.go` | Create | `Provisioner` implementing `composeRunner`: preflight, project write, staged `up`, one-shot `bootstrap-admin`, reachability poll, provenance, `Down`. All `exec.CommandContext` argv slices — never `sh -c` |
| `internal/infra/install/compose/assets/docker-compose.yml` | Create | The embedded canonical compose project |
| `docker-compose.yml` | Rewrite | Byte-identical copy of the embedded asset: pulls `${REGIXTRY_IMAGE}`, no `build:`, no `networks:`/`external: true`, no literal credentials, `depends_on: condition: service_healthy`, healthchecks and named volumes for both services |
| `.env.example` | Create | Documents `REGIXTRY_POSTGRES_PASSWORD`, `REGIXTRY_AUTH_POSTGRES_DSN`, `REGIXTRY_IMAGE`, `REGIXTRY_PORT` for manual `docker compose` users |
| `install.sh` | Modify | One line after `install.sh:332`: `- sudo ${privileged_command} setup --mode docker --public-url http://127.0.0.1:5000` |
| `README.md` | Modify | New `### One command: regixtry setup --mode docker` as the first subsection of `## Run as a container`, above the manual recipes; new `### The TUI against a container`; **`README.md:77` ("`install.sh` and `regixtry setup` do not offer a container-selection branch") becomes false and must be rewritten**; drop the `docker network create dtf-netwok` step at `README.md:39`; migration note for dev users of the old file |
| `docs/verification/scripts/setup-docker-smoke.sh` | Create | Clean-state stack smoke, sibling of `container-release-smoke.sh` |

## Interfaces / Contracts

```go
// cmd/regixtry/main.go — same seam shape as newBootstrapRunner (main.go:52)
type composeRunner interface {
	Preflight(ctx context.Context) error
	WriteProject(compose.ProjectConfig) (compose.Project, error)
	StartDatabase(ctx context.Context, p compose.Project) error   // no-op when external
	BootstrapAdmin(ctx context.Context, p compose.Project, username, password string) error
	StartRegistry(ctx context.Context, p compose.Project) error
	WaitReachable(ctx context.Context, p compose.Project) error
	SaveProvenance(p compose.Project) error
	Down(ctx context.Context, p compose.Project) error
}
```

Compose interpolation carries mandatory, self-describing errors so a clean checkout fails actionably instead of running on a guessed credential:

```yaml
POSTGRES_PASSWORD: ${REGIXTRY_POSTGRES_PASSWORD:?set it in .env (regixtry setup --mode docker generates one; manually: cp .env.example .env)}
```

`REGIXTRY_POSTGRES_PASSWORD` is generated and written **in both modes**; under external DSN the bundled service is never started, so the value stays inert and is not printed.

Preflight maps three distinct causes to three distinct messages, none of them a panic or a raw exec error: `docker` not on `PATH` → *"docker is not installed or not on PATH"*; `docker compose version` fails while `docker version` succeeds → *"Docker Compose v2 is required (the `docker compose` plugin was not found)"*; `docker version` fails → *"the Docker daemon is not reachable; ensure it is running and your user can access it"*. A compose project of that name with existing containers is refused rather than adopted.

Provenance (`regixtry-compose-state.json`, `0600`): `mode: "docker"`, compose project name, project directory, compose file path, env file path, pinned image, service and volume names, `bundled_postgres`, public URL — enough for a later `uninstall` to run `docker compose down --volumes` without re-deriving anything. No secrets.

## Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | `resolveSetupMode` accepts `docker`/`3`, rejects unknown, non-TTY error names all three modes | Table-driven, `go test ./...` |
| Unit | `runSetup` docker orchestration **order** (preflight → env → db → bootstrap → registry → reachable → provenance), and rollback on each failure point | Fake `composeRunner` recording an ordered call log |
| Unit | Preflight cause→message mapping; env-file rendering golden; `0600` modes; password absent from compose bytes, argv, and provenance | Fake `exec` runner + golden text |
| Unit | Embedded asset equals repo-root `docker-compose.yml` byte-for-byte | Drift test reading `../../../../docker-compose.yml` |
| Integration | Real stack from clean state: bundled and external scenarios | `setup-docker-smoke.sh`, RUN_ID-scoped project name, `down -v` trap |
| E2E | Auth actually gates and grants; TUI reachable | Anonymous `/v2/` → 401 + `WWW-Authenticate`; `/auth/token` Basic → Bearer `/v2/` → 200; `docker exec … regixtry tui -snapshot` → exit 0 |

`setup-docker-smoke.sh --external-postgres` additionally asserts the bundled `postgres` service is **not** running in the project. Every assertion is written RED and proven able to fail before implementation — the discipline `container-release-smoke.sh` established. The password assertions are deliberately three: present in the env file, absent from the compose file, and printed to stdout exactly once.

## Threat Matrix

| Boundary | Applicability | Design response | Planned RED tests |
|---|---|---|---|
| Documentation-like paths | N/A — nothing classifies files for execution | None | None |
| Git repository selection | N/A — no repo/cwd selection | None | None |
| Commit state | N/A — no commit automation | None | None |
| Push state | N/A — no ref/push automation | None | None |
| PR commands | N/A — no PR/VCS command composition | None | None |

The canonical five do not cover this change's real boundary, so these rows are added rather than manufactured:

| Boundary | Applicability | Design response | Planned RED tests |
|---|---|---|---|
| Subprocess argv composition | Applicable — `docker compose` invoked with operator-supplied DSN, project name, image, public URL | `exec.CommandContext` argv slices only; never `sh -c`, never string interpolation into a shell | DSN/project name containing `;`, `$(…)`, spaces, and a leading `-` reaches `docker` as one literal argument |
| Secret channel | Applicable — bundled password and admin password | Admin password via stdin into `-password-stdin`; bundled password only to a `0600` file and one stdout print | Assert neither secret appears in argv, env, compose bytes, or provenance |
| Missing/!broken tool detection | Applicable — Docker or Compose absent, old, or daemon down | Three distinct truthful errors; nothing written before preflight passes | One test per cause; assert no files created on failure |
| Filesystem target selection | Applicable — project dir derived from `-state-path` | Paths joined with `filepath.Join`, existing project refused not adopted | Refusal test on a pre-existing project |

## Migration / Rollout

No data migration. `docker-compose.yml` changes meaning for existing dev users: the external `dtf-netwok` network and the `registry:registry` literal are gone, and the file now pulls instead of builds. That is a documented behavior change with a README migration note plus `.env.example`, not a silent rewrite; `docker build .` still targets the `dev` stage, so the local-build path survives via `docker compose build`.

**Rollback.** The `docker` case is additive — deleting it restores the two-option chooser with `binary-only`/`daemon-sqlite` untouched, because nothing in `installlinux` changed. `docker-compose.yml` reverts through git history. A stack created by this mode is destroyed with `docker compose --project-name <p> down --volumes` plus removal of the compose directory; no host state exists outside the state dir, and no systemd unit is ever created.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Docker-mode provenance reaches today's `uninstall` and triggers systemd teardown | Med | Separate `regixtry-compose-state.json` filename; `uninstall` reads `regixtry-lifecycle-state.json` and never sees it |
| Repo-root and embedded compose files drift | Med | Byte-equality drift test in CI, same rule the `Dockerfile` stages already follow |
| Generated password lost by the operator | Med | Persisted `0600` **and** printed once; the print is the mitigation, not a leak |
| `up --no-deps regixtry` still parses the whole file, so mandatory interpolation fails in external mode | Med | The password is generated and written in both modes; the external path is covered by its own smoke scenario |
| Bundled Postgres mistaken for production-grade | Med | Same honesty framing the other modes use, stated in README and in the success message |
| Smoke script needs a real daemon, so CI cost grows | Low | Not wired into `release.yml` in this change; run locally and on demand, like the `--auth-postgres` scenario was introduced |

## Open Questions

- [ ] `uninstall`/`upgrade` support for `docker` mode is **out of scope here**. The provenance file is designed to make it possible later (project name, paths, volumes, image all recorded), but until it lands, teardown is the documented `docker compose down --volumes`. This is a forward-compatibility constraint, not a delivered capability.
- [ ] TLS: `docker` mode is local-HTTP only for now; `-runtime-tls-mode` is not honored. Reverse-proxy in front of the published port is the documented answer — decide whether a later slice threads TLS through compose.
- [ ] Should `setup --mode docker` refuse to run as root-with-docker-group, or accept both? `install.sh` suggests `sudo` for state-dir writes, which may conflict with a rootless daemon.
- [ ] Whether `.env.example` or a generated `.env` should be the documented manual path for non-`setup` compose users.

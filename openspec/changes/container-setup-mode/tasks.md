# Tasks: Container Setup Mode

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines (total) | 1400-2050 |
| 400-line budget risk (skill default, per-PR) | Medium for PR #1/#4; Low-Medium for PR #2/#3/#5 |
| 800-line budget risk (session budget, per-PR) | Low for every PR when split as below; High for the whole change as a single PR |
| Chained PRs recommended | Yes |
| Suggested split | PR #1 (base) → PR #2 → PR #3 → PR #4 → PR #5, Feature Branch Chain |
| Delivery strategy | single-pr (received) — resolves to a required `size:exception` decision unless chaining is accepted |
| Chain strategy | feature-branch-chain (proposed) |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: feature-branch-chain
400-line budget risk: Medium

### Why this estimate is High, stated up front (learned from `registry-container-mode`)

That prior change's first estimate for one phase (450-580) proved optimistic once a
security-sensitive scenario (`run_auth_scenario`) was fully scoped, forcing a
mid-flight re-estimate to 580-780 and a 3-way PR split discovered only during
`sdd-apply`. This change has comparably dense pieces from the start — a brand-new
Go package (`internal/infra/install/compose`) with an 8-method interface doing real
subprocess orchestration, credential generation/disclosure code, a rewritten
`docker-compose.yml`, and a new smoke script analogous to
`container-release-smoke.sh` (itself ~250-430 lines). Summing every file in the
design's `## File Changes` table honestly, before any implementation surprises,
already lands at 1400-2050 lines — far over both the skill's 400-line default and
this session's 800-line budget as a single PR. Splitting is proposed now, not
discovered later.

### Chain Overview

```text
develop
 └── feature/container-setup-mode                          ← tracker (draft, no direct merge until chain completes)
      ↑ PR #1 base: feature/container-setup-mode
      └── feature/container-setup-mode-01-compose-foundation
           ↑ PR #2 base: feature/container-setup-mode-01-compose-foundation
           └── feature/container-setup-mode-02-compose-credentials
                ↑ PR #3 base: feature/container-setup-mode-02-compose-credentials
                └── feature/container-setup-mode-03-compose-lifecycle
                     ↑ PR #4 base: feature/container-setup-mode-03-compose-lifecycle
                     └── feature/container-setup-mode-04-setup-wiring
                          ↑ PR #5 base: feature/container-setup-mode-04-setup-wiring
                          └── feature/container-setup-mode-05-docs-smoke
```

Only `feature/container-setup-mode` merges into `develop`, and only after PR #5 is
reviewed and integrated.

### Suggested Work Units (per PR)

| PR | Branch | Base | Goal | Est. lines | Focused test command | Runtime harness | Rollback boundary |
|----|--------|------|------|-----------|----------------------|-----------------|-------------------|
| #1 | `feature/container-setup-mode-01-compose-foundation` | `feature/container-setup-mode` | New `compose` package types + `Preflight` + `WriteProject`; embedded asset + drift-locked repo-root `docker-compose.yml` rewrite + `.env.example` | 395-545 | `go test ./internal/infra/install/compose/...` | N/A — no Docker daemon needed; fake-exec unit tests only | Revert this branch entirely; package is unreferenced by `main.go` until PR #4, so nothing else breaks |
| #2 | `feature/container-setup-mode-02-compose-credentials` | PR #1 branch | `StartDatabase` + `BootstrapAdmin` (bundled Postgres bring-up, stdin-only admin credential, DSN argv safety) — the security-sensitive cluster | 230-330 | `go test ./internal/infra/install/compose/...` | N/A — fake-exec unit tests; no daemon | Revert `StartDatabase`/`BootstrapAdmin`; `Preflight`/`WriteProject` from PR #1 keep working unmodified |
| #3 | `feature/container-setup-mode-03-compose-lifecycle` | PR #2 branch | `StartRegistry` + `WaitReachable` + `SaveProvenance` + `Down`; `regixtry-compose-state.json` shape | 150-250 | `go test ./internal/infra/install/compose/...` | N/A — fake-exec unit tests; no daemon | Revert these four methods; earlier compose methods keep working unmodified |
| #4 | `feature/container-setup-mode-04-setup-wiring` | PR #3 branch | `resolveSetupMode`/`runSetup` `"docker"` case, `promptSetupDockerConfig`, `composeRunner` interface + `newComposeRunner` seam, orchestration-order/rollback tests | 350-540 | `go test ./cmd/regixtry/...` | N/A — orchestration-order test uses a fake `composeRunner`, no daemon | Revert `main.go` docker-mode additions; `binary-only`/`daemon-sqlite` cases untouched |
| #5 | `feature/container-setup-mode-05-docs-smoke` | PR #4 branch | `install.sh` guidance line, README docker path + TUI + migration note, `setup-docker-smoke.sh` (bundled + external-DSN scenarios) | 308-535 | `go test ./...` (no Go changes here, sanity only) | `bash docs/verification/scripts/setup-docker-smoke.sh` and `... --external-postgres`, against a locally built or published image | Revert `setup-docker-smoke.sh` (new file), `install.sh` line, README sections; PR #1-#4 code paths keep working unmodified |

Sum: 1433-2200 lines depending on implementation friction, consistent with the
1400-2050 whole-change estimate. PR #4 carries the widest `main.go` blast radius
(mirrors two existing setup modes' structure) and PR #2 carries the most
security-sensitive surface (credential generation, stdin-only admin password,
subprocess argv composition with operator-supplied DSN) — same shape of caution
`registry-container-mode`'s PR #3 needed for `run_auth_scenario`.

---

## PR #1 (base) — targets `feature/container-setup-mode`

**Branch**: `feature/container-setup-mode-01-compose-foundation`
**Scope**: New `internal/infra/install/compose` package skeleton (types, `Preflight`,
`WriteProject`), embedded compose asset, drift-locked repo-root rewrite, `.env.example`.
**Chain note**: The package is not yet referenced from `cmd/regixtry/main.go` — that
wiring lands in PR #4. This mirrors how `registry-container-mode` PR #1 shipped the
healthcheck subcommand before the smoke script that exercised it existed.

### Phase 1: Compose Package Types & Preflight (Strict TDD; threat: missing/broken tool detection)

- [x] 1.1 RED: `internal/infra/install/compose/compose_test.go` — table-driven tests for `Preflight(ctx)` covering `docker` absent from `PATH`, `docker compose version` failing while `docker version` succeeds, and `docker version` failing (daemon unreachable); assert the three distinct truthful messages from design and that nothing is written to disk on any failure.
- [x] 1.2 GREEN: define `ProjectConfig`/`Project` types and implement `Provisioner.Preflight` in `internal/infra/install/compose/compose.go`, using an injectable exec-runner seam (fake in tests, `exec.CommandContext` argv slices in production — never `sh -c`).
- [x] 1.3 REFACTOR: extract the fake exec-runner into a small test helper reusable by PR #2/#3; `go vet ./...` and `gofmt -w .`.

### Phase 2: Compose Artifacts, Env File & WriteProject (Strict TDD; threat: filesystem target selection, secret channel)

- [x] 2.1 Create `internal/infra/install/compose/assets/docker-compose.yml`: pulls `${REGIXTRY_IMAGE}`, no `build:`, no `networks:`/`external: true`, mandatory `${REGIXTRY_POSTGRES_PASSWORD:?...}` interpolation, `depends_on: condition: service_healthy`, named volumes and healthchecks for both services. **Deviation**: healthcheck is declared only for `postgres` (required for `depends_on: condition: service_healthy`); no compose-level healthcheck is declared for `regixtry` because no `regixtry healthcheck`-equivalent tool exists in the pulled image's minimal `debian:bookworm-slim` runtime on this branch (that hardening landed only on the separate `registry-container-mode` branch, not yet in `develop`), and editing the local Dockerfile is out of this PR's File Changes scope. `WaitReachable` (PR #3) polls the public URL over HTTP directly, so this has no functional gap for setup orchestration.
- [x] 2.2 Rewrite repo-root `docker-compose.yml` as a byte-identical copy of the embedded asset (removes the `dtf-netwok` external-network requirement and the hardcoded `POSTGRES_PASSWORD: registry` literal).
- [x] 2.3 RED: drift test reading `../../../../docker-compose.yml` and asserting byte-equality with the embedded asset, same rule the Dockerfile stages already follow.
- [ ] 2.4 Create `.env.example` documenting `REGIXTRY_POSTGRES_PASSWORD`, `REGIXTRY_AUTH_POSTGRES_DSN`, `REGIXTRY_IMAGE`, `REGIXTRY_PORT`. **BLOCKED**: the sandbox's dotenv-pattern write protection hard-denies any write to a path matching `.env*` at the repo root, for every tool (Write and Bash redirection alike), regardless of content — confirmed via a bare `printf 'X=1\n' > .env.example` probe, denied identically to the real content. The intended content is recorded verbatim in `apply-progress` (`sdd/container-setup-mode/apply-progress`, and `openspec/changes/container-setup-mode/apply-progress.md`) for a human, or a session with relaxed dotenv protection, to create in one command.
- [x] 2.5 RED: test asserting `WriteProject` refuses a pre-existing project at the target path rather than adopting it, and that project/env-file paths are built only via `filepath.Join` (never string concatenation) — DSN/project-name strings containing `;`, `$(…)`, spaces, or a leading `-` must reach `docker` as one literal argument, not shell-interpreted.
- [x] 2.6 RED: test asserting the generated `crypto/rand` bundled password is absent from the embedded/repo-root compose bytes and from the env file's own key names, and that the env file is written with mode `0600`.
- [x] 2.7 GREEN: implement `Provisioner.WriteProject(compose.ProjectConfig) (compose.Project, error)` — generate the password, render the `0600` env file, copy the embedded compose asset, refuse an existing project.
- [x] 2.8 REFACTOR: extract env-file rendering into a small golden-text helper reused by PR #2's secret-channel tests.

### Phase 3: PR #1 Verification

- [x] 3.1 Run `go test ./internal/infra/install/compose/...` — confirm `Preflight`, `WriteProject`, and the drift test all pass.
- [x] 3.2 Run `go vet ./...` and `gofmt -l .` — confirm no diagnostics.

---

## PR #2 — targets PR #1 branch (`feature/container-setup-mode-01-compose-foundation`)

**Branch**: `feature/container-setup-mode-02-compose-credentials`
**Scope**: `StartDatabase` (bundled Postgres bring-up + readiness poll) and
`BootstrapAdmin` (one-shot admin creation) — the most credential-sensitive cluster
in this change, split out deliberately rather than folded into a larger PR, learning
from how `run_auth_scenario` alone justified its own PR in `registry-container-mode`.

### Phase 4: Bundled Database & Bootstrap Admin (Strict TDD; threat: secret channel, subprocess argv composition)

- [ ] 4.1 RED: test asserting `StartDatabase(ctx, p)` is a no-op when `p` is external (no `docker compose up` call recorded on the fake exec-runner) and issues `docker compose up -d postgres` only when bundled.
- [ ] 4.2 RED: test for the bounded `docker compose exec -T postgres pg_isready -U registry -d regixtry_auth` poll — asserts a bounded retry count/timeout, not an unbounded loop.
- [ ] 4.3 GREEN: implement `Provisioner.StartDatabase(ctx context.Context, p compose.Project) error`.
- [ ] 4.4 RED: test asserting `BootstrapAdmin` pipes the admin password via stdin into `docker compose run --rm --no-deps -T regixtry bootstrap-admin -password-stdin`, never through argv or an environment variable.
- [ ] 4.5 RED: test asserting a DSN or username containing `;`, `$(…)`, spaces, or a leading `-` reaches `docker` as one literal argument (argv-slice composition, never `sh -c`).
- [ ] 4.6 GREEN: implement `Provisioner.BootstrapAdmin(ctx context.Context, p compose.Project, username, password string) error`.
- [ ] 4.7 REFACTOR: `go vet ./...` and `gofmt -w .`; confirm the stdin-writer path has no leftover buffered copy of the password after the call returns.

### Phase 5: PR #2 Verification

- [ ] 5.1 Run `go test ./internal/infra/install/compose/...` — confirm `StartDatabase`/`BootstrapAdmin` suites pass, including every secret-channel and argv-safety assertion.

---

## PR #3 — targets PR #2 branch (`feature/container-setup-mode-02-compose-credentials`)

**Branch**: `feature/container-setup-mode-03-compose-lifecycle`
**Scope**: `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — the remaining
`composeRunner` methods, plus the `regixtry-compose-state.json` provenance shape.

### Phase 6: Registry Start, Reachability, Provenance & Teardown (Strict TDD)

- [ ] 6.1 GREEN: implement `Provisioner.StartRegistry(ctx context.Context, p compose.Project) error` — `docker compose up -d` when bundled, `docker compose up -d --no-deps regixtry` when external.
- [ ] 6.2 RED: test for `WaitReachable` polling `GET <public-url>/v2/` for `200` or `401` (same semantics as `regixtry healthcheck`), bounded, and failing with an installation-failure error (not a healthy-stack claim) when the poll never succeeds.
- [ ] 6.3 GREEN: implement `Provisioner.WaitReachable(ctx context.Context, p compose.Project) error`.
- [ ] 6.4 RED: test asserting `regixtry-compose-state.json` (`0600`) contains `mode: "docker"`, compose project name/dir/file path, env file path, pinned image, service/volume names, `bundled_postgres`, public URL — and contains no secret.
- [ ] 6.5 GREEN: implement `Provisioner.SaveProvenance(p compose.Project) error`, writing the file distinct from `regixtry-lifecycle-state.json` so today's `uninstall` never mistakes it for a systemd install.
- [ ] 6.6 RED: test asserting `Down(ctx, p)` runs `docker compose --project-name <p> down --volumes` and removes the generated project directory/env file, matching `rollbackSetupFailure`'s compose analogue.
- [ ] 6.7 GREEN: implement `Provisioner.Down(ctx context.Context, p compose.Project) error`.
- [ ] 6.8 REFACTOR: `go vet ./...` and `gofmt -w .`; confirm the full `composeRunner`-shaped method set on `Provisioner` compiles against the interface literal from design (interface itself is declared in PR #4's `main.go`).

### Phase 7: PR #3 Verification

- [ ] 7.1 Run `go test ./internal/infra/install/compose/...` — confirm the full package suite passes end to end against fakes.

---

## PR #4 — targets PR #3 branch (`feature/container-setup-mode-03-compose-lifecycle`)

**Branch**: `feature/container-setup-mode-04-setup-wiring`
**Scope**: Wire the `compose` package into `cmd/regixtry/main.go` as the third setup
mode — `resolveSetupMode`, `runSetup`, `promptSetupDockerConfig`, the `composeRunner`
interface, and the `newComposeRunner` seam.

### Phase 8: Setup Mode Wiring (Strict TDD)

- [ ] 8.1 RED: table-driven tests in `cmd/regixtry/main_test.go` for `resolveSetupMode` — accepts `"docker"`/`"3"`, rejects unknown modes, non-TTY error text names all three modes (`binary-only`, `daemon-sqlite`, `docker`).
- [ ] 8.2 GREEN: add the `"docker"` literal branch and `3) docker` menu entry to `resolveSetupMode` (`main.go:1785`).
- [ ] 8.3 Declare the `composeRunner` interface (`Preflight`, `WriteProject`, `StartDatabase`, `BootstrapAdmin`, `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down`) and `var newComposeRunner` seam in `main.go`, mirroring `newBootstrapRunner` (`main.go:52`).
- [ ] 8.4 RED: test for `promptSetupDockerConfig` — bundled-vs-external prompt appears only when `-auth-postgres-dsn` is absent and the session is interactive; absent + non-interactive defaults to bundled (documented divergence from `daemon-sqlite`'s anonymous default).
- [ ] 8.5 GREEN: implement `promptSetupDockerConfig(reader *bufio.Reader, stdout io.Writer, cfg setupConfig, promptState setupPromptState, selectedInteractively bool) (setupConfig, error)`, mirroring `promptSetupDaemonConfig` (`main.go:1335`).
- [ ] 8.6 RED: orchestration-order test for `runSetup`'s `case "docker"` using a fake `composeRunner` recording an ordered call log — asserts `Preflight → WriteProject → StartDatabase → BootstrapAdmin → StartRegistry → WaitReachable → SaveProvenance`, matching the design's data-flow ordering (admin created before the registry starts, since `serve` refuses to start with auth on and no admin).
- [ ] 8.7 RED: rollback tests — a failure at each stage after `WriteProject` triggers `Down` plus generated-file removal, mirroring `rollbackSetupFailure` (`main.go:2073`).
- [ ] 8.8 GREEN: implement `case "docker"` in `runSetup` (`main.go:1268`), calling `promptSetupDockerConfig` when interactive, `validateSetupAuthConfig`-equivalent DSN handling, and the ordered `composeRunner` calls with rollback on failure.
- [ ] 8.9 GREEN: on success, print reachability, the env-file path, the generated password once, and `docker exec -it <container> regixtry tui` guidance to stdout.
- [ ] 8.10 REFACTOR: `go vet ./...` and `gofmt -w .`; confirm `binary-only`/`daemon-sqlite` cases are byte-for-byte unchanged in the diff.

### Phase 9: PR #4 Verification

- [ ] 9.1 Run `go test ./cmd/regixtry/...` — confirm `resolveSetupMode`, `promptSetupDockerConfig`, orchestration-order, and rollback suites all pass.
- [ ] 9.2 Run `go test ./...` — confirm `binary-only`/`daemon-sqlite` test suites are unaffected.

---

## PR #5 — targets PR #4 branch (`feature/container-setup-mode-04-setup-wiring`)

**Branch**: `feature/container-setup-mode-05-docs-smoke`
**Scope**: `install.sh` guidance, README documentation, and the setup-mode-docker
smoke script proving the end-to-end flow (bundled and external-DSN paths).

### Phase 10: Install Guidance & README

- [ ] 10.1 Add one guidance line after `install.sh:332`: `sudo ${privileged_command} setup --mode docker --public-url http://127.0.0.1:5000`, alongside the existing `binary-only`/`daemon-sqlite` lines.
- [ ] 10.2 Add `### One command: regixtry setup --mode docker` as the first subsection of `## Run as a container` in `README.md`, above the manual `docker run`/`docker compose` recipes from `registry-container-mode`.
- [ ] 10.3 Add `### The TUI against a container` documenting `docker exec -it <container> regixtry tui -storage-root /var/lib/regixtry` as the supported path, explicitly stating no host-side TUI access to a container's data is supported.
- [ ] 10.4 Rewrite `README.md:77` ("`install.sh` and `regixtry setup` do not offer a container-selection branch") — this sentence becomes false with this change and MUST be corrected, not left stale (the same class of gap this session already learned to watch for).
- [ ] 10.5 Drop the `docker network create dtf-netwok` manual step at `README.md:39`; add a migration note for existing dev users of the old `docker-compose.yml` (external network + hardcoded credential removed, image now pulled instead of built for the `release` target, `docker build .` still targets `dev` unchanged).

### Phase 11: Setup-Mode-Docker Smoke Script (bundled + external-DSN scenarios)

- [ ] 11.1 Create `docs/verification/scripts/setup-docker-smoke.sh`, sibling of `container-release-smoke.sh`, reusing its `fail()`/`cleanup()` trap/`wait_healthy()`/`http_status()` shape; RUN_ID-scoped project name; every cleanup step `|| true`.
- [ ] 11.2 Implement the bundled-Postgres scenario from a clean checkout: run `regixtry setup --mode docker` (or the equivalent compose-package call path) with no DSN, assert the stack becomes healthy, assert the generated password is present in the `0600` env file, absent from `docker-compose.yml`, and printed to stdout exactly once.
- [ ] 11.3 Implement the `--external-postgres` scenario: start a standalone `postgres:17-alpine`, pass its DSN via `-auth-postgres-dsn`, assert the bundled `postgres` service is **not** running in the project, and assert auth still succeeds against the external instance.
- [ ] 11.4 Assert anonymous `GET /v2/` → `401` + `WWW-Authenticate`, authenticated Basic→Bearer→blob round trip succeeds, and `docker exec … regixtry tui -snapshot` exits `0`, for both scenarios.
- [ ] 11.5 RED-path-prove every load-bearing assertion by deliberately breaking each one first (password-in-compose check, external-skip check, reachability check), same discipline `container-release-smoke.sh` established.

### Phase 12: PR #5 Verification & Explicit No-CI-Wiring Note

- [ ] 12.1 Run `setup-docker-smoke.sh` locally (bundled scenario) against a local `docker build --target=release .` or the published GHCR image, from a clean checkout.
- [ ] 12.2 Run `setup-docker-smoke.sh --external-postgres` locally against the same image, from a clean checkout.
- [ ] 12.3 Explicit CI-surface note (do not leave this implicit — the prior change's CI-wiring gap must not repeat): per design's Risk table, `setup-docker-smoke.sh` is **deliberately not wired into `.github/workflows/release.yml` in this change** ("Smoke script needs a real daemon, so CI cost grows... Not wired into `release.yml` in this change; run locally and on demand, like the `--auth-postgres` scenario was introduced"). Local/manual execution of Phase 12.1/12.2 is the sole verification gate for `docker` setup mode until a future change adds CI wiring. State this explicitly in the PR description so reviewers do not assume CI coverage exists.

---

## Chain Status

Five PRs (Phase totals: 3+8+2 / 7+1 / 8+1 / 10+2 / 5+5+3 = 55 tasks total) are planned but not yet applied. `feature/container-setup-mode` (tracker branch) does not exist yet; `sdd-apply` creates each stacked branch in order. No branch merges happen until PR #5 is reviewed and the tracker merges to `develop`.

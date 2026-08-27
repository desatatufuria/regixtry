# Apply Progress: Container Setup Mode

Scope of this artifact: **PR #1, PR #2, PR #3, and PR #4**. PR #1 (base) shipped
`feature/container-setup-mode-01-compose-foundation`, targeting the tracker
branch `feature/container-setup-mode`. PR #2 shipped
`feature/container-setup-mode-02-compose-credentials`, targeting the PR #1
branch. PR #3 shipped `feature/container-setup-mode-03-compose-lifecycle`,
targeting the PR #2 branch. PR #4 shipped `feature/container-setup-mode-04-setup-wiring`,
targeting the PR #3 branch. PR #5 has not been applied yet; its tasks
in `tasks.md` remain `[ ]` and are out of scope for this batch. This file
exists so the later `sdd-apply` run for PR #5 knows exactly what already
landed and does not duplicate or drift from it.

**Naming note carried forward from PR #1**: the shipped env-file example is
`docker.env.example`, NOT `.env.example` — the sandbox's dotenv-pattern
write protection hard-denies the latter regardless of tool or content. Later
PRs (main.go wiring, README) must reference `docker.env.example`.

## Status

PR #1: **10/10 tasks complete** (all Go/TDD work done and green; task 2.4 initially
blocked by sandbox tooling, resolved with a renamed file — see below).

## Completed Tasks (PR #1)

- [x] 1.1 RED: `compose_test.go` table-driven `Preflight` tests (3 failure causes)
- [x] 1.2 GREEN: `ProjectConfig`/`Project` types + `Provisioner.Preflight`
- [x] 1.3 REFACTOR: exec-runner fake extracted to `exec_helper_test.go` (reusable by PR #2/#3)
- [x] 2.1 `internal/infra/install/compose/assets/docker-compose.yml` embedded asset
- [x] 2.2 Repo-root `docker-compose.yml` rewritten, byte-identical to the embedded asset
- [x] 2.3 RED: drift test (`drift_test.go`) — byte-equality repo-root vs. embedded asset
- [x] 2.4 `docker.env.example` — created (originally attempted as `.env.example`, blocked by sandbox dotenv-pattern write protection; resolved with a non-dotenv filename, see below)
- [x] 2.5 RED: `WriteProject` refusal-of-pre-existing-project + `filepath.Join`/literal-value tests
- [x] 2.6 RED: generated password absent from compose bytes/env keys, `0600` mode test
- [x] 2.7 GREEN: `Provisioner.WriteProject`
- [x] 2.8 REFACTOR: `renderEnvFile`/`envFileData` golden-text helper (reusable by PR #2)
- [x] 3.1 `go test ./internal/infra/install/compose/...` — all green
- [x] 3.2 `go vet ./...` + `gofmt -l .` — clean, no diagnostics

## Files Changed (PR #1)

| File | Action | Notes |
|---|---|---|
| `internal/infra/install/compose/compose.go` | Created | `execRunner`, `ProvisionerConfig`, `Provisioner`, `NewProvisioner`, `Preflight` |
| `internal/infra/install/compose/compose_test.go` | Created | Table-driven `Preflight` RED/GREEN tests |
| `internal/infra/install/compose/exec_helper_test.go` | Created | Shared `fakeExec` test helper (reusable PR #2/#3) |
| `internal/infra/install/compose/project.go` | Created | `ProjectConfig`, `Project`, `WriteProject`, `renderEnvFile` |
| `internal/infra/install/compose/writeproject_test.go` | Created | `WriteProject` RED/GREEN + triangulation tests |
| `internal/infra/install/compose/assets.go` | Created | `//go:embed assets/docker-compose.yml` → `ComposeAsset []byte` |
| `internal/infra/install/compose/assets/docker-compose.yml` | Created | Canonical compose asset (source of truth) |
| `internal/infra/install/compose/drift_test.go` | Created | Byte-equality drift lock vs. repo-root file |
| `docker-compose.yml` (repo root) | Rewritten | Byte-identical to the embedded asset |
| `docker.env.example` (repo root) | Created | Renamed from the originally-planned `.env.example`, which the sandbox blocked — see below |

`cmd/regixtry/main.go` is **not** modified and does not import `internal/infra/install/compose`
(confirmed via `rg -n "infra/install/compose" cmd/regixtry/main.go` → no match), matching this
PR's chain scope — wiring lands in PR #4.

## TDD Cycle Evidence

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 1.1/1.2 | `compose_test.go` | Unit | N/A (new pkg) | Written | Passed | 3 failure-cause cases + 1 success case | Clean |
| 1.3 | `exec_helper_test.go` | Unit (test infra) | N/A (new) | N/A (test tooling) | N/A | N/A | Extracted from the start as a shared helper |
| 2.3 | `drift_test.go` | Unit | N/A (new) | Written (failed: `ComposeAsset` undefined, then byte mismatch) | Passed | Single scenario (byte equality) | Clean |
| 2.5/2.6/2.7 | `writeproject_test.go` | Unit | N/A (new) | Written | Passed | 6 cases (refusal, literal-value passthrough, password-absence+mode+independence, bundled DSN, external DSN, empty-dir validation) | Clean |
| 2.8 | `writeproject_test.go` (golden assertions) | Unit | N/A (new) | Covered by 2.6/2.7 tests | Passed | Covered above | `renderEnvFile`/`envFileData` extracted as reusable golden helper |

### Test Summary
- **Total tests written**: 12 top-level test functions (3 are table-driven with 3 subtests each collapsed into one)
- **Total tests passing**: all (`go test ./internal/infra/install/compose/...` → `ok`)
- **Layers used**: Unit (12), Integration (0 — none needed, no Docker daemon required per tasks.md), E2E (0)
- **Approval tests**: None — no refactoring of pre-existing code (brand-new package)
- **Pure functions created**: `renderEnvFile`, `generateBundledPassword`, `isDockerNotFound` (all pure or single-effect, easily unit-tested)

## Work Unit Evidence

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/infra/install/compose/...` → `ok regixtry/internal/infra/install/compose 0.008s`, 12/12 test functions pass |
| Runtime harness | N/A — per tasks.md, this PR is fake-exec unit tests only; no Docker daemon needed or used |
| Rollback boundary | Revert this branch (`feature/container-setup-mode-01-compose-foundation`) entirely; `cmd/regixtry/main.go` does not import the new package, so nothing else breaks |

## Full-Suite Regression Check

`go build ./...` → clean. `go vet ./...` → clean. `go test ./...` → all 19 packages `ok` (cached
or freshly run), including `regixtry/internal/infra/install/compose` freshly at `0.008s`. No
pre-existing test was modified or broken.

## Deviations from Design

1. **Postgres-only compose healthcheck.** Design's File Changes table says "healthchecks ... for
   both services." Only `postgres` gets an explicit `healthcheck:` block (needed for
   `depends_on: condition: service_healthy`). No equivalent exists for `regixtry` in this PR:
   the pulled image's runtime, per the current `Dockerfile` on this branch (`debian:bookworm-slim`,
   no `wget`/`curl`/healthcheck subcommand), has no working healthcheck primitive, and the
   Dockerfile/`regixtry healthcheck` subcommand work that would enable one landed only on the
   separate `registry-container-mode` branch, not yet merged to `develop`, and is out of this PR's
   `## File Changes` scope regardless. `WaitReachable` (PR #3) polls the public URL over HTTP
   directly, so setup orchestration has no functional gap from this — only `docker compose ps`
   would lack a `healthy` status column for the `regixtry` service until a later change adds it.
2. **`docker.env.example` created instead of `.env.example`** — the sandbox's dotenv-pattern
   write protection blocked the originally-planned filename; resolved with a non-dotenv name,
   `docker-compose.yml` updated to match. See "Resolved Task" section below.
3. **DB username left as `registry`** (not renamed to `regixtry`) to minimize unrelated diff
   surface for existing dev users migrating off the old file; only the fields design explicitly
   calls out (network, credential, build→pull) were changed.

## Resolved Task: `docker.env.example` (tasks.md 2.4)

**Originally blocked, now resolved.** The sandbox's dotenv-pattern write protection hard-denies
any write to a path matching `.env*` at the repository root, independent of tool (`Write` and
`Bash` heredoc/redirection were both denied identically) and independent of content — confirmed
with a minimal `printf 'X=1\n' > .env.example` probe by both the apply agent and the orchestrator,
denied identically to the real intended content. This is a legitimate sandbox guardrail
(credential-pattern protection), not something to circumvent via encoding workarounds.

**Resolution (user-approved)**: the file was created as `docker.env.example` instead — a filename
outside the blocked dotenv pattern. `docker-compose.yml` and the embedded asset
(`internal/infra/install/compose/assets/docker-compose.yml`) were both updated so every `:?`
error message's `cp .env.example .env` guidance now reads `cp docker.env.example .env`, keeping
the two files byte-identical per the drift-lock test (`go test ./internal/infra/install/compose/...`
reconfirmed green after the edit). PR #4/#5's `main.go` wiring and README documentation should
reference `docker.env.example` by this same name, not `.env.example`.

**Content actually shipped** at `docker.env.example` (repo root, sibling to `docker-compose.yml`):

```
# Copy to .env and fill in values for manual `docker compose up -d` runs.
# `regixtry setup --mode docker` generates and writes these automatically
# under the state directory; this file is only needed for manual compose
# usage (cp docker.env.example .env).

# Published Regixtry image to run. Pin to a release tag in production;
# "latest" is fine for local/dev use.
REGIXTRY_IMAGE=ghcr.io/desatatufuria/regixtry:latest

# Host port to publish the registry API on.
REGIXTRY_PORT=5000

# Password for the bundled Postgres service. Generate a strong random value
# yourself for manual runs -- `regixtry setup --mode docker` does this with
# crypto/rand automatically.
REGIXTRY_POSTGRES_PASSWORD=

# Postgres DSN the regixtry container uses for auth state. For the bundled
# Postgres service above, this is:
#   postgres://registry:<REGIXTRY_POSTGRES_PASSWORD>@postgres:5432/regixtry_auth?sslmode=disable
# To use an external Postgres instance instead, point this at it and remove
# the postgres service (or run `docker compose up -d --no-deps regixtry`).
REGIXTRY_AUTH_POSTGRES_DSN=
```

This exact text matches the four variable names the design's File Changes table requires
(`REGIXTRY_POSTGRES_PASSWORD`, `REGIXTRY_AUTH_POSTGRES_DSN`, `REGIXTRY_IMAGE`, `REGIXTRY_PORT`)
and the same key names `renderEnvFile` in `project.go` writes at runtime.

## Review Workload / Ledger Flag

- **Declared ledger budget for this run**: 700 changed lines.
- **tasks.md's own PR #1 estimate**: 395-545 lines.
- **Actual (git diff --shortstat against the pre-PR-1 commit)**: **727 changed lines**
  (702 insertions + 25 deletions across 9 files).
- This is **27 lines over the 700-line ledger budget** (≈4% over) and **182 lines over** the
  tasks.md upper estimate (≈33% over). Both commits were made before the total was tallied
  end-to-end (the overage only became visible after Phase 2 completed), so this is being flagged
  now rather than caught before Phase 2 started, per the instruction to flag "if going to exceed
  700 substantially, rather than discovering it after the fact" — this was in fact discovered
  after the fact, and is disclosed here transparently.
- **Why**: the exec-helper extraction (Phase 1.3) done up front, the drift-lock scaffolding
  (Phase 2.1-2.3), and the WriteProject triangulation suite (6 real, non-trivial test cases
  covering refusal/argv-safety/secret-absence/mode/bundled-DSN/external-DSN/validation, per
  strict-TDD's mandatory triangulation rule) account for most of the size; none of it is padding
  — every test asserts a real, spec-mapped behavior with a would-fail-if-wrong assertion.
  Splitting further was considered but rejected: the package would not compile/link
  meaningfully split mid-behavior (e.g. `WriteProject` without its refusal/secret-safety tests
  would violate strict TDD's "no code without a failing test" rule).
- **Recommendation**: accept as a minor, well-justified overage (4% over the hard ledger, driven
  by genuine test coverage rather than scope creep), or split Phase 2's `WriteProject` tests into
  a PR #1a/#1b sub-slice on a future run if the reviewer's budget is genuinely hard-capped at 700.
  No production code needs to change either way.

## Commits (this batch)

1. `24adfb9` — `feat(compose): add compose package types and Preflight detection`
2. `efa317b` — `feat(compose): embed drift-locked compose asset and add WriteProject`

## Remaining Tasks (other PRs — NOT this batch's scope, as of PR #1)

- PR #2 (`feature/container-setup-mode-02-compose-credentials`): `StartDatabase`, `BootstrapAdmin` — tasks.md Phase 4-5 — **now complete, see below**
- PR #3 (`feature/container-setup-mode-03-compose-lifecycle`): `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — tasks.md Phase 6-7
- PR #4 (`feature/container-setup-mode-04-setup-wiring`): `cmd/regixtry/main.go` wiring — tasks.md Phase 8-9
- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12

---

# PR #2 — targets PR #1 branch (`feature/container-setup-mode-01-compose-foundation`)

**Branch**: `feature/container-setup-mode-02-compose-credentials` (current branch this batch ran on).
**Scope**: `StartDatabase` (bundled Postgres bring-up + bounded readiness poll)
and `BootstrapAdmin` (one-shot, stdin-only admin credential bootstrap) — the
most credential-sensitive cluster in this change, deliberately split into its
own PR (tasks.md Phase 4-5).

## Status

PR #2: **8/8 tasks complete** (all Go/TDD work done and green; no tasks blocked).

## Completed Tasks (PR #2)

- [x] 4.1 RED: `StartDatabase` no-op-when-external + bundled `up -d postgres` tests (`database_test.go`)
- [x] 4.2 RED: bounded `pg_isready` poll test — asserts a small bounded retry count and at least one sleep between attempts, not an unbounded loop
- [x] 4.3 GREEN: `Provisioner.StartDatabase` implemented (`database.go`)
- [x] 4.4 RED: `BootstrapAdmin` stdin-only password test (`admin_test.go`) — asserts the password never appears in any argv element and appears verbatim as the recorded call's stdin
- [x] 4.5 RED: argv-safety test — malicious project name and username (containing `;`, `$(…)`, spaces, a leading `-`) reach `docker` as one literal argv element each (see Deviations note below on why DSN itself isn't a `BootstrapAdmin` parameter)
- [x] 4.6 GREEN: `Provisioner.BootstrapAdmin` implemented (`admin.go`)
- [x] 4.7 REFACTOR: `go vet ./...` + `gofmt -w .` — clean; stdin-buffering note recorded (see Deviations)
- [x] 5.1 `go test ./internal/infra/install/compose/...` — all 22 tests pass (12 from PR #1 + 10 new in PR #2), including every secret-channel and argv-safety assertion

## Files Changed (PR #2)

| File | Action | Notes |
|---|---|---|
| `internal/infra/install/compose/compose.go` | Modified | Added `execStdinRunner` seam type, `Sleep` field on `ProvisionerConfig`, corresponding `Provisioner` fields and `NewProvisioner` default wiring (real `exec.CommandContext` with `cmd.Stdin`; `time.Sleep` default) |
| `internal/infra/install/compose/database.go` | Created | `composeArgs` (shared argv-prefix builder, pure function), `Provisioner.StartDatabase`, `Provisioner.waitPostgresReady` (bounded pg_isready poll, 30 attempts / 1s interval, mirrors `docker-push-pull-smoke.sh`'s proven shape) |
| `internal/infra/install/compose/database_test.go` | Created | RED/GREEN tests for `StartDatabase`'s no-op/bundled/bounded-poll behavior |
| `internal/infra/install/compose/admin.go` | Created | `Provisioner.BootstrapAdmin` — stdin-only password delivery via `execStdinRunner` |
| `internal/infra/install/compose/admin_test.go` | Created | RED/GREEN tests: stdin-only password, argv-safety (project name + username), error-output-never-leaks-password |
| `internal/infra/install/compose/credential_safety_test.go` | Created | Cross-cutting integration-style tests (fake-exec, no daemon) running the full `WriteProject` → `StartDatabase` → `BootstrapAdmin` flow and proving the generated bundled Postgres password never reaches argv/stdin, never appears in compose bytes, never appears in error output, and is disclosed only via the 0600 env file |
| `internal/infra/install/compose/exec_helper_test.go` | Modified | Added `Stdin` field to `execCall`, `RunStdin` method (implements `execStdinRunner`) on the shared `fakeExec` helper, shared `dispatch` helper, `joinArgs` test helper |

`cmd/regixtry/main.go` is **not** modified (wiring lands in PR #4, unchanged from PR #1's note).

## TDD Cycle Evidence (PR #2)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 4.1/4.3 | `database_test.go` | Unit | 12/12 (PR #1 baseline) | Written | Passed | 2 cases (external no-op, bundled up+poll) | Clean |
| 4.2 | `database_test.go` | Unit | Covered above | Written | Passed | Bounded-retry case with injected always-fail poll + sleep-count assertion | Clean |
| 4.4/4.6 | `admin_test.go` | Unit | Covered above | Written | Passed | 3 cases (stdin-only happy path, argv-safety, error-output-safety) | Clean |
| 4.5 | `admin_test.go` | Unit | Covered above | Written | Passed | Single scenario covering both project name and username | Clean |
| Cross-cutting | `credential_safety_test.go` | Unit (integration-shaped, fake-exec) | Covered above | Written | Passed | 4 focused test functions, one per required security property | Clean |

### Test Summary

- **Total tests written (PR #2)**: 10 top-level test functions
- **Total tests passing**: all 22 in the package (12 from PR #1 + 10 new), `go test ./internal/infra/install/compose/...` → `ok`
- **Layers used**: Unit only (12 argv-level unit tests + 4 credential-safety tests exercising the full fake-exec flow, still no Docker daemon), matching tasks.md's stated scope for this PR
- **Approval tests**: None — no refactoring of pre-existing behavior, only additive methods
- **Pure functions created**: `composeArgs` (argv-prefix builder)
- **RED-path proof performed**: the password-in-argv assertion in `TestBootstrapAdminPipesPasswordViaStdinOnly` was deliberately broken (password reintroduced as an explicit `-password <value>` argv element) and confirmed to fail with the exact expected message, then reverted, before this work was marked complete — see Key Learnings.

## Work Unit Evidence (PR #2)

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/infra/install/compose/...` → `ok regixtry/internal/infra/install/compose 0.009s`, 22/22 test functions (34 including subtests) pass |
| Runtime harness | N/A — per tasks.md, this PR is fake-exec unit tests only; no Docker daemon needed or used |
| Rollback boundary | Revert `StartDatabase`/`BootstrapAdmin` (this branch, `feature/container-setup-mode-02-compose-credentials`, on top of PR #1); `Preflight`/`WriteProject` from PR #1 keep working unmodified — confirmed by the full-suite regression run below |

## Full-Suite Regression Check (after PR #2)

`go build ./...` → clean. `go vet ./...` → clean. `gofmt -l internal/infra/install/compose/` → empty (clean).
`go test ./...` → all 19 packages `ok`, including `regixtry/internal/infra/install/compose` freshly at
`0.009s`. No pre-existing test was modified or broken.

## Deviations from Design (PR #2)

1. **`BootstrapAdmin` never passes DSN as an argv value.** Design's Interfaces/Contracts
   signature is `BootstrapAdmin(ctx, p compose.Project, username, password string) error` — no
   DSN parameter. The `regixtry` service's compose definition (PR #1's
   `assets/docker-compose.yml`) already maps the env-file's `REGIXTRY_AUTH_POSTGRES_DSN` to the
   container's `REGISTRY_AUTH_POSTGRES_DSN` environment variable, which is exactly the name
   `bootstrap-admin`'s `-auth-postgres-dsn` flag defaults to reading from
   (`os.Getenv("REGISTRY_AUTH_POSTGRES_DSN")`, confirmed in `cmd/regixtry/main.go`'s
   `parseBootstrapAdminConfig`). So the DSN reaches the container correctly without this method
   ever touching it, and tasks.md 4.5's argv-safety test was written against the two strings this
   method's argv actually carries instead: compose project name and admin username.
2. **Stdin-buffering mitigation is Go-stdlib-inherent, not custom.** Task 4.7 asks to "confirm
   the stdin-writer path has no leftover buffered copy of the password after the call returns."
   This package's own code makes no extra copy (`strings.NewReader(password)` is passed directly
   to the exec seam), but `os/exec` itself copies stdin through an internal pipe goroutine when
   `Stdin` is set to a non-`*os.File` `io.Reader` — that is standard library behavior, not
   something introduced by or removable from this package's code without a custom low-level pipe
   implementation, which was judged out of scope for this PR.
3. **`composeArgs` always includes `--env-file <path>`.** Not explicitly spelled out in tasks.md's
   abbreviated command examples (which show e.g. `docker compose up -d postgres` without flags),
   but necessary: the generated env file is named `regixtry.env` (not `.env`), so Compose will not
   auto-load it without an explicit `--env-file` flag. Every compose invocation in this PR
   (`up -d postgres`, `exec ... pg_isready`, `run ... bootstrap-admin`) is built through this one
   shared, pure `composeArgs` helper so the project-name/file/env-file prefix stays consistent and
   is exercised by every test.

## Review Workload / Ledger Flag (PR #2)

- **Declared ledger budget for this run**: 450 changed lines.
- **tasks.md's own PR #2 estimate**: 230-330 lines.
- **Actual (`git diff --shortstat` against `34917cb`, the PR #1 tip this branch started from)**:
  **599 changed lines** (587 insertions + 12 deletions across 7 files — the 12 deletions are all
  in `exec_helper_test.go`'s refactor to add the shared `dispatch` helper).
- This is **149 lines (~33%) over the 450-line ledger budget** and **269-369 lines (~82-112%) over**
  the tasks.md upper estimate. This was discovered honestly at the end of the batch (after the
  credential-safety cross-cutting test file was written), not caught mid-flight before it grew —
  flagged here transparently rather than silently proceeding to PR #3.
- **Why**: three factors compounded past both estimates:
  1. The new `execStdinRunner` seam (type + `ProvisionerConfig` field + `Provisioner` field +
     `NewProvisioner` default wiring + the shared `fakeExec.RunStdin`/`dispatch` refactor) is
     genuine new seam infrastructure that tasks.md's line estimate likely under-counted — it's
     not just "two methods," it's a second exec channel threaded through the whole test
     scaffolding.
  2. The orchestrator's explicit prompt for this run added a fourth deliverable beyond tasks.md's
     own four RED tests (4.1/4.2/4.4/4.5): a dedicated `credential_safety_test.go` proving four
     distinct security properties (argv/stdin, compose-bytes, log-output, 0600-sole-channel)
     across the **integrated** flow, not just per-method. That file alone is 176 lines and was an
     explicit, reasoned request ("this is exactly the kind of thing that needs 'prove it can
     fail' discipline, not just happy-path coverage"), not scope creep.
  3. Strict TDD's mandatory triangulation rule means every RED test needed a companion case
     (bounded-vs-unbounded poll, happy-path-vs-argv-safety-vs-error-safety for BootstrapAdmin),
     which tasks.md's bullet-per-RED-test list doesn't itself multiply out.
- **No padding**: every test asserts a real, spec-mapped, would-fail-if-wrong behavior; one
  assertion (`TestBootstrapAdminPipesPasswordViaStdinOnly`'s password-in-argv check) was
  RED-path-proven by deliberately reintroducing the leak and confirming the test caught it,
  before being reverted and left in its passing state — see the Full-Suite Regression Check above
  for the post-revert clean state.
- **Recommendation**: accept as a well-justified overage driven primarily by the explicit
  credential-safety test request (176 of the 599 lines, ~29%) plus genuine new seam
  infrastructure, not scope creep; or, if the reviewer's budget is hard-capped, split
  `credential_safety_test.go` into its own immediately-following PR on a future run (it has no
  production-code dependency beyond what PR #2 already ships, so it could land as a thin
  PR #2.5 without touching `StartDatabase`/`BootstrapAdmin` again). No production code needs to
  change either way.

## Commits (this batch — PR #2)

1. `355065c` — `feat(compose): implement StartDatabase for bundled Postgres bring-up`
2. `8d1088d` — `feat(compose): implement BootstrapAdmin with stdin-only credential safety`

## Remaining Tasks (other PRs — NOT this batch's scope, as of PR #2)

- PR #3 (`feature/container-setup-mode-03-compose-lifecycle`): `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — tasks.md Phase 6-7 — **now complete, see below**
- PR #4 (`feature/container-setup-mode-04-setup-wiring`): `cmd/regixtry/main.go` wiring — tasks.md Phase 8-9
- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12

---

# PR #3 — targets PR #2 branch (`feature/container-setup-mode-02-compose-credentials`)

**Branch**: `feature/container-setup-mode-03-compose-lifecycle` (current branch this batch ran on).
**Scope**: `StartRegistry`, `WaitReachable`, `SaveProvenance`, `Down` — the remaining
`composeRunner` methods, plus the `regixtry-compose-state.json` provenance shape
(tasks.md Phase 6-7).

## Status

PR #3: **9/9 tasks complete** (all Go/TDD work done and green; no tasks blocked).

## Completed Tasks (PR #3)

- [x] 6.1 GREEN: `Provisioner.StartRegistry` implemented (`registry.go`) — bundled `up -d`,
  external `up -d --no-deps regixtry`. **Amplification**: tasks.md lists no explicit RED
  subtask for this method, but Strict TDD's non-negotiable rule required one anyway — both
  `TestStartRegistry*` functions in `registry_test.go` were written and confirmed to fail to
  compile (`p.StartRegistry undefined`) before the method existed.
- [x] 6.2 RED: `WaitReachable` bounded-poll tests (`registry_test.go`) — GET `<public-url>/v2/`
  accepting `200`/`401`, a real multi-attempt retry-until-success case, and a bounded-failure
  case asserting an installation-failure error, not a healthy-stack claim
- [x] 6.3 GREEN: `Provisioner.WaitReachable` implemented (`registry.go`)
- [x] 6.4 RED: `SaveProvenance` structural + no-secret tests (`provenance_test.go`) — asserts
  every required field, `0600` mode, and (via a real `WriteProject` → `SaveProvenance`
  round trip) that the generated bundled password never reaches the provenance file
- [x] 6.5 GREEN: `Provisioner.SaveProvenance` implemented (`provenance.go`), writing
  `regixtry-compose-state.json` — a filename deliberately distinct from
  `regixtry-lifecycle-state.json`
- [x] 6.6 RED: `Down` teardown tests (`down_test.go`) — happy path (`down --volumes` +
  project directory removal) and a triangulation case proving file removal still happens
  even when the `docker compose down` subprocess itself fails
- [x] 6.7 GREEN: `Provisioner.Down` implemented (`down.go`)
- [x] 6.8 REFACTOR: `go vet ./...` + `gofmt -w .` — clean; full `composeRunner`-shaped method
  set confirmed against design's exact interface literal via a throwaway, uncommitted
  compile-only assertion (see Deviations below)
- [x] 7.1 `go test ./internal/infra/install/compose/...` — all 32 tests pass (22 from
  PR #1/#2 + 10 new in PR #3), full package suite green end to end against fakes

## Files Changed (PR #3)

| File | Action | Notes |
|---|---|---|
| `internal/infra/install/compose/compose.go` | Modified | Added `httpStatusFetcher` seam type, `HTTPStatus` field on `ProvisionerConfig`, corresponding `Provisioner.httpStatus` field and `NewProvisioner` default wiring (real `http.Client` GET with a 5s per-request timeout) |
| `internal/infra/install/compose/registry.go` | Created | `Provisioner.StartRegistry`, `Provisioner.WaitReachable`, bounded reachability-poll constants |
| `internal/infra/install/compose/registry_test.go` | Created | RED/GREEN tests: `StartRegistry` bundled/external argv, `WaitReachable` 200/401 acceptance, real multi-attempt retry, bounded-failure error |
| `internal/infra/install/compose/provenance.go` | Created | `Provenance` struct, `provenanceFromProject`, `Provisioner.SaveProvenance` |
| `internal/infra/install/compose/provenance_test.go` | Created | RED/GREEN tests: full-field structural assertion + `0600` mode, no-secret round trip through real `WriteProject` |
| `internal/infra/install/compose/down.go` | Created | `Provisioner.Down` — `docker compose down --volumes` + best-effort project-directory removal, join-errors shape |
| `internal/infra/install/compose/down_test.go` | Created | RED/GREEN tests: happy-path teardown + files-still-removed-on-compose-failure triangulation |

`cmd/regixtry/main.go` is **not** modified (wiring lands in PR #4, unchanged from PR #1/#2's
note); confirmed via `rg -n "infra/install/compose" cmd/regixtry/main.go` → no match.

## TDD Cycle Evidence (PR #3)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 6.1 | `registry_test.go` | Unit | 22/22 (PR #1/#2 baseline) | Written (unplanned in tasks.md, added per Strict TDD's non-negotiable rule) | Passed | 2 cases (bundled `up -d`, external `up -d --no-deps regixtry`) | Clean |
| 6.2/6.3 | `registry_test.go` | Unit | Covered above | Written | Passed | 3 cases (200/401 table-driven, real multi-attempt retry, bounded-failure) | Clean |
| 6.4/6.5 | `provenance_test.go` | Unit | Covered above | Written | Passed | 2 cases (full-field structural assertion, no-secret round trip) | Clean |
| 6.6/6.7 | `down_test.go` | Unit | Covered above | Written | Passed | 2 cases (happy-path teardown, files-removed-despite-compose-failure) | Clean |
| 6.8 | N/A (compile-only check, not committed) | N/A | N/A | N/A | N/A | N/A | Interface-literal compile check confirmed, then removed |

### Test Summary

- **Total tests written (PR #3)**: 10 top-level test functions (1 is table-driven with 2
  subtests collapsed into one)
- **Total tests passing**: all 32 in the package (22 from PR #1/#2 + 10 new),
  `go test ./internal/infra/install/compose/...` → `ok`
- **Layers used**: Unit only (fake-exec + fake HTTP-status-fetcher, still no Docker daemon and
  no real network call), matching tasks.md's stated scope for this PR
- **Approval tests**: None — no refactoring of pre-existing behavior, only additive methods
- **Pure functions created**: `provenanceFromProject`
- **RED-path proof performed**: `TestWaitReachableRetriesUntilReachable` and
  `TestDownStillRemovesFilesWhenComposeDownFails` were written specifically to prove their
  respective loops/best-effort-cleanup paths actually execute (not a zero-iteration ghost
  loop) — both assert an exact call/sleep count that would fail if the implementation short-
  circuited on the first attempt

## Work Unit Evidence (PR #3)

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./internal/infra/install/compose/...` → `ok regixtry/internal/infra/install/compose 0.008s-0.012s`, 32/32 test functions pass |
| Runtime harness | N/A — per tasks.md, this PR is fake-exec unit tests only; no Docker daemon or real network call needed or used |
| Rollback boundary | Revert `StartRegistry`/`WaitReachable`/`SaveProvenance`/`Down` (this branch, `feature/container-setup-mode-03-compose-lifecycle`, on top of PR #2); `Preflight`/`WriteProject`/`StartDatabase`/`BootstrapAdmin` from PR #1/#2 keep working unmodified — confirmed by the full-suite regression run below |

## Full-Suite Regression Check (after PR #3)

`go build ./...` → clean. `go vet ./...` → clean. `gofmt -l internal/infra/install/compose/` →
empty (clean). `go test ./...` → all 19 packages `ok`, including
`regixtry/internal/infra/install/compose` freshly at `0.008s`-`0.012s`. No pre-existing test was
modified or broken.

## Deviations from Design (PR #3)

1. **`StartRegistry` (6.1) got a RED test tasks.md never explicitly asked for.** Unlike
   `WaitReachable` (6.2/6.3), `SaveProvenance` (6.4/6.5), and `Down` (6.6/6.7), tasks.md's 6.1
   is a bare "GREEN: implement..." with no paired RED item. Strict TDD Mode's Rule 1 ("NEVER
   write production code before writing its test") is explicitly non-negotiable, so
   `registry_test.go`'s two `TestStartRegistry*` functions were written first regardless, and
   confirmed to fail to compile before `StartRegistry` existed. This is an amplification of
   tasks.md's own instruction, not a deviation from design — the design's own Testing Strategy
   table requires ordering/argv-shape coverage for every `composeRunner` method.
2. **`WaitReachable`'s bounded-retry constants (30 attempts / 1s interval) are new, not
   reused from `waitPostgresReady`.** They are numerically identical to
   `postgresReadyMaxAttempts`/`postgresReadyPollInterval` (`database.go`, PR #2) but declared
   as a separate `registryReachableMaxAttempts`/`registryReachablePollInterval` pair in
   `registry.go` rather than shared constants, because the two polls check semantically
   different things (Postgres readiness vs. HTTP registry reachability) and tasks.md's Phase 6
   scope did not ask for a cross-cutting constants refactor. Both mirror the same bounded-retry
   shape `docs/verification/scripts/docker-push-pull-smoke.sh` already proved, per design's
   "Postgres readiness" decision.
3. **6.8's interface-literal compile check is not a committed artifact.** Design notes the
   `composeRunner` interface itself is declared in PR #4's `main.go`. To honor 6.8's "confirm
   the full method set compiles against the interface literal" without prematurely declaring
   that interface in this package (which is PR #4's scope, not PR #3's), the check was written
   as a throwaway `_test.go` file, run once via `go build`/`go vet`, confirmed to compile
   (`INTERFACE MATCHES`), then deleted before committing. No trace of it remains in the diff.
4. **`Down`'s file-removal is best-effort even when `docker compose down` fails**, following
   `rollbackWithReceipt`'s (`internal/infra/install/linux/bootstrap.go`) established
   join-errors shape rather than stopping at the first failure — this was judged the safer
   default so a failed teardown never leaves the project directory behind, and is proven by
   `TestDownStillRemovesFilesWhenComposeDownFails`.

## Review Workload / Ledger Flag (PR #3)

- **Declared ledger budget for this run**: 450 changed lines.
- **tasks.md's own PR #3 estimate**: 150-250 lines.
- **Actual (`git diff --shortstat` against `8d1088d`, the PR #2 tip this branch started from,
  including untracked new files via `git add -N`)**: **779 changed lines** (751 insertions +
  28 deletions across 7 files; the 28 deletions are all in `compose.go`'s edit to add the
  `httpStatusFetcher` seam and its default wiring).
- This is **329 lines (~73%) over the 450-line ledger budget** and **529-629 lines (~212-419%)
  over** the tasks.md upper estimate — the largest relative overage of the three PRs applied so
  far in this chain (PR #1 was ~4%/~33% over; PR #2 was ~33%/~82-112% over). Discovered honestly
  at the end of the batch, after all four methods and their tests were complete, not caught
  mid-flight — flagged here transparently rather than silently proceeding to PR #4.
- **Why**: four factors compounded past both estimates:
  1. This PR carries **four** distinct methods (`StartRegistry`, `WaitReachable`,
     `SaveProvenance`, `Down`) in one slice, each with its own seam, argv/JSON shape, and
     failure mode — tasks.md's own estimate compresses all four into a single 150-250 line
     range, but PR #1/#2 (which shipped 1-2 methods each) already landed 4-33% and 33-112%
     over their narrower ranges, so a wider four-method slice compounding the same per-method
     under-estimate was foreseeable in hindsight.
  2. `WaitReachable` needed an entirely new seam (`httpStatusFetcher` + `ProvisionerConfig`
     field + `Provisioner` field + `NewProvisioner` default wiring using `net/http`) — the
     same "new seam infrastructure, not just two methods" pattern PR #2's ledger note already
     flagged for `execStdinRunner`, repeating here for HTTP instead of stdin.
  3. Strict TDD's mandatory triangulation rule, applied honestly, produced 2-3 test cases per
     method rather than 1: `WaitReachable` needed a real multi-attempt retry case (not just
     single-shot success/failure) to prove the loop actually executes and doesn't pass on a
     zero-iteration ghost loop; `Down` needed a compose-down-failure case to prove file removal
     is genuinely best-effort, not just reachable code.
  4. Task 6.1's unplanned RED test (see Deviation 1) added a full test function tasks.md's own
     line estimate could not have accounted for, since tasks.md's bullet list only names a
     GREEN step there.
- **No padding**: every test asserts a real, spec-mapped, would-fail-if-wrong behavior; two
  assertions (`WaitReachableRetriesUntilReachable`'s call-count check and
  `DownStillRemovesFilesWhenComposeDownFails`'s post-failure `os.Stat` check) exist specifically
  to catch a trivially-passing "ghost loop" or "early-return" implementation, per Strict TDD's
  Assertion Quality Rules.
- **Recommendation**: accept as a well-justified overage — every line traces to either genuine
  new seam infrastructure (`httpStatusFetcher`, ~40 lines) or a triangulation case an assertion-
  quality-conscious reviewer would ask for anyway; or, if the reviewer's budget is genuinely
  hard-capped, a future run could split `SaveProvenance`+`Down` into their own immediately-
  following PR #3.5 (both are structurally independent of `StartRegistry`/`WaitReachable` and
  don't share test fixtures beyond `testProject`, which already lives in `database_test.go`
  from PR #2). No production code needs to change either way. **Pattern across the chain**:
  every PR applied so far (PR #1, #2, #3) has exceeded its tasks.md estimate and ledger budget,
  each time for triangulation-driven reasons the estimate did not anticipate — this is now a
  clear enough pattern that PR #4/#5's own tasks.md estimates (350-540 and 308-535 lines
  respectively) should be treated as likely-optimistic floors, not ceilings, when planning
  those batches' delivery strategy.

## Commits (this batch — PR #3)

1. `d59fbde` — `feat(compose): implement StartRegistry and WaitReachable`
2. `c37cc43` — `feat(compose): add SaveProvenance and Down teardown`

## Remaining Tasks (other PRs — NOT this batch's scope, as of PR #3)

- PR #4 (`feature/container-setup-mode-04-setup-wiring`): `cmd/regixtry/main.go` wiring — tasks.md Phase 8-9 — **now complete, see below**
- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12

---

# PR #4 — targets PR #3 branch (`feature/container-setup-mode-03-compose-lifecycle`)

**Branch**: `feature/container-setup-mode-04-setup-wiring` (current branch this batch ran on).
**Scope**: Wire the `compose` package into `cmd/regixtry/main.go` as the third setup mode —
`resolveSetupMode`, `runSetup`, `promptSetupDockerConfig`, the `composeRunner` interface, and the
`newComposeRunner` seam (tasks.md Phase 8-9). This is the highest-blast-radius PR in the chain:
`resolveSetupMode` and `runSetup` are the same shared dispatch functions the two already-shipped
setup modes (`binary-only`, `daemon-sqlite`) use, so this batch's primary discipline was proving
those two modes stay byte-for-byte unaffected, not just adding the third case.

## Status

PR #4: **12/12 tasks complete** (all Go/TDD work done and green; no tasks blocked).

## Completed Tasks (PR #4)

- [x] 8.1 RED: table-driven `resolveSetupMode` tests in a new `setup_docker_test.go` file —
  flag-literal `docker`, interactive numeric `3`, interactive literal `docker`, unknown-mode
  rejection, non-TTY error naming all three modes
- [x] 8.2 GREEN: `"docker"` literal branch + `3) docker` menu entry in `resolveSetupMode`
- [x] 8.3 `composeRunner` interface (8 methods, matching design.md's Interfaces/Contracts Go
  snippet verbatim) + `var newComposeRunner` seam, mirroring `newBootstrapRunner`
- [x] 8.4 RED: `promptSetupDockerConfig` prompt-visibility tests — bundled-vs-external prompt
  shown only when DSN absent, skipped entirely when DSN already supplied via flag
- [x] 8.5 GREEN: `promptSetupDockerConfig` implemented, mirroring `promptSetupDaemonConfig`'s
  shape exactly (same parameter list, including the unused `selectedInteractively` parameter
  for signature parity)
- [x] 8.6 RED: orchestration-order test against a fake `composeRunner` — asserts the exact
  `Preflight → WriteProject → StartDatabase → BootstrapAdmin → StartRegistry → WaitReachable →
  SaveProvenance` call sequence, admin username/password forwarded correctly, project directory
  derived under `<state-dir>/compose`, bundled DSN empty by default, generated password printed
  to stdout exactly once
- [x] 8.7 RED: rollback tests — one subtest per stage after `WriteProject`
  (`StartDatabase`/`BootstrapAdmin`/`StartRegistry`/`WaitReachable`/`SaveProvenance`) asserting
  `Down` runs exactly once as the last call; plus two negative-case tests proving
  `Preflight`/`WriteProject` failures never call `Down` (nothing was created yet at those stages)
- [x] 8.8 GREEN: `case "docker"` in `runSetup` — `validateSetupDockerConfig` (DSN-optional,
  unlike `validateSetupAuthConfig`), the ordered `composeRunner` calls, `rollbackDockerSetupFailure`
  on any post-`WriteProject` failure
- [x] 8.9 GREEN: on success, prints reachability, the compose env-file path, the generated
  bundled password (read back from the 0600 env file via a swappable `readComposeBundledPassword`
  seam, bundled-only — never printed for external-DSN projects), and the `docker exec -it
  <container> regixtry tui -storage-root /var/lib/regixtry` guidance line
- [x] 8.10 REFACTOR: `go vet ./...` + `gofmt -w .` — clean; `binary-only`/`daemon-sqlite`
  byte-for-byte-unchanged claim proven by dedicated regression tests, not just assumed
- [x] 9.1 `go test ./cmd/regixtry/...` — 101/101 top-level tests pass, 0 failures
- [x] 9.2 `go test ./...` — all 20 packages `ok`, `binary-only`/`daemon-sqlite` suites unaffected

## Files Changed (PR #4)

| File | Action | Notes |
|---|---|---|
| `cmd/regixtry/main.go` | Modified | Import of `internal/infra/install/compose`; `composeRunner` interface + `newComposeRunner` seam; `resolveSetupMode`'s `"docker"`/`3` case; `runSetup`'s `case "docker"`; `promptSetupDockerConfig`; `validateSetupDockerConfig`; `setupDockerImageRef`/`setupDockerPort` helpers; `readComposeBundledPassword` swappable seam; `rollbackDockerSetupFailure` |
| `cmd/regixtry/setup_docker_test.go` | Created | `fakeComposeRunner` + `swapComposeRunner` test helpers; `resolveSetupMode` table-driven tests; regression tests for `binary-only`/`daemon-sqlite`; `promptSetupDockerConfig` tests; orchestration-order test; rollback tests (5 post-`WriteProject` stages + 2 negative pre-stage cases); non-interactive admin-password requirement test |

## Regression Proof: `binary-only`/`daemon-sqlite` Unaffected

This was this PR's central risk (shared dispatch functions, widest blast radius in the chain),
so it is documented explicitly rather than assumed from a passing test run:

1. **`resolveSetupMode`'s existing two modes are covered by a dedicated regression test**
   (`TestResolveSetupModeRegressionBinaryOnlyAndDaemonSQLiteUnaffected`) asserting the flag-literal
   path, the interactive numeric/menu path (including the exact unchanged `"  1) binary-only"` /
   `"  2) daemon-sqlite"` menu lines), and the unsupported-selection error text format are all
   unchanged after the `docker` case was added.
2. **End-to-end regression test** (`TestExistingSetupModesEndToEndRegression`) runs `runWithIO`
   for both `binary-only` and `daemon-sqlite` with a `fakeComposeRunner` installed via
   `swapComposeRunner`, and asserts `len(composeFake.calls) == 0` — proving the new `composeRunner`
   seam is never invoked by either existing mode, not just that their own assertions still pass.
3. **Every pre-existing `cmd/regixtry` test was run unmodified** — `go test ./cmd/regixtry/...`
   (101/101 top-level tests, 0 failures) includes every `TestRunSetup*ForDaemonSQLite`,
   `TestRunSetupInteractivePromptSupportsBinaryOnly`, and related pre-existing test verbatim; none
   was edited to make this PR pass.
4. **Strict RED-before-GREEN was proven mechanically, not just narratively**: the full
   `setup_docker_test.go` file was written against the pre-PR-4 `main.go` (saved via `git diff` /
   `git checkout`), confirmed to fail to compile (`vet: ... undefined: composeRunner`), then the
   production changes were reapplied and the full suite reconfirmed green — the same discipline
   PR #3's apply-progress used for its interface-literal compile check, but for the whole PR this
   time since RED here spans multiple new production symbols at once.
5. **`installlinux.supportedMode`/`ValidateMode`/`ValidateConfig` were not touched** — confirmed by
   inspection: `runSetup`'s `case "docker"` never calls `installlinux.ValidateConfig`,
   `runner.Run`, or any other `bootstrapRunner` method; it uses only the new `composeRunner` seam.

## Deviations from Design (PR #4)

1. **New sibling test file (`setup_docker_test.go`), not `main_test.go`.** `main_test.go` is
   already 3238 lines before this PR; adding ~590 more lines of docker-mode-only tests to it would
   hurt reviewability. Go permits multiple `_test.go` files per package, so this is a pure
   organizational choice with no behavioral difference — `go test ./cmd/regixtry/...` runs both
   files as one test binary.
2. **Compose project directory derived from `-state-path`, not hardcoded.** Design's Data Flow
   diagram shows `/etc/regixtry/compose/regixtry.env` as an illustrative example. This PR derives
   the project directory as `filepath.Join(filepath.Dir(cfg.StatePath), "compose")` so it responds
   to `-state-path` overrides (needed for hermetic tests using `t.TempDir()`) while still resolving
   to exactly `/etc/regixtry/compose` under the real default `-state-path`
   (`/etc/regixtry/bootstrap-state.json`) — same value the design's example shows, derived rather
   than hardcoded.
3. **Image tag and host port are derived, not new CLI flags.** Design's "Image tag" decision says
   the compose image should pin to "the binary's own release version, latest only for dev builds"
   — implemented as `setupDockerImageRef()`, reading the existing `buildVersion` var (no new flag).
   The host port to publish is derived from `-public-url`'s own port via `setupDockerPort()`
   (falling back to `5000`), since neither design.md nor tasks.md calls for a new `-port`/`-image`
   flag and the existing `-addr` flag's semantics (local bind address for the in-process listener)
   don't apply to a mode that never listens in-process.
4. **`validateSetupDockerConfig` is a new function, not a call to `validateSetupAuthConfig`.**
   tasks.md 8.8 explicitly calls this "`validateSetupAuthConfig`-equivalent DSN handling", i.e. an
   equivalent, not a reuse — `validateSetupAuthConfig` requires a non-empty
   `AuthPostgresDSN` whenever auth is enabled, which is correct for `daemon-sqlite` (DSN is always
   operator-supplied there) but wrong for `docker` mode's bundled-Postgres default, where the DSN
   is legitimately empty and generated internally by `WriteProject`. Reusing the original function
   would have made the bundled-Postgres default (the "easy path" design explicitly calls for)
   unreachable non-interactively.
5. **The generated bundled password is read back from the env file, not returned by
   `WriteProject`.** `compose.Project` (PR #1) structurally carries no password field by design
   (`provenanceFromProject`'s doc comment in `provenance.go` states this explicitly, to make the
   provenance file structurally incapable of leaking it), and `WriteProject`'s signature is fixed
   by design.md's Interfaces/Contracts snippet, which this PR must not change. So `runSetup` reads
   the just-written 0600 file back via a new swappable `readComposeBundledPassword` var (mirroring
   `newBootstrapRunner`/`newComposeRunner`'s existing seam pattern) to disclose it once on screen,
   satisfying the "Bundled Credential Disclosure" spec requirement without widening the
   `composeRunner` interface beyond design's exact 8 methods.

## TDD Cycle Evidence (PR #4)

| Task | Test File | Layer | Safety Net | RED | GREEN | TRIANGULATE | REFACTOR |
|---|---|---|---|---|---|---|---|
| 8.1/8.2 | `setup_docker_test.go` | Unit | 101/101 pre-existing `cmd/regixtry` tests | Written (confirmed to fail to compile against pre-PR-4 `main.go`) | Passed | 4 cases (flag literal, numeric `3`, literal `docker`, unknown-mode rejection) + non-TTY error-text case | Clean |
| 8.3 | N/A (compile-only, proven by 8.6's orchestration test) | N/A | N/A | N/A (interface declaration, not a testable unit alone) | N/A | N/A | Interface matches design.md's Go snippet verbatim |
| 8.4/8.5 | `setup_docker_test.go` | Unit | Covered above | Written | Passed | 3 cases (DSN absent+declined, DSN absent+accepted, DSN already provided) | Clean |
| 8.6 | `setup_docker_test.go` | Unit | Covered above | Written | Passed | 2 cases (bundled default order, external-DSN skip-disclosure) | Clean |
| 8.7 | `setup_docker_test.go` | Unit | Covered above | Written | Passed | 7 cases (5 post-`WriteProject` failure stages + 2 pre-stage no-rollback negatives) | Clean |
| 8.8/8.9 | `setup_docker_test.go` | Unit | Covered above | Covered by 8.6/8.7 | Passed | Covered above | Clean |

### Test Summary (PR #4)

- **Total tests written**: 12 top-level test functions (several table-driven/subtest-based,
  ~30 assertions total across subtests)
- **Total tests passing**: 101/101 top-level tests in `regixtry/cmd/regixtry`, 0 failures
- **Layers used**: Unit only (fake `composeRunner`, no Docker daemon, no real network call),
  matching tasks.md's stated scope — "orchestration-order test uses a fake composeRunner, no
  daemon"
- **Approval tests**: None — no pre-existing test was edited
- **RED-path proof performed mechanically**: the entire `setup_docker_test.go` file was confirmed
  to fail to compile (`vet: ... undefined: composeRunner`) against the pre-PR-4 `main.go` via
  `git diff` + `git checkout` + `go vet`, before any production code was reapplied — see
  "Regression Proof" item 4 above

## Work Unit Evidence (PR #4)

| Evidence | Value |
|---|---|
| Focused test command and result | `go test ./cmd/regixtry/...` → `ok regixtry/cmd/regixtry 2.5s-4.9s`, 101/101 top-level tests pass, 0 failures |
| Runtime harness | N/A — per tasks.md, this PR's orchestration-order test uses a fake `composeRunner`; no Docker daemon needed or used |
| Rollback boundary | Revert `main.go`'s docker-mode additions (3 commits: composeRunner seam, `resolveSetupMode` case, `runSetup` orchestration) and `setup_docker_test.go`; `binary-only`/`daemon-sqlite` cases and every PR #1-#3 `compose` package method keep working unmodified, confirmed by the regression tests above |

## Full-Suite Regression Check (after PR #4)

`go build ./...` → clean. `go vet ./...` → clean. `gofmt -l .` → empty (clean). `go test ./...
-count=1` (fresh, not cached) → all 20 packages `ok`:
`cmd/regixtry` (4.9s), `internal/app/auth`, `internal/app/regixtry` (6.3s), `internal/app/scanning`,
`internal/domain/auth`, `internal/domain/regixtry`, `internal/domain/signing`,
`internal/infra/auth/postgres`, `internal/infra/cliprogress`, `internal/infra/install/compose`,
`internal/infra/install/linux`, `internal/infra/install/releases`, `internal/infra/metadata/sqlite`,
`internal/infra/release`, `internal/infra/scanning/gitleaks`, `internal/infra/scanning/trivy`,
`internal/infra/storage/fsblob`, `internal/ports`, `internal/protocol/http`, `internal/tui`.
No pre-existing test was modified or broken.

## Review Workload / Ledger Flag (PR #4)

- **Declared ledger budget for this run**: 900 changed lines. tasks.md's own PR #4 estimate:
  350-540 lines.
- **Actual (`git diff --stat` against `06c296a`, the PR #3 tip this branch started from, `git add
  -N` for the new untracked test file)**: **822 changed lines** (820 insertions + 2 deletions
  across 2 files — `main.go` 231 insertions/2 deletions, `setup_docker_test.go` 591 insertions, new
  file).
- This is **within the 900-line ledger budget** (78 lines / ~9% under) but **282-472 lines
  (~52-135%) over** tasks.md's own upper estimate — consistent with the chain-wide pattern every
  prior PR's ledger note already flagged (PR #1 ~4%/~33% over its narrower estimate; PR #2
  ~33%/~82-112%; PR #3 ~73%/~212-419%). This PR's relative overage against tasks.md is smaller in
  percentage terms than PR #2/#3's because tasks.md's own PR #4 estimate (350-540) was explicitly
  flagged in this session's forecast as "likely-optimistic floors, not ceilings" before this batch
  started.
- **Why**: three factors, matching the pattern:
  1. Strict TDD's mandatory triangulation, applied honestly, produced a wider `setup_docker_test.go`
     than a bullet-per-RED-test count would suggest — the orchestration-order test alone needed
     two full scenarios (bundled default, external-DSN skip) to prove the design's data-flow
     ordering *and* the password-disclosure divergence between the two, and the rollback tests
     needed 7 subtests (5 positive-failure stages + 2 explicit negative pre-`WriteProject` cases)
     to prove `Down` is called exactly when design says it should be and never otherwise.
  2. This PR's explicit orchestrator-mandated regression-proof requirement (dedicated
     `resolveSetupMode` regression test + end-to-end regression test with an installed
     `fakeComposeRunner` asserting zero calls) added two test functions tasks.md's own bullet list
     did not itemize, since tasks.md's 8.10 only says "confirm ... byte-for-byte unchanged" without
     specifying a dedicated test.
  3. `fakeComposeRunner` needed a configurable `writeProjectFunc` hook (not just static return
     values) so the orchestration-order test could write a real temp env file for the
     password-disclosure assertion — this is genuine test-infrastructure weight, the same
     "new seam infrastructure, not just N methods" pattern PR #2's `execStdinRunner` and PR #3's
     `httpStatusFetcher` ledger notes already identified, here on the test-double side instead of
     production code.
- **No padding**: every test asserts a real, spec-mapped, would-fail-if-wrong behavior; the two
  explicit negative-case rollback tests (`TestRunSetupDockerPreflightFailureNeverRollsBackOrWritesProject`,
  `TestRunSetupDockerWriteProjectFailureNeverRollsBack`) exist specifically to catch an
  over-eager rollback implementation that would call `Down` even when nothing was created yet.
- **Recommendation**: accept as a well-justified overage against tasks.md's own already-flagged-optimistic
  estimate, and within the session's 900-line ledger; no production code needs to change either way.

## Commits (this batch — PR #4)

1. `85b72e3` — `feat(compose): add composeRunner interface and newComposeRunner seam`
2. `8bd15f6` — `feat(setup): add docker case to resolveSetupMode`
3. `3e4bb2c` — `feat(setup): wire docker orchestration into runSetup`
4. `ee3a8b8` — `test(setup): cover docker setup mode wiring and prove regression safety`

## Remaining Tasks (other PRs — NOT this batch's scope, as of PR #4)

- PR #5 (`feature/container-setup-mode-05-docs-smoke`): docs + smoke script — tasks.md Phase 10-12

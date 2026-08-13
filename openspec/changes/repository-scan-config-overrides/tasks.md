# Tasks: Per-Repository Scan Config Overrides (repository-scan-config-overrides)

## Spec Correction Flagged (resolved mid-flight, see Phase 4 task 4.7)

`image-secret-scans/spec.md`'s Baseline Note and "A Disabling Gitleaks Override
Suppresses..." requirement assumed gitleaks has "no push-time path," citing the
shipped `gitleaks-managed-feature` requirement. **Verified false against current
code**: `executeScanRun` (`internal/app/regixtry/service_scanning.go:295,305`)
unconditionally launches `go s.executeSecretScanLeg(...)` regardless of
`run.Trigger`, and `executeScanRun` is itself reached by `queuePushScan`
(`:261-270`, shipped by `scan-policy-gate` this session, after the original
gitleaks spec was written). Gitleaks therefore already executes on push today.
Task 4.7 corrects the spec text (Baseline Note + a new push-suppression
scenario, mirroring Trivy's) **before** Phase 5 implements the gate, so the
implementation targets the corrected spec, not the stale one. Task 5.2 proves
the actual push code path is gated, not just the trigger-agnostic function
signature.

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~1300–1700 (prod ~380–480, tests ~950–1250) |
| 400-line budget risk | High |
| Chained PRs recommended | Yes |
| Suggested split | PR1 storage+ports -> PR2 resolution+runners (+spec fix) -> PR3 HTTP -> PR4 TUI |
| Delivery strategy | ask-on-risk (not specified by orchestrator; default assumed) |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: Yes
Chain strategy: pending
400-line budget risk: High

**Rationale**: 12 production files touched (1 new) plus a spec-text fix —
matching design.md's own risk note ("~12 files touched... possible line-budget
breach"). This is comparably scoped to `scan-policy-gate` (11 files,
~950–1250 lines under an old 800-line budget) but adds a brand-new table +
JSON codec registry, 2 runners each gaining argv+preflight, a nested HTTP
resource with ordering-sensitive dispatch, and a 4-field TUI modal (vs.
scan-policy-gate's 2-field modal) plus a row-annotation change. Test surface is
large: 2 argv-snapshot suites, a threat-matrix set (subprocess argv, arbitrary
file read, HTTP path dispatch, fail-open config), store round-trip tests, HTTP
resource tests, a router-ordering regression test, 5 TUI test files, and a gate
-coupling integration test. Design's own phase order (storage+ports ->
resolution+runners -> HTTP -> TUI) gives four independently revertable,
dependency-ordered slices — recommend chaining rather than a single PR.

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Storage + ports (Phases 1–2) | PR 1 | `go test ./internal/infra/metadata/sqlite/... -run RepositoryFeatureOverride` | N/A — pure storage, covered by store tests | Revert `store.go` DDL block + `ports/regixtry.go` types/methods |
| 2 | Resolution + runners + spec correction (Phases 3–6) | PR 2 | `go test ./internal/app/regixtry/... -run RepositoryOverride` and `go test ./internal/infra/scanning/... -run Override` | Manual: seed a Trivy override with `Enabled: false` for one repository, push an image, confirm no `scan_runs` row for it while other repositories still scan | Revert `repository_overrides.go`, override calls in `service_scanning.go`, argv/preflight blocks in both runners, spec.md wording |
| 3 | Admin HTTP resource (Phase 7) | PR 3 | `go test ./internal/protocol/http/... -run RepositoryOverride` | Manual: `curl -X PUT /admin/v1/features/trivy/repository-overrides/library/alpine` then `GET`/`DELETE` | Revert the `handleAdminFeatureResource` case + service methods used only by it |
| 4 | TUI modal + row annotation (Phases 8–9) | PR 4 | `go test ./internal/tui/... -run RepositoryOverride` | Debug print, modal render at height 24 (task 8.12) | Revert `session.go` struct + `admin_views.go` render fn + `model.go` wiring + `admin_tables.go` annotation |

## Phase 1: Ports & Types — foundation (Decisions 2, 3, 4)

- [x] 1.1 Add `ports.TrivyOverride`, `ports.GitleaksOverride`,
      `ports.RepositoryFeatureOverride` structs to `internal/ports/regixtry.go`
      (design Decision 2, 3 — exact field shapes as specified).
- [x] 1.2 Add 3 resolve-time-only fields to `ports.ScanSettings`:
      `IgnoreFilePath`, `IgnorePolicyPath`, `ConfigPath`, all `json:"-"`,
      absent from any SQL (design Decision 4 — mirrors `BinaryPath`/`CacheDir`
      precedent exactly).
- [x] 1.3 Add 4 methods to the `MetadataStore` interface: `GetRepositoryFeatureOverride`,
      `ListRepositoryFeatureOverrides`, `UpsertRepositoryFeatureOverride`,
      `DeleteRepositoryFeatureOverride` (design Decision 2 — typed `NotFound`,
      no `found bool` return).

## Phase 2: Storage (Decision 1, 2) — depends on Phase 1

- [x] 2.1 RED `internal/infra/metadata/sqlite/store_test.go`: `GetRepositoryFeatureOverride`
      on an absent row returns typed `domain.ErrorCodeNotFound`.
- [x] 2.2 RED `store_test.go`: `UpsertRepositoryFeatureOverride` then `Get`
      round-trips `payload` bytes and `updated_at` (RFC3339Nano).
- [x] 2.3 RED `store_test.go`: same repository, two feature names -> two
      independent rows; a third fabricated feature name round-trips with **no
      migration** (proves the reuse claim from proposal Success Criteria).
- [x] 2.4 RED `store_test.go`: `DeleteRepositoryFeatureOverride` on an absent
      row returns `NotFound`; on a present row removes it and a subsequent
      `List` no longer includes it.
- [x] 2.5 GREEN: append the `repository_feature_overrides` `CREATE TABLE IF NOT EXISTS`
      to `Store.init()`'s `statements` slice, exact SQL from design Decision 1.
- [x] 2.6 GREEN: implement Get/List/Upsert/Delete on `store.go` — `sql.ErrNoRows`
      -> `domain.NewNotFoundError`, `INSERT ... ON CONFLICT(tenant, repository,
      feature_name) DO UPDATE SET ...`, mirroring `Get/UpsertScanSettings` and
      `DeleteUpload` (design Decision 2).
- [x] 2.7 Confirm 2.1–2.4 GREEN: `go test ./internal/infra/metadata/sqlite/...
      -run RepositoryFeatureOverride`.

## Phase 3: Codec Registry + Resolution Helper (Decision 3, 4) — depends on Phase 1, 2

- [x] 3.1 RED `internal/app/regixtry/repository_overrides_test.go` (new):
      `normalizeTrivyOverride` rejects unknown fields (`DisallowUnknownFields`),
      a relative path, and a path starting with `-` (threat matrix: subprocess
      argv row), table-driven.
- [x] 3.2 RED: `normalizeGitleaksOverride` same table-driven cases for
      `ConfigPath`.
- [x] 3.3 RED: `applyTrivyOverride`/`applyGitleaksOverride` round-trip a
      payload into `ScanSettings`; a payload with an unknown extra field still
      applies (lenient-decode rollback tolerance, design Decision 3's
      documented asymmetry) — table-driven, no store.
- [x] 3.4 GREEN: create `internal/app/regixtry/repository_overrides.go` with
      `repositoryOverrideCodec`, `repositoryOverrideCodecs` map keyed by
      `trivyFeatureName`/`gitleaksFeatureName`, and the four
      normalize/apply functions, exact code from design Decision 3.
- [x] 3.5 RED `service_scanning_test.go`: `applyRepositoryOverride` returns
      `settings` unchanged on `NotFound`; returns `codec.Apply`'s result when
      a row is found; an unrecognized feature name in the registry returns
      `settings` unchanged (defensive branch, design Decision 4).
- [x] 3.6 GREEN: implement `Service.applyRepositoryOverride` in
      `repository_overrides.go`, exact code from design Decision 4.
- [x] 3.7 Confirm 3.1–3.3, 3.5 GREEN: `go test ./internal/app/regixtry/... -run
      'Normalize|ApplyOverride|RepositoryOverride'`.

## Phase 4: Wire Resolution Into Trivy Queueing (Decision 4 call-site table) — depends on Phase 3

- [x] 4.1 RED `service_scanning_test.go`: `queueScheduledScan` with a Trivy
      override `Enabled: false` for one repository does not queue a run for it
      even though the global row's `Enabled` is `true`.
- [x] 4.2 RED: `queuePushScan` with the same override does not queue on push
      (mirrors 4.1 for the push path).
- [x] 4.3 RED: `QueueManualScan` with the same override returns
      `domain.NewValidationError`, matching the existing global-disabled
      branch's error shape.
- [x] 4.4 RED: override `Enabled: true` re-enables scanning when the global
      row's `Enabled` is `false`, for scheduled, push, and manual triggers (3
      cases, table-driven — proposal's resolved question 4).
- [x] 4.5 GREEN: call `applyRepositoryOverride` for `trivyFeatureName` at all
      three call sites per design Decision 4's table (`queueScheduledScan`,
      `queuePushScan`, `QueueManualScan`), immediately after each site's
      existing global-settings resolution.
- [x] 4.6 Confirm 4.1–4.4 GREEN: `go test ./internal/app/regixtry/... -run
      'QueueScheduledScan|QueuePushScan|QueueManualScan'`.
- [x] **4.7 [SPEC CORRECTION]** Edit
      `openspec/changes/repository-scan-config-overrides/specs/image-secret-scans/spec.md`:
      (a) rewrite the Baseline Note to state that `executeScanRun`
      (`internal/app/regixtry/service_scanning.go:295,305`) unconditionally
      launches `executeSecretScanLeg` regardless of `run.Trigger`, and is
      itself reached by push (`queuePushScan`, shipped by `scan-policy-gate`
      after the original gitleaks spec was written) — so gitleaks already
      executes on push today; the shipped "Reused Rescan Trigger, No
      Push-Time Path" requirement's *trigger-source* claim still holds
      (gitleaks has no push-specific trigger of its own) but its practical
      "push never runs gitleaks" implication no longer does; (b) add a fourth
      scenario "Overridden repository does not scan on push" to the "A
      Disabling Gitleaks Override Suppresses..." requirement, mirroring
      `repository-vulnerability-scans/spec.md`'s "Overridden repository does
      not scan on push" scenario in shape.

## Phase 5: Wire Resolution Into Gitleaks Leg (Decision 4, corrected spec) — depends on Phase 3, 4.7

- [x] 5.1 RED `service_scanning_test.go`: `executeSecretScanLeg` with a
      gitleaks override `Enabled: false` does not create a `SecretScanRun` for
      a manual/scheduled trigger.
- [x] 5.2 RED `service_scanning_test.go`: **the same suppression exercised
      through the actual push code path** — call `queuePushScan` (not
      `executeSecretScanLeg` directly) for a repository with a gitleaks
      override `Enabled: false`, assert no `SecretScanRun` is created. This
      proves the corrected spec's push-suppression scenario against the real
      call chain, not just the trigger-agnostic function signature.
- [x] 5.3 RED: override `Enabled: true` re-enables gitleaks when the global
      row's `Enabled` is `false` (mirrors 4.4 for the secret leg).
- [x] 5.4 GREEN: call `applyRepositoryOverride` for `gitleaksFeatureName`
      inside `executeSecretScanLeg`, after `resolveManagedSecretScanSettings`
      (`service_scanning.go:357`), `!Enabled` -> silent return matching the
      existing branch (design Decision 4's table).
- [x] 5.5 Confirm 5.1–5.3 GREEN, explicitly re-running 5.2 in isolation:
      `go test ./internal/app/regixtry/... -run 'ExecuteSecretScanLeg|QueuePushScan' -v`.

## Phase 6: Runner Argv + Pre-Flight (Decision 5, 6) — depends on Phase 1

- [x] 6.1 RED `internal/infra/scanning/trivy/runner_test.go`: no override ->
      byte-identical argv to today (regression pin).
- [x] 6.2 RED: `IgnoreFilePath` set -> `--ignorefile` appended before
      `imageRef`; `IgnorePolicyPath` set -> `--ignore-policy` appended; both
      set -> both appended in that order (design Decision 5's exact argv
      shape).
- [x] 6.3 RED: an unreadable or missing `IgnoreFilePath` -> the runner returns
      an error before `exec.CommandContext`/`r.exec` is invoked (fake exec
      asserting it was never called — threat matrix "fail-open scan config").
- [x] 6.4 GREEN: add `os` import, `requireReadableFile` helper, and the
      conditional `--ignorefile`/`--ignore-policy` argv appends to
      `trivy/runner.go`, placed between the existing `--cache-dir` block and
      the `imageRef` append (design Decision 5, 6 exact code and placement).
- [x] 6.5 RED `internal/infra/scanning/gitleaks/runner_test.go`: no override ->
      byte-identical argv to today's fixed literal slice (regression pin,
      keeps the existing `gitleaks-managed-feature` argv-snapshot test valid
      unchanged).
- [x] 6.6 RED: `ConfigPath` set -> `--config` appended after the existing
      fixed slice.
- [x] 6.7 RED: an unreadable or missing `ConfigPath` -> the runner returns an
      error before `r.exec` is invoked.
- [x] 6.8 GREEN: add `requireReadableFile` (own copy in this package — the two
      runner packages do not import each other) and the conditional
      `--config` argv append to `gitleaks/runner.go`, after the existing fixed
      slice.
- [x] 6.9 Confirm 6.1–6.3, 6.5–6.7 GREEN: `go test ./internal/infra/scanning/... -run Override`.

## Phase 7: Admin HTTP Resource (Decision 7) — depends on Phase 1, 2, 3

- [x] 7.1 RED `internal/protocol/http/admin_handlers_test.go`: `GET
      /admin/v1/features/trivy/repository-overrides/library/alpine` returns
      404 when no override row exists (row-presence boundary on the wire,
      design Decision 7).
- [x] 7.2 RED: `GET` returns 200 with the override object plus `updated_at`
      when a row exists.
- [x] 7.3 RED: `PUT` persists and round-trips; `PUT` with a gitleaks-shaped
      body at the trivy resource returns 400 (`DisallowUnknownFields`).
- [x] 7.4 RED: `DELETE` returns 204, then a second `DELETE` on the same
      resource returns 404 (`DeleteUpload` precedent).
- [x] 7.5 RED: an unknown feature name in the URL returns 404 via the
      codec-registry lookup.
- [x] 7.6 RED **[threat matrix — HTTP path dispatch]**: a repository literally
      named `team/config` routes to the override handler, not the existing
      `HasSuffix(resource, "/config")` branch (design Decision 7's ordering
      hazard — the new `case` must be first in `handleAdminFeatureResource`'s
      switch, ahead of `/config` and `/status`).
- [x] 7.7 GREEN: implement `Service.GetRepositoryOverride`,
      `ListRepositoryOverrides`, `SetRepositoryOverride(ctx, repository,
      feature string, raw []byte)`, `ClearRepositoryOverride` in
      `repository_overrides.go` — feature-agnostic, call `parseRepository`
      first, then `codec.Normalize`.
- [x] 7.8 GREEN: add the `repository-overrides` case **first** in
      `handleAdminFeatureResource`'s switch (`admin_handlers.go:72-189`),
      using `adminNestedResource(resource, "/repository-overrides/")` and a
      `HasSuffix(resource, "/repository-overrides")` collection branch;
      wire GET/PUT/DELETE handlers.
- [x] 7.9 Confirm 7.1–7.6 GREEN: `go test ./internal/protocol/http/... -run RepositoryOverride`.

## Phase 8: TUI Modal (Decision 8) — depends on Phase 1, 7

- [x] 8.1 RED `internal/tui/session_test.go`: `repositoryOverrideModal.Active()`
      open/closed; `nextRepositoryOverrideField` skips
      `…PathSecondary` when `Feature == gitleaksFeatureName` (table-driven).
- [x] 8.2 RED `internal/tui/admin_views_test.go`: `renderRepositoryOverrideModal`
      row budgets per design Decision 8's table — <=19 (trivy+error), <=17
      (trivy, no error), <=17 (gitleaks+error), <=15 (gitleaks, no error).
- [x] 8.3 RED `admin_views_test.go`: the modal heading shows the repository
      name; the status line shows `Loading…` / `override active` /
      `inheriting global settings` for the three inheritance states.
- [x] 8.4 RED `internal/tui/model_test.go`: pressing `o` on a highlighted
      Repository Alerts row opens the modal bound to that repository and
      `trivyFeatureName`; `o` on a different admin screen or with no row
      highlighted does not open it (operator-admin-tui spec scenario).
- [x] 8.5 RED `model_test.go`: `o` does not collide with
      `featureActionForKey`'s fallback (regression guard per design Decision
      8's ordering note — the case sits before `model.go:1257`).
- [x] 8.6 GREEN: add `repositoryOverrideField`, `repositoryOverrideModal`,
      `Active()`, `AdminViewState.RepositoryOverrideModal` to `session.go`;
      zero it in `clearSelectedAdminDetails` and `applyFeaturePage` (design
      Decision 8 piece 1, exact struct shape).
- [x] 8.7 GREEN: add the `o` opener case, the `Active()` branch, the
      `updateRepositoryOverrideModalKey` handler, 2 `tea.Msg` types, and 3
      commands to `model.go`, modeled on `scanPolicyModal`'s equivalents
      (design Decision 8 piece 2).
- [x] 8.8 GREEN: add `Get/List/Set/ClearRepositoryOverride` to
      `admin_client.go`'s interface + HTTP implementation; `DELETE` via
      `requestNoContent(..., MethodDelete, ..., StatusNoContent)`; **do not**
      `PathEscape` the repository (design Decision 8 wire shape — escaping
      would defeat the server-side split).
- [x] 8.9 GREEN: add `renderRepositoryOverrideModal` to `admin_views.go`, a
      fifth `compositeOverlay` branch on `renderAdminWorkspace`, and `o` in
      `adminFeatureHelp` (design Decision 8 piece 3).
- [x] 8.10 RED `internal/tui/admin_tables_test.go`: a Repository Alerts row
      renders "scanning disabled" distinctly from both a never-scanned
      repository's row and a normally-scanned repository's row, for a
      repository whose override sets `Enabled: false` (operator-admin-tui
      spec, both scenarios).
- [x] 8.11 GREEN: annotate Repository Alerts rows in `admin_tables.go` using
      data from the list-overrides endpoint.
- [x] 8.12 Live-render verification: throwaway debug test rendering the modal
      as a floating overlay over the base workspace at height 24;
      `ansi.Strip` + `fmt.Println`; confirm the full bottom border and help
      line at both the 19-row (trivy+error) and 15-row (gitleaks, no error)
      extremes; delete before finishing (resolves design's "Live-render
      confirmation" open question).
- [x] 8.13 Confirm 8.1–8.5, 8.10 GREEN: `go test ./internal/tui/... -run RepositoryOverride`.

## Phase 9: Gate Coupling Integration (Decision 9) — depends on Phase 4, 6, 7

- [x] 9.1 RED `service_scanning_test.go` (integration): a Trivy ignore-file
      override changes `run.Critical` such that `OpenManifest` allows a digest
      it previously blocked, but **only after a rescan**; before that rescan
      completes, the gate still blocks the same digest (proposal's last
      Success Criterion; design Decision 9's two stated consequences).
- [x] 9.2 RED: with an override present for one repository, that repository's
      run fails/queues per its `Enabled` value while a second, non-overridden
      repository's run stays byte-identical in argv and outcome (proposal
      Success Criterion 2).
- [x] 9.3 Confirm 9.1–9.2 GREEN: `go test ./internal/app/regixtry/... -run
      'PullGate|PolicyCoupling' -v`.

## Phase 10: Non-Regression

- [x] 10.1 `go build ./...`, `go vet ./...`, `gofmt -l .` clean.
- [x] 10.2 Full `go test -count=1 ./...` green (not just touched packages).
- [x] 10.3 Confirm rollback inertness: with the migration applied but zero
      override rows, the existing `scan-policy-gate` and
      `gitleaks-managed-feature` test suites pass unchanged (Migration /
      Rollout section).
- [x] 10.4 Resolve design.md's Open Questions flagged for apply/verify: confirm
      the installed Trivy's actual `--ignorefile`-missing vs.
      `--ignore-policy`-missing behavior if the binary is available in this
      environment (else record as still-unverified, per design); confirm at
      150x24 the modal's full bottom border/help line (covered by 8.12).
- [x] 10.5 Update this file's checkboxes as work lands; save `apply-progress`
      to Engram at each phase boundary (for `sdd-apply` to resume from).
- [x] **10.6 [VERIFY REMEDIATION]** Added retroactively during `sdd-verify`
      remediation — this requirement was never decomposed into a task in the
      original Phase 2 pass. RED/GREEN
      `internal/infra/metadata/sqlite/store_test.go >
      TestStoreRepositoryFeatureOverrideResolutionIsExactNameOnlyNoOrphanLeakage`:
      proves `repository-config-overrides/spec.md`'s "Override Rows Are Not
      Cascade-Deleted On Repository Lifecycle Changes" requirement (both
      scenarios) via the closest testable proxy given no repository
      deletion/rename operation exists — an override upserted for one exact
      repository name must not be observable when querying a different
      repository name (NotFound, no fuzzy/prefix leakage), while the
      original row remains present and inert under its exact original name.
      Confirmed passing against the existing, unmodified implementation — no
      production code change required.

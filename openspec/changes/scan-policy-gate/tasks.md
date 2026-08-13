# Tasks: Vulnerability Policy Gate (scan-policy-gate)

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~950–1250 (prod ~260–330, tests ~690–920) |
| 800-line budget risk | Medium |
| Chained PRs recommended | No (single PR, phase-ordered commits) |
| Suggested split | Single PR; CI scan-status route stays in-line, see rationale below |
| Delivery strategy | ask-on-risk |
| Chain strategy | pending |

Decision needed before apply: Yes
Chained PRs recommended: No
Chain strategy: pending
800-line budget risk: Medium

**Rationale**: nine numbered work items across 11 files, but each production
diff is shallow (2 new store methods mirroring existing ones, one pure
function, a 4-line gate insertion, a 1-line goroutine call, 2 HTTP handlers
mirroring `handleAdminScanSettings`, one route dispatch, one modal struct +
render function, one composed line). The bulk of the line count is tests:
Decision 2's table (7 cases) is flagged in the design brief as the
highest-value surface and must not be under-tested; the route-collision case,
the fail-open 5-case integration matrix, and 2 mandatory live-render passes
all add lines without adding review risk per line. Estimate sits close to,
and plausibly over, the 800-line budget but every item is independently
revertable and low-coupling — **recommend a single PR with 9 commit
boundaries** (one per numbered decision) rather than chaining, so the
orchestrator can size-exception it if the running total exceeds budget after
Phase 4 (the gate) lands, without needing separate PR ceremony. **The CI
scan-status endpoint (Phase 7) is the one item explicitly flagged as
splittable in the proposal** — if the actual diff after Phases 1–6 already
approaches 800 lines, split Phase 7 (+ its table-driven router tests) into a
follow-up PR; it depends only on Phase 2 (already merged) and has no
downstream dependents (Phases 8–9 depend on Phase 1/6, not Phase 7). Given the
estimate is Medium and not High, and Phase 7 is small (~90–120 lines including
tests), the default recommendation is to keep it in-line and only split on
Decision needed before apply: Yes prompts a running-total check after Phase 6.

### Suggested Work Units

| Unit | Goal | PR | Focused test | Runtime harness | Rollback boundary |
|------|------|----|--------------|---------|--------------------|
| 1 | Policy settings storage (Decision 1) | single PR, commit 1 | `go test ./internal/infra/metadata/sqlite/... -run ScanPolicySettings` | N/A — pure storage, covered by store tests | Revert `store.go` DDL block + `ports/regixtry.go` type |
| 2 | `GetLatestScanRunByDigest` (Decision 2a) | commit 2 | `go test ./internal/infra/metadata/sqlite/... -run LatestScanRunByDigest` | N/A — pure storage | Revert new store method |
| 3 | `scanPolicyViolated` evaluator (Decision 2b) | commit 3 | `go test ./internal/app/regixtry/... -run ScanPolicyViolated` | N/A — pure function | Revert function in `service_scanning.go` |
| 4 | Pull gate in `OpenManifest` (Decision 3) | commit 4 | `go test ./internal/app/regixtry/... -run OpenManifest` and `go test ./internal/protocol/http/... -run WriteError` | Manual: `curl` a pull against a seeded critical-scan digest, expect 403 | Revert gate call + error code + `writeError` case |
| 5 | Push auto-queue (Decision 4) | commit 5 | `go test ./internal/app/regixtry/... -run QueuePushScan` | Manual: push then poll `scan_runs` table for a queued row | Revert `go queuePushScan(...)` line + method |
| 6 | Admin HTTP GET/PUT (Decision 5a) | commit 6 | `go test ./internal/protocol/http/... -run AdminScanPolicy` | Manual: `curl -X PUT /admin/v1/scan-policy` | Revert `admin_handlers.go` case |
| 7 | CI scan-status route (Decision 5b) | commit 7 (or follow-up PR if over budget) | `go test ./internal/protocol/http/... -run ManifestScanStatus` | Manual: `curl /v2/<name>/manifests/<ref>/scan-status` with pull-only token | Revert route dispatch + handler |
| 8 | TUI policy modal (Decision 6a) | commit 8 | `go test ./internal/tui/... -run ScanPolicyModal` | Debug print, modal render at height 24 (task 8.6) | Revert `session.go` struct + `admin_views.go` render fn + `model.go` wiring |
| 9 | TUI badge (Decision 6b) | commit 9 | `go test ./internal/tui/... -run 'TrivyTabs\|PolicyBadge'` | Debug print, composed tab line (task 9.4) | Revert composed line in `renderTrivyTabs` |

## Phase 1: Policy Settings Storage (Decision 1) — foundation, independent

- [x] 1.1 Add `ScanPolicySettings` struct and `ScanPolicyThresholdCritical`/
      `ScanPolicyThresholdCriticalHigh` constants to `internal/ports/regixtry.go`;
      add `GetScanPolicySettings`/`UpsertScanPolicySettings` to the
      `MetadataStore` interface.
- [x] 1.2 RED `internal/infra/metadata/sqlite/store_test.go`: seed no row, call
      `store.GetScanPolicySettings` — expect typed `domain.ErrorCodeNotFound`
      (store has no fallback, matching `GetScanSettings` precedent).
- [x] 1.3 RED `store_test.go`: `UpsertScanPolicySettings` then
      `GetScanPolicySettings` round-trips `enabled` and both threshold values,
      table-driven (mirrors `TestStorePersistsDefaultDisabledScanSettings`).
- [x] 1.4 GREEN: add `scan_policy_settings` `CREATE TABLE IF NOT EXISTS` to
      `Store.init()`'s `statements` slice (`store.go:945-1142`); implement
      `Get`/`UpsertScanPolicySettings` mirroring `Get`/`UpsertScanSettings`
      (`store.go:436-498`) exactly — `sql.ErrNoRows` →
      `domain.NewNotFoundError("scan_policy_settings", tenant)`,
      `INSERT ... ON CONFLICT(tenant) DO UPDATE SET`, `updated_at` as
      `time.RFC3339Nano`.
- [x] 1.5 Confirm 1.2–1.3 GREEN:
      `go test ./internal/infra/metadata/sqlite/... -run ScanPolicySettings`.
- [x] 1.6 RED `internal/app/regixtry/service_test.go`:
      `Service.GetScanPolicySettings` returns `Enabled: true, SeverityThreshold:
      ScanPolicyThresholdCritical` with **zero rows written** (fresh
      `t.TempDir()` store, no `Ensure*` call).
- [x] 1.7 GREEN: add `Service.GetScanPolicySettings` (code-level default
      fallback on `NotFound`, per design Decision 1 — NOT a migration default,
      NOT a startup `Ensure*` seed) and `Service.UpdateScanPolicySettings` to
      `internal/app/regixtry/service_scanning.go`.
- [x] 1.8 Confirm 1.6 GREEN: `go test ./internal/app/regixtry/... -run
      ScanPolicySettings`.

## Phase 2: `GetLatestScanRunByDigest` (Decision 2a) — independent of Phase 1

- [x] 2.1 RED `internal/infra/metadata/sqlite/store_test.go`: seed a `queued`,
      then a `completed`, then a `failed` run for the same digest (distinct
      `created_at`) — `GetLatestScanRunByDigest` returns the **newest run
      regardless of status**, including the `completed` one. This is the exact
      bug (`GetActiveScanRunByDigest` only sees `queued|running`) the design
      is preventing; do not skip this case.
- [x] 2.2 RED `store_test.go`: no run ever created for the digest —
      `GetLatestScanRunByDigest` returns typed `domain.ErrorCodeNotFound`, not
      a zero-value `ports.ScanRun`.
- [x] 2.3 GREEN: add `GetLatestScanRunByDigest` to `store.go` — same SELECT
      list/ordering/`scanRunRow` decoder as `GetActiveScanRunByDigest`, only
      dropping the `status IN (?, ?)` predicate; add the method to
      `MetadataStore` in `internal/ports/regixtry.go`.
- [x] 2.4 Confirm 2.1–2.2 GREEN: `go test
      ./internal/infra/metadata/sqlite/... -run LatestScanRunByDigest`.

## Phase 3: Pure Evaluator `scanPolicyViolated` (Decision 2b) — depends on Phase 1 & 2 types

- [x] 3.1 RED `internal/app/regixtry/service_scanning_test.go` (new file):
      table-driven over the full fail-open matrix from design Decision 2 — no
      scan (not applicable to this pure fn; covered at gate level) / queued →
      allow / running → allow / failed → allow / completed+critical at
      CRITICAL → block / completed+critical at CRITICAL+HIGH → block /
      completed+high-only at CRITICAL → allow / completed+high-only at
      CRITICAL+HIGH → block / completed+clean → allow / `Enabled: false` with
      a violating completed run → allow. This is the single highest-value test
      surface in the change; do not under-test it.
- [x] 3.2 GREEN: implement `scanPolicyViolated(settings ports.ScanPolicySettings,
      run ports.ScanRun) bool` in `internal/app/regixtry/service_scanning.go`
      exactly per design Decision 2 (unknown/empty threshold behaves as
      CRITICAL — the permissive default branch).
- [x] 3.3 Confirm 3.1 GREEN: `go test ./internal/app/regixtry/... -run
      ScanPolicyViolated -v` (verify all table cases individually, not just
      the aggregate pass).

## Phase 4: Pull-Time Gate (Decision 3) — depends on Phase 3

- [x] 4.1 RED `internal/domain/regixtry/errors_test.go` (or existing errors
      test file): `NewPolicyViolationError` produces
      `ErrorCodePolicyViolation`.
- [x] 4.2 GREEN: add `ErrorCodePolicyViolation ErrorCode = "POLICY_VIOLATION"`
      and `NewPolicyViolationError(message string)` to
      `internal/domain/regixtry/errors.go`.
- [x] 4.3 RED `internal/app/regixtry/service_test.go`: `OpenManifest` 403s
      (returns `ErrorCodePolicyViolation`) when the digest's latest completed
      scan has `Critical > 0` and threshold is CRITICAL; same setup but
      threshold CRITICAL+HIGH also blocks a high-only completed scan.
- [x] 4.4 RED `service_test.go`: `OpenManifest` allows the pull for each of —
      no scan run, `queued`, `running`, `failed`, and `Enabled: false` with an
      otherwise-violating completed scan (5 cases, table-driven).
- [x] 4.5 RED `service_test.go`: **regression guard** — `ResolveManifest`
      allows the same digest that the gate blocks in 4.3 (proves the TUI
      browse path is untouched).
- [x] 4.6 GREEN: add `Service.enforceScanPolicy(ctx, repository, digest string)
      error` to `service_scanning.go` (calls `GetScanPolicySettings` +
      `GetLatestScanRunByDigest`, `NotFound` → allow, else
      `scanPolicyViolated`); wire the call into `OpenManifest`
      (`internal/app/regixtry/queries.go:71-82`) after
      `s.metadata.ResolveManifest`, per design Decision 3 — do NOT modify
      `ResolveManifest`.
- [x] 4.7 RED `internal/protocol/http/router_test.go`: `writeError` maps
      `ErrorCodePolicyViolation` to HTTP 403 with code `DENIED` and **no**
      `WWW-Authenticate` header.
- [x] 4.8 GREEN: add the `ErrorCodePolicyViolation` case to `writeError`
      (`router.go:559-578`), between `NotFound` and the 400 group.
- [x] 4.9 Confirm 4.1, 4.3–4.5, 4.7 GREEN: `go test ./internal/app/regixtry/...
      -run 'OpenManifest|ResolveManifest'` and `go test
      ./internal/protocol/http/... -run WriteError`.

## Phase 5: Push Auto-Queue (Decision 4) — depends on Phase 1

- [x] 5.1 Add `ScanTriggerPush = "push"` constant to `internal/ports/regixtry.go`
      (sibling of existing `ScanTrigger*` constants; no migration needed,
      `trigger` column has no CHECK constraint).
- [x] 5.2 RED `internal/app/regixtry/service_test.go`: `queuePushScan` called
      directly (not through `PublishManifest` — avoids goroutine/sleep
      flakiness per design risk note) creates a `queued` `ScanRun` with
      `Trigger: ScanTriggerPush` when `ScanSettings.Enabled` is true and no
      active run exists for the digest.
- [x] 5.3 RED `service_test.go`: `queuePushScan` creates nothing when
      `ScanSettings.Enabled` is false.
- [x] 5.4 RED `service_test.go`: `queuePushScan` does not create a second
      `queued`/`running` run when `GetActiveScanRunByDigest` already reports
      one for the digest (dedup via the existing method, NOT
      `GetLatestScanRunByDigest`).
- [x] 5.5 GREEN: implement `queuePushScan(ctx context.Context, tenant,
      repository, reference, digest string)` in `service_scanning.go` —
      sibling of `queueScheduledScan`; receives the digest directly (no
      `ResolveManifest` round-trip); gates on `settings.Enabled` only, not
      `ScheduleEnabled`; dedups via `GetActiveScanRunByDigest`.
- [x] 5.6 GREEN: add `go s.queuePushScan(context.Background(), s.tenant(ctx),
      repository.String(), reference, manifest.Digest.String())` to
      `internal/app/regixtry/service.go` `PublishManifest` (after the
      metadata write succeeds, `service.go:201-205`) — tenant captured before
      the goroutine, `context.Background()` inside, errors dropped (never
      surfaced), matching the `executeSecretScanLeg` precedent.
- [x] 5.7 RED `service_test.go`: `PublishManifest` returns 201/success without
      blocking on scan completion (assert the call returns before any scan
      state transition is observable).
- [x] 5.8 Confirm 5.2–5.4, 5.7 GREEN: `go test ./internal/app/regixtry/... -run
      'QueuePushScan|PublishManifest'`.

## Phase 6: Admin HTTP GET/PUT (Decision 5a) — depends on Phase 1

- [x] 6.1 RED `internal/protocol/http/admin_handlers_test.go`: `GET
      /admin/v1/scan-policy` returns the current settings JSON; requires
      `requireAdminPrincipal`.
- [x] 6.2 RED `admin_handlers_test.go`: `PUT /admin/v1/scan-policy` with
      `{"enabled":true,"severity_threshold":"critical_high"}` persists and
      round-trips.
- [x] 6.3 RED `admin_handlers_test.go`: `PUT /admin/v1/scan-policy` with an
      unknown `severity_threshold` value returns 400 via
      `domainauth.NewValidationError` (proves the evaluator's permissive
      default branch is unreachable from the API).
- [x] 6.4 GREEN: add the `scan-policy` case to `handleAdmin`'s switch
      (`admin_handlers.go:34-53`), modeled on `handleAdminScanSettings`
      (`:189-214`); decode/response helpers for `ScanPolicySettings`.
- [x] 6.5 Confirm 6.1–6.3 GREEN: `go test ./internal/protocol/http/... -run
      AdminScanPolicy`.

## Phase 7: CI Scan-Status Route (Decision 5b) — depends on Phase 2

- [x] 7.1 RED `internal/protocol/http/router_test.go`: `GET
      /v2/<name>/manifests/<reference>/scan-status` returns 200 with
      `state: "unscanned"` when no run exists; `in_progress` for
      queued/running; `failed`; `clean` for a completed non-violating scan;
      `blocked` for a completed violating scan (5 cases, table-driven,
      asserting the exact response shape from design Decision 5:
      `would_block_pull`, `policy`, `scan` fields, `scan` omitted when
      unscanned).
- [x] 7.2 RED `router_test.go`: **route-collision case** — a manifest with tag
      literally named `scan-status` (i.e. `manifests/scan-status`) still
      routes to `handleManifest`, not `handleManifestScanStatus`. This is the
      exact hazard the design's suffix-matching-on-trimmed-reference fix
      exists to prevent; must be a distinct RED case, not folded into 7.1.
- [x] 7.3 RED `router_test.go`: a caller with valid pull-only credentials for
      the repository succeeds; a caller with no credentials gets 401; a caller
      with credentials scoped to a different repository is refused.
- [x] 7.4 GREEN: add `handleManifestScanStatus` to `router.go`, authorized via
      `ports.Action{Verb: ports.ActionPull, Repository: repository}` through
      `r.withPrincipal`; dispatch inside the existing `manifests/` case
      (`router.go:153-154`) — test `strings.HasSuffix` against the **trimmed
      reference**, not `suffix`, per design Decision 5.
- [x] 7.5 Confirm 7.1–7.3 GREEN: `go test ./internal/protocol/http/... -run
      ManifestScanStatus`.

## Phase 8: TUI Policy Modal (Decision 6a) — depends on Phase 1 & 6

- [x] 8.1 RED `internal/tui/session_test.go`: `scanPolicyModal.Active()`
      behaves like `trivyConfigModal.Active()` (open/closed state, focus
      cycling between the 2 fields).
- [x] 8.2 RED `internal/tui/admin_views_test.go`: `renderScanPolicyModal` <= 13
      rows with and without an error present; height is invariant across
      focus position (table-driven, no reflow) — mirrors
      `TestRenderTrivyConfigModalFitsWithinCompactedRowBudget`'s pattern at
      the 11/13-row budget instead of 17/20.
- [x] 8.3 RED `admin_views_test.go`: `renderScanPolicyModal` never renders
      inside/adjacent to `trivyConfigModal`'s output — **regression guard**
      proving the policy fields do not appear when `trivyConfigModal` is
      rendered standalone (spec's explicit "separate surface" scenario).
- [x] 8.4 GREEN: add `scanPolicyModal` struct (own struct, sibling of
      `trivyConfigModal:111-124`, NOT an extension of it) + field enum +
      `Active()` to `internal/tui/session.go`; add
      `AdminViewState.ScanPolicyModal`/`ScanPolicy` fields.
- [x] 8.5 GREEN: add `renderScanPolicyModal` to `internal/tui/admin_views.go`
      (heading + 2 fields × 2 rows + blank/help + `theme.section` chrome, per
      design Decision 6's 11-row budget); wire as a fourth branch of
      `renderAdminWorkspace`'s `compositeOverlay` tail; add `p` key handling
      and a save command in `internal/tui/model.go`; add `p: policy` to
      `adminFeatureHelp` (`admin_views.go:595-607`).
- [x] 8.6 Live-render verification: throwaway debug test rendering
      `renderScanPolicyModal` as a floating overlay over the base workspace at
      height 24; `ansi.Strip` + `fmt.Println`; confirm full bottom border,
      help line, and no reflow between the enabled-toggle and threshold-cycle
      focus positions; delete before finishing.
- [x] 8.7 Confirm 8.1–8.3 GREEN: `go test ./internal/tui/... -run
      ScanPolicyModal`.
- [x] 8.8 Remediation (post sdd-verify FAIL): RED/GREEN
      `internal/tui/model_test.go`
      `TestModelScanPolicyModalOpenToggleSubmitPersistsAndReflectsCurrentSettings`
      — a `Model.Update()`-level integration test (mirrors
      `TestModelTrivyConfigModalOpenCancelAndSubmitCurrentSettingsOnly`)
      that opens the policy modal via `p`, toggles `Enabled`, cycles
      `SeverityThreshold` via Tab+Space, submits via Enter, and asserts on
      `fakeAdminClient.updateScanPolicyCalls`/`lastScanPolicyInput` plus
      `AdminViewState.ScanPolicy`/`View()` post-submit state. Closes the two
      UNTESTED scenarios ("Operator toggles policy enabled state",
      "Operator changes the severity threshold") flagged by the
      `sdd-verify` CRITICAL finding. Test-only; no production code changed.

## Phase 9: TUI Policy Badge (Decision 6b) — depends on Phase 8

- [x] 9.1 RED `internal/tui/admin_views_test.go`: `renderTrivyTabs` still
      returns exactly 2 rows (`lipgloss.Height`) with the badge composed in,
      for both enabled and disabled policy states.
- [x] 9.2 RED `admin_views_test.go`: composed `renderTrivyTabs` output width
      stays `<= sectionWidth(150)` (design measures 55 of 146 columns; assert
      no wrap at the 150-column floor).
- [x] 9.3 RED `admin_views_test.go`: badge text is `Policy: ON (CRITICAL)` /
      `Policy: ON (CRITICAL_HIGH)` / `Policy: OFF` depending on state
      (table-driven); **regression guard** — badge content contains no
      non-ASCII/icon rune (spec's explicit "text, not glyph" scenario) via a
      rune-class assertion, not just a string-equality check.
- [x] 9.4 GREEN: compose the policy badge onto `renderTrivyTabs`'s existing
      tab line (`admin_views.go:228-237`) — `runtimeLabel + " | " +
      alertsLabel + "  " + policyBadge`, same 2-row return; make NO change to
      `contentBudget`/`fitLines`/`SectionRows`, matching the spec's explicit
      "no arithmetic change" scope note.
- [x] 9.5 Live-render verification: throwaway debug test rendering the
      composed Trivy tab line at the 150-column floor; `ansi.Strip` +
      `fmt.Println`; confirm no wrap and the badge is visibly distinct by eye;
      delete before finishing.
- [x] 9.6 Confirm 9.1–9.3 GREEN: `go test ./internal/tui/... -run
      'TrivyTabs|PolicyBadge'`.

## Phase 10: Non-Regression

- [x] 10.1 `go build ./...`, `go vet ./...`, `gofmt -l .` clean.
- [x] 10.2 Full `go test -count=1 ./...` green (not just touched packages).
- [x] 10.3 Combined live-render pass: one throwaway debug test rendering the
      final build's Trivy tab line with badge and the policy modal together;
      `ansi.Strip` + `fmt.Println`; inspect by eye; delete before finishing.
- [x] 10.4 Resolve design.md's Open Questions: confirm at 150x24 the tab line
      does not wrap and the modal shows its full bottom border/help line
      (live-DB confirmation of `init()` idempotency on an existing database is
      `sdd-apply`'s explicit responsibility per design, not a task here).
- [x] 10.5 Update this file's checkboxes as work lands; save
      `apply-progress` to Engram at each phase boundary (for `sdd-apply` to
      resume from, not performed during this task-planning phase).

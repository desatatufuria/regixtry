# Proposal: Optional Trivy Rescans

## Intent

Add optional Trivy-based rescans for already-published images so operators can refresh vulnerability findings after publication. CI/CD remains the first scan gate; Regixtry adds later manual and periodic rescans without blocking manifest publish.

## Scope

### In Scope
- Optional install-time Trivy settings, disabled by default, with provenance-backed persistence.
- Digest-centric manual and periodic rescans with stored run history and summary status.
- Admin API surfaces to view/update settings, trigger rescans, and inspect recent history.
- Basic documentation and verification updates for setup, admin API, and bounded scheduler behavior.

### Out of Scope
- Publish-path gating, admission control, or blocking manifest availability.
- A second helper service, multi-node coordination, or generic job platform.
- Rich TUI settings/history/actions beyond minimal visibility already present.
- Broader UX improvements and non-essential observability polish.

## Capabilities

### New Capabilities
- `image-vulnerability-rescans`: Optional Trivy scan settings, manual rescans, periodic rescans, digest-based run storage, and result inspection for published images.

### Modified Capabilities
- `installation-modes`: Setup must capture initial Trivy rescan configuration as part of managed runtime artifacts.
- `lifecycle-cli`: Setup/uninstall provenance must preserve and clean up managed rescan configuration truthfully.

## Approach

Use a bounded in-process scan subsystem with SQLite-backed settings and run history. Follow the operating model proven by Harbor and ECR: separate scan-on-push from later manual/scheduled rescans, keep scan state visible to operators, and persist queued/running/completed results. Use Trivy against image references resolved to manifest digests, share a cache under the storage root, and store scanner/db freshness evidence with each run. Make the admin API the first authoritative management surface; a richer TUI workflow remains a follow-up slice on top of the same backend contracts.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `cmd/regixtry/main.go` | Modified | Wire optional scan subsystem into `serve` and `setup`, including scheduler lifecycle. |
| `internal/infra/install/linux/` | Modified | Render, persist, and recover managed Trivy settings. |
| `internal/protocol/http/admin_handlers.go` | Modified | Add admin settings and manual rescan endpoints. |
| `internal/infra/metadata/sqlite/store.go` | Modified | Persist scan settings, run history, and scheduler state. |
| `docs/`, `README.md` | Modified | Document install-time config, admin API controls, scheduler expectations, and deferred TUI follow-up. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Scheduler overload or duplicate work | Med | Bound concurrency, persist ownership/state, keep scope single-process. |
| Stale findings misread as current truth | Med | Store scan timestamps and DB freshness on every run. |
| SQLite schema growth becomes brittle | Med | Keep schema narrow and aligned to one scan subsystem. |

## Rollback Plan

Disable the feature flag/config, stop scheduling new rescans, and revert scan wiring/routes/UI while leaving existing publish behavior unchanged. If needed, ignore or remove scan metadata tables without touching manifest/blob data.

## Dependencies

- Trivy CLI availability on operator-managed hosts.
- Updated install/admin operator documentation for the optional feature.

## Success Criteria

- [ ] Operators can enable optional Trivy rescans at setup, then view/change settings via the admin API with truthful persisted defaults and history.
- [ ] Operators can trigger digest-centric manual rescans and configure bounded periodic rescans for published images.
- [ ] Manifest publish remains non-blocking while scan runs/results persist independently.

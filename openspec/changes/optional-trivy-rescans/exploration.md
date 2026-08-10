## Exploration: Optional Trivy rescans for published images

### Current State
Regixtry currently publishes and serves OCI content, but it has no built-in vulnerability scanning, no scan persistence, no mutable settings subsystem, and no background scheduler beyond an inline `JobRunner` that executes work immediately in-process. The release workflow only runs `go test ./...`, GoReleaser, and installer smoke checks, so CI/CD can remain the first deployment gate, but the repository does not yet provide any registry-side rescan capability.

The operator value is clear: vulnerability intelligence changes after publication, so admins need digest-centric rescans, stored history, and admin-visible controls without turning publish into a blocking scan workflow. Verified extension points already exist for this shape: install/setup writes managed runtime env files and lifecycle provenance, `/admin/v1` already hosts authenticated admin CRUD routes, the TUI already consumes the admin API through `AdminClient`, and SQLite metadata already persists repositories, manifests, tags, blobs, and uploads by digest/reference.

Industry/Trivy-aligned operating model for this repo:
- Resolve tags to digests at trigger time and persist scan targets by manifest digest.
- Run Trivy against an image reference or digest reference, not a manifest payload by itself.
- Use a shared cache directory under the storage root (for example `<storage-root>/trivy-cache`).
- Let Trivy refresh its DB when needed for normal scans; use `--download-db-only` for explicit cache warm-up and reserve `--skip-db-update` for offline or explicitly stale-tolerant runs.
- Persist scan runs/results in Regixtry metadata with scanner version, DB epoch/freshness evidence, timestamps, status, and summary counts so stale policy is based on stored evidence, not only cache contents.

### Affected Areas
- `cmd/regixtry/main.go` — runtime wiring, `serve`/`tui` config parsing, and current inline `JobRunner` injection.
- `internal/infra/install/linux/templates.go` — managed env rendering for install-time Trivy configuration.
- `internal/infra/install/linux/provenance.go` and `internal/infra/install/linux/intent.go` — lifecycle provenance and upgrade-time recovery of managed settings.
- `internal/ports/auth.go` — admin API service contracts, which would need settings and scan-run endpoints.
- `internal/protocol/http/admin_handlers.go` — current `/admin/v1` routing pattern where security scanning settings and scan routes fit naturally.
- `internal/tui/admin_client.go` — admin API client seam for settings CRUD and manual rescan actions.
- `internal/tui/model.go` and `internal/tui/admin_views.go` — admin-only TUI surfaces for scanning settings, trigger actions, and latest summaries.
- `internal/app/regixtry/service.go` and `internal/app/regixtry/queries.go` — digest-centric manifest resolution and publish/query seams for scan target resolution.
- `internal/infra/metadata/sqlite/store.go` — current metadata schema and the place where scan settings, run history, and scheduling state would be persisted.
- `.github/workflows/release.yml` — confirms current CI gate stays separate from registry-side rescans.

### Approaches
1. **Embedded scan subsystem with bounded internal scheduler** — Keep scan orchestration inside the main Regixtry process, but as a narrow subsystem with persisted scan settings/runs, manual admin-triggered scans, and one bounded periodic batch loop.
   - Pros: Best fit for the current single-binary, single-node repo; reuses existing install/env/provenance flow; keeps admin API and TUI in the same product boundary; avoids packaging and operating a second managed service; easiest path to manual + periodic rescans now.
   - Cons: Adds background responsibility to the registry process; needs careful shutdown/locking/backpressure rules; future HA will need stronger coordination if the product becomes multi-node.
   - Effort: Medium

2. **Helper process using shared metadata/admin contracts** — Add a second process/binary that reads persisted scan settings and performs periodic rescans while Regixtry exposes config and manual trigger APIs.
   - Pros: Cleaner runtime isolation for long-running scan work; easier future separation if scan throughput grows; publish-serving path stays operationally simpler.
   - Cons: Overhead is too high for this repo right now: new binary, install/upgrade/unit management, new observability surface, and more failure modes before the product even has a generic settings/job subsystem.
   - Effort: High

3. **Manual/API-only rescans with external cron and no in-product scheduler** — Support manual triggers and batch endpoints only, leaving all periodic execution to external automation.
   - Pros: Smallest first slice; minimal runtime complexity; preserves digest-centric design and avoids queue/platform work.
   - Cons: Fails the stated product need for native periodic batch operation; pushes too much operational glue onto users; weakens TUI/admin value because the product cannot own schedule state.
   - Effort: Low

### Recommendation
Recommend **Approach 1: embedded scan subsystem with a bounded internal scheduler**, explicitly scoped to this repository's current reality. Regixtry is still a single-process, single-node product with managed install artifacts, inline jobs, SQLite metadata, an admin HTTP namespace, and an admin-oriented TUI. Adding a second helper process NOW would solve tomorrow's scale problem by creating today's operational complexity.

The recommended architecture is:
- Optional Trivy integration, disabled by default.
- Install-time defaults stored in managed env/provenance, then surfaced as mutable admin settings through API and TUI.
- Digest-centric scan targets: resolve a requested tag to a manifest digest at trigger time and store the resolved digest on each run.
- Manual `POST` trigger for an immediate rescan plus persisted schedule policy for periodic batches.
- A single bounded in-process scheduler loop that only enqueues/runs scan batches, not a generic distributed job platform.
- Stored scan runs/summaries in SQLite so results survive Trivy cache eviction and can be inspected later.
- No publish-path gating for this change; CI/CD remains the first gate and registry rescans stay decoupled from manifest publish success.

Recommended OpenSpec change name: **`optional-trivy-rescans`**.

### Risks
- The current SQLite store uses inline `CREATE TABLE IF NOT EXISTS` statements and has no versioned migration framework, so schema expansion must be designed carefully.
- A scheduler inside the main process is safe for today's single-node shape, but it must record run ownership/locking clearly to avoid double execution if deployment topology changes later.
- Trivy cache and DB growth need explicit operator controls and documentation (`download-db-only`, `clean --scan-cache`, `clean --vuln-db`).
- Stale-result policy must be explicit; otherwise operators may treat old findings as current security truth.

### Ready for Proposal
Yes — proceed with a proposal centered on `optional-trivy-rescans`, optional Trivy config persisted through setup + admin settings, digest-centric scan result storage, manual admin rescans, and a bounded internal scheduler for periodic batches without introducing publish-path blocking or a separate helper service.

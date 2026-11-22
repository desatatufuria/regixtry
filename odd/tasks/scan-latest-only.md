# scan-latest-only

Objective: scan results work like Harbor: one latest result per image digest, no rescanning when nothing changed.
Branch: `feat/scan-latest-only` (base `integration/audit-fixes`). Delivery strategy: ask-on-risk, local only; nothing is merged into develop or the tracker until the user tests it by hand. Test runner: `go test ./...` (GOTOOLCHAIN=go1.27.4, GOMODCACHE=/tmp/opencode/gomodcache, GOFLAGS=-mod=mod, GOPROXY=off).

## Specs

- S1 Latest only. User: "1. solo el ultimo escaneo". Acceptance: one latest result per (tenant, repository, digest); a completed scan replaces the previous one.
- S2 Scan fingerprint. Acceptance: fingerprint = trivy version + vulnerability DB `updated_at` (+ DB version) + hash of the effective scan configuration (ignore file path and content hash, ignore policy path and content hash, severity threshold and scanners as applicable).
- S3 One decision function. User: "2. sí" (a manual scan always forces a rescan and replaces the result). Acceptance: a single `needs scan?` function is used by push, scheduled sweep and manual triggers; push and sweep skip a digest whose latest completed result has the current fingerprint; manual always scans and replaces.
- S4 Replacement safety. Acceptance: the pull policy reads the latest completed run and fails open without one, so a digest must NEVER be without a completed result during replacement: write new + delete old in ONE transaction; a failed rescan keeps the previous completed result and marks it stale; the audit-branch `skipped` status must still never outrank a real result.
- S5 Sweep. User: "3. sí" (download the Trivy vulnerability DB once per sweep, needs network, and compare its metadata). Acceptance: refresh the Trivy DB once per sweep (or read its cache metadata), compare with the previous sweep's DB, scan only new digests or digests with a different fingerprint; cover ALL digests with tags (not only `latest` of the first 100 repositories) with pagination and bounded concurrency; record a `last_checked_at` per digest even when not rescanned.
- S6 Migration. User: "5. purgar al migrar". Acceptance: purge existing duplicate history keeping only the newest completed run per (tenant, repository, digest) (and the newest failed run only when no completed one exists), in batches through the sqlite write queue (`beginWrite`), idempotent, with a marker row like the repository_blobs backfill, tested on an old-schema fixture with many duplicates, documented as irreversible (backup advice) in the release notes and docs.
- S7 TUI and API. Acceptance: Scan runs shows one row per digest with the DB version used and the last check time; unknown or new columns degrade gracefully; docs updated (`docs/api.md`, `docs/tui.md`, `docs/configuration.md`).
- S8 Out of scope. User: "4. después" (Gitleaks secret scanning gets the same treatment later, as a separate feature). Also out of scope: notifications, per-image exceptions.

## Tasks

- T1 [S1-S4] Fingerprint + settings hash + latest-only model + needs-scan decision + atomic replacement + regression tests - route: delegated writer - status: done - commit: d39e0b4 (schema+atomic replacement), 1615743 (fingerprint+decision), ce73158 (docs); evidence: RED = new tests do not compile on base (new ports fields/methods), plus mutation checks (policy reading any-status latest -> in-flight assertion FAILs; push without decision -> SameFingerprint test FAILs); GREEN = go test ./... ; fix commit 1298640 (DB from cache metadata after scan, pre-scan settings hash, bounded/no-symlink metadata read, newer-DB-wins on out-of-order completion; RED on previous commit: 3 new tests failed); Risk: item 1 (data), item 2 (security via pull policy)
- T2 [S5] Sweep rewrite: DB refresh and compare, all digests, pagination, bounded concurrency, `last_checked_at` - route: delegated writer - status: done - commit: fb8354b (sweep, scheduler lease renewal, store, status), 6870b5e (docs); evidence: RED = sweep tests scanned 98 of 750 digests and never refreshed the DB on the old code, mutation checks (no cancel cleanup -> stuck-run test FAILs; no renewal -> lease tests FAIL); GREEN = go test ./... (except a pre-existing cmd/regixtry -race test race); Risk: item 4 (sweep concurrency, lease), item 5 (REGISTRY_TRIVY_REFRESH_DB_ON_SWEEP)
- T3 [S6] Migration and purge with old-schema fixture - route: delegated writer - status: pending - commit: -; Risk: item 1 (deletes stored data)
- T4 [S7] TUI, API and docs - route: delegated writer - status: pending - commit: -
- T5 [S1-S7] Independent verification (fresh verifier) + native review - route: delegated verifier - status: pending - commit: -
- T6 Final report - route: inline - status: pending - commit: -

## Log

- L1 (2026-11-22) User, verbatim: "vamos a hacer lo que hace harbor. no tiene sentido si estamos usando la misma base de vulnerabilidades seguir escaneando la misma imagen ya escaneada. una vez o mil siempre darán el mismo resultado con la misma imagen y con la misma base de datos. quiero que generes un plan para esto"
- L2 (2026-11-22) User decisions, verbatim: "1. solo el ultimo escaneo / 2. sí / 3. sí / 4. después / 5. purgar al migrar". Meaning: (1) history: only the latest scan result per digest; (2) a manual scan always forces a rescan and replaces the result; (3) download the Trivy vulnerability DB once per sweep (needs network) and compare its metadata; (4) Gitleaks secret scanning gets the same treatment LATER (separate feature, out of scope now); (5) purge existing duplicate history when migrating (keep only the newest completed run per digest).
- L3 (2026-11-22) Facts verified in code: `scan_runs` already stores `trivy_version` and `db_updated_at`; `scan_run_db_freshness` exists; no retention exists; dedup only prevents concurrent in-flight runs; `RunScheduledScans` sweeps `latest` of the first 100 repositories; default `ScheduleEnabled` is false and Interval 24h; the default pull policy is Enabled with threshold CRITICAL and fails open without a completed run.
- L4 Branch cut from `integration/audit-fixes` (67ab6c0), which existed.

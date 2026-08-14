# Professional readiness and Trivy roadmap

> **Historical planning document.** This was written BEFORE Trivy/Gitleaks scanning and cosign signing were implemented. It proposed an env-var-based configuration model (`REGISTRY_TRIVY_ENABLED`, `REGISTRY_TRIVY_MODE=local|server`) and a set of new `scan_settings`/`scan_runs` tables. What actually shipped is different in shape: a SQLite-row-based `scan_settings`/scan-run persistence model driven through the general feature-registry pattern (`internal/app/scanning/`, `internal/infra/scanning/trivy/`, `internal/infra/scanning/gitleaks/`), managed via the `feature` CLI subcommand and `/admin/v1` feature routes rather than dedicated Trivy-only env vars or a `local|server` mode toggle. The catalog scope bug described below has also since been fixed. See `docs/roadmap.md` and `docs/documentation-audit.md` for current status; treat the sections below as historical context, not a live plan. Corrections are inlined where the delta is small; larger proposals are left as-is to preserve the original reasoning, but must not be read as an accurate description of the shipped system.

This document captures the production-readiness assessment for Regixtry *as it stood before the Trivy/Gitleaks/signing feature system was built*, and the proposed optional Trivy integration path that preceded it.

## Current verified state

- Regixtry is a single-process Go binary with:
  - OCI-compatible push/pull/catalog/tags/manifests/blobs
  - filesystem blob storage
  - SQLite metadata
  - optional Postgres-backed auth and admin API
  - Bubble Tea operator TUI
- The current runtime is single-tenant and single-node.
- The current job runner is inline only. There is no persistent worker or async queue.
- `_catalog` visibility is filtered by grants in application code.
- ~~There is a real catalog scope mismatch bug: challenge emits `registry:catalog:*`, scope parser expects `regixtry:catalog:*`.~~ **Fixed.** `internal/ports/regixtry.go`'s `Action.Scope()` now emits `"regixtry:catalog:*"` for `ActionCatalog` (verified against current source), matching the `scopeTypeRegixtry` constant in `internal/domain/auth/scope.go`. This bug is resolved.

## What is still missing for professional use

### Critical gaps

1. Security and authorization maturity
   - stronger RBAC/policy model
   - multi-tenant isolation
   - audit trail
   - token lifecycle cleanup
   - ~~scope/challenge consistency~~ (fixed — see the corrected note under "Current verified state" above)

2. Operational maturity
   - dedicated health endpoint
   - metrics
   - tracing/structured diagnostics
   - rate limiting

3. Data lifecycle
   - manifest/blob deletion
   - garbage collection
   - retention
   - quotas
   - integrated or fully productized backup/restore guidance
   - versioned migrations

4. Deployment architecture
   - remote object storage
   - replication / HA strategy
   - failure-domain recovery story

5. Product confidence
   - stronger CI for PR/main
   - conformance testing beyond narrow smoke coverage
   - documentation drift reduction

## Trivy: corrected model

Trivy does not meaningfully scan a raw image manifest by itself for package vulnerabilities.

What it really scans for image vulnerabilities is the image target as a whole:

- a registry image reference
- an image digest reference
- an OCI layout
- an image tar archive

The manifest or digest helps identify the image to scan, but the scanner still needs access to the image metadata/layers or an equivalent resolved target.

## Industry pattern

The industry pattern is usually NOT "manifest-only synchronous scanning".

The common product shapes are:

1. scan on push
2. scan on demand
3. async scan jobs with queue/backpressure
4. optional warm scanner service for performance/caching

The best long-term shape for Regixtry is:

- product trigger: scan on demand and optional scan on push
- execution model: async jobs
- scanner runtime: Trivy local process first, optional Trivy server later

## Recommendation for Regixtry

### Short answer

Start simple:

- make Trivy integration optional
- default to disabled
- execute Trivy on demand as a local process
- persist scan results in Regixtry metadata
- expose settings and results through admin API and TUI

Do NOT make a persistent Trivy container/service mandatory in v1 of this integration.

### Why

Regixtry today is a single binary with inline jobs, no queue, and no generic settings subsystem. A mandatory long-lived Trivy service would add operational complexity faster than product value.

### When a persistent Trivy service makes sense

Offer it later as an optional performance mode when any of these become true:

- repeated local scans make DB/cache warmup expensive
- scan throughput becomes meaningful
- multiple runtime nodes need shared scan infrastructure
- operators want a dedicated vulnerability feed/cache lifecycle

## Proposed configuration model

### Installation-time configuration

Add optional setup/runtime configuration such as:

- `REGISTRY_TRIVY_ENABLED=false`
- `REGISTRY_TRIVY_MODE=local|server`
- `REGISTRY_TRIVY_BINARY_PATH=trivy`
- `REGISTRY_TRIVY_SERVER_URL=`
- `REGISTRY_TRIVY_CACHE_DIR=<storage-root>/trivy-cache`
- `REGISTRY_TRIVY_TIMEOUT=...`
- `REGISTRY_TRIVY_SCAN_ON_PUSH=false`
- `REGISTRY_TRIVY_SEVERITY=HIGH,CRITICAL`
- `REGISTRY_TRIVY_IGNORE_UNFIXED=false`

Interpretation:

- `local`: Regixtry shells out to a local Trivy binary
- `server`: Regixtry talks to a Trivy server

Persist these through the same installation/lifecycle mechanism that already writes `regixtry.env` and lifecycle provenance.

## Proposed product behavior

### Initial trigger set

Phase 1 should support:

- manual admin-triggered scan for a repository manifest/image reference
- result retrieval from API and TUI

Phase 2 can add:

- optional scan on push after manifest publish succeeds
- optional rescan of the latest tag/digest

### Important sequencing rule

Do not block manifest publication on the first Trivy integration slice.

Reason:

- synchronous blocking scans will hurt push latency
- scanner failures will become registry failures
- the current runtime has no async resilience primitives yet

## Proposed API shape

Add a narrow admin surface instead of mixing this into the registry protocol:

- `GET /admin/v1/settings/security-scanning`
- `PUT /admin/v1/settings/security-scanning`
- `POST /admin/v1/repositories/{repo}/scans`
- `GET /admin/v1/repositories/{repo}/scans`
- `GET /admin/v1/repositories/{repo}/scans/{scan_id}`
- optional later: `POST /admin/v1/repositories/{repo}/scans:rescan-latest`

Initial response concerns:

- requested target (tag/digest)
- resolved manifest digest
- scanner mode (`local` or `server`)
- status (`queued`, `running`, `succeeded`, `failed`)
- timestamps
- severity summary
- top findings summary
- raw report reference or stored normalized subset

## Proposed TUI shape

Add two admin areas:

1. Security scanning settings
   - enabled/disabled
   - mode
   - server URL or binary path
   - severity policy
   - scan-on-push toggle

2. Repository/manifest scan actions
   - trigger scan for selected manifest/tag
   - view latest scan summary
   - drill into findings summary

Keep this admin-only at first.

## Proposed persistence model

### Configuration

- installation/runtime defaults in env + lifecycle provenance
- mutable admin settings in metadata store once a settings subsystem exists

### Results

Store scan runs/results in Regixtry metadata, not only in Trivy cache.

Likely new tables:

- `scan_settings`
- `scan_runs`
- `scan_targets`
- `scan_summaries`
- optional later: normalized `scan_findings`

Trivy cache/DB should live under storage root in local mode.

## Recommended delivery roadmap

### Track A — professional readiness first

1. ~~Fix catalog scope mismatch bug~~ — done.
2. Add health endpoint + metrics baseline — still open.
3. Add PR CI (test/build/lint/smoke) — still open.
4. Add migration discipline and operational backup guidance — still open.

### Track B — Trivy MVP

This track shipped, though not through the exact env-var/`local|server` shape proposed below — see the banner at the top of this document. What actually shipped: Trivy and Gitleaks as managed features under `internal/app/scanning/` and `internal/infra/scanning/`, with SQLite-backed `scan_settings`/scan-run persistence, `feature` CLI + `/admin/v1` administration, and TUI scan-history views. Cosign signature verification (`internal/domain/signing/`) shipped alongside it with its own fail-closed `/admin/v1/signing-policy` gate — a track this document did not originally scope.

Original MVP plan (for historical reference):

1. Add optional install/runtime config for Trivy
2. Add domain/service seam for scan requests/results
3. Add metadata persistence for scan runs
4. Add admin API for settings + manual scans + result retrieval
5. Add TUI admin screens for settings + scan trigger + summary
6. Execute Trivy as local process in on-demand mode

### Track C — Trivy hardening

1. Add async job execution instead of inline-only execution
2. Add optional scan-on-push trigger after manifest publish
3. Add retries/timeouts/backpressure
4. Add optional Trivy server mode
5. Add retention/rescan policies

### Track D — platform-grade supply chain

1. Provenance/signing roadmap
2. policy/gating on scan results
3. richer RBAC/tenancy around security workflows
4. OCI/supply-chain ecosystem expansion

## Recommended decision

Yes: Trivy fits the product direction.

But the correct first move is NOT "just run a scanner against the manifest".

The correct first move is:

- optional Trivy integration
- admin-driven on-demand scans first
- local-process mode first
- stored scan results
- API and TUI management surface
- async evolution after the MVP proves value

## Evidence

- `cmd/regixtry/main.go`
- `internal/app/regixtry/service.go`
- `internal/app/regixtry/queries.go`
- `internal/domain/auth/principal.go`
- `internal/ports/regixtry.go`
- `internal/ports/defaults.go`
- `internal/protocol/http/router.go`
- `internal/protocol/http/admin_handlers.go`
- `internal/infra/metadata/sqlite/store.go`
- `internal/infra/install/linux/templates.go`
- `internal/infra/install/linux/provenance.go`
- `internal/tui/model.go`
- `internal/tui/admin_views.go`
- `docs/roadmap.md`
- `docs/configuration.md`
- `docs/api.md`
- `docs/tui.md`
- `docs/operations.md`
- `docs/security.md`

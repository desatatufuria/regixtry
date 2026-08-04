## Exploration: registry-auth-v1

### Current State
The registry already has a clean seam for authorization checks, but it is only a binary anonymous-access gate today. `internal/app/registry/service.go` authorizes every registry action through `ports.AccessController`, `internal/protocol/http/router.go` translates unauthorized errors into `401` plus `WWW-Authenticate`, and `cmd/registry/main.go` wires the runtime with a configurable anonymous pull/push controller.

That seam is NOT sufficient for the requested v1 model yet. There is no identity concept, no credential verification flow, no token issuing endpoint, no repository-scoped role model, no user storage, and no Postgres adapter. The current challenge object only carries `scheme`, `realm`, and `service`, and the TUI uses `localOperatorAccessController` to bypass all access checks entirely.

### Affected Areas
- `internal/ports/registry.go` — current `AccessController`, `Action`, `Challenge`, and `MetadataStore` contracts are too small for authenticated principals, token claims, repository-scoped roles, and a Postgres-backed auth store.
- `internal/ports/defaults.go` — `NewConfigurableAccessController` is currently an anonymous allow/deny switch, so this is the main replacement point for real authn/authz wiring.
- `internal/app/registry/service.go` — all push/pull/catalog/inspect decisions already funnel through `authorize`, so this is the core enforcement layer for repository-scoped roles.
- `internal/app/registry/queries.go` — catalog, tags, uploads, and manifest inspection currently return all tenant data allowed by the metadata store; auth-aware filtering rules will affect these queries directly.
- `internal/protocol/http/router.go` — registry challenge behavior lives here; Vault-like token flow will require authenticated request parsing plus richer bearer challenge output.
- `cmd/registry/main.go` — CLI/runtime wiring is SQLite-specific today and has no auth config beyond anonymous flags; this entrypoint will need Postgres and auth bootstrap/config seams.
- `internal/infra/metadata/sqlite/store.go` — current metadata persistence is SQLite-only, so choosing Postgres for auth creates either a second datastore or a larger metadata-store migration decision.
- `internal/tui/model.go` — current TUI is read-only registry inspection; user administration introduces a new operator workflow and screen set.
- `cmd/registry/main.go` (`runTUI`, `localOperatorAccessController`) — the TUI currently bypasses auth completely, which is a deliberate v1 foundation shortcut but a direct mismatch with auth-enabled user administration.
- `internal/protocol/http/router_test.go`, `internal/app/registry/service_test.go`, `cmd/registry/main_test.go`, `internal/tui/model_test.go` — the existing tests cover anonymous gating and registry behavior, but there is no coverage yet for login, token issuance, scoped permissions, or user-admin UX.
- `go.mod` — the module only brings in `modernc.org/sqlite`; Postgres-backed auth will add new runtime dependencies.

### Approaches
1. **Auth as a dedicated subsystem beside the registry metadata store** — keep registry content metadata on SQLite for now, add a separate Postgres-backed auth subsystem for users, credentials, tokens, roles, and repo grants.
   - Pros: Smallest first slice; preserves the current registry foundation; isolates auth schema churn from content metadata; matches the user’s fixed Postgres choice without forcing a full storage migration.
   - Cons: Two datastores in v1; some operational complexity; future unification may still be desired.
   - Effort: Medium

2. **Full metadata-store migration to Postgres** — move both registry metadata and auth data onto Postgres before adding auth features.
   - Pros: Single relational store; easier cross-domain queries later; avoids split operational model.
   - Cons: Too much scope for a first auth slice; touches stable registry-path code and tests at the same time; increases rollout risk dramatically.
   - Effort: High

3. **Embed auth inside the existing access controller only** — keep the current store model, extend the access controller ad hoc for passwords/tokens/roles, and defer a larger auth architecture.
   - Pros: Fastest to start.
   - Cons: Architecturally weak; mixes authentication, authorization, token issuance, and persistence into a seam that currently only answers allow/deny; makes AppRole/external auth harder later.
   - Effort: Medium

### Recommendation
Use **Approach 1**.

The sane first slice is NOT the whole requested scope at once. Start with an auth foundation slice that delivers: local users in Postgres, password login, admin-preissued tokens, bearer token validation on registry HTTP requests, a repository-scoped fixed-role model, and the global anonymous-pull switch applied to catalog/tags/discovery.

Keep these explicitly OUT of the first slice: external credential backends, AppRole/goVault compatibility beyond interface seams, token self-service policy UX, and broad TUI user-administration workflows beyond minimal admin CRUD/listing. That keeps the change reviewable and avoids mixing protocol auth, storage migration, and rich operator UI in one jump.

Architecturally, proposal/design should lock these decisions early:
- Split **authentication** (credential verification + token issuance) from **authorization** (role evaluation on `ports.Action`).
- Introduce a principal/claims model because the current `Authorize(ctx, action)` contract has no user identity input.
- Extend bearer challenge support so the registry can advertise the token realm/service correctly for Docker-compatible auth flows.
- Add a dedicated auth repository layer backed by Postgres rather than forcing auth tables into the existing SQLite metadata adapter.
- Replace the TUI auth bypass with an explicit operator access model before shipping user administration.

### Risks
- The current `AccessController` contract is authz-only; without a principal/claims model, repository-scoped roles cannot be enforced cleanly.
- The current bearer challenge model lacks token-endpoint/scope richness, which is a compatibility risk for real Docker/Vault-like token flows.
- Catalog and tags are currently broad queries; once anonymous pull is off, discovery endpoints need correct auth gating and likely per-repository filtering semantics.
- The TUI currently trusts `localOperatorAccessController`; adding user administration on top of that would create a security/consistency hole unless the operator path is redesigned.
- Postgres is chosen only for auth so far; if the design quietly grows into a full metadata migration, the slice will balloon past a sane first implementation.
- Admin-preissued tokens and password login imply token lifecycle decisions (format, revocation, expiry, hashing, auditability) that are easy to under-specify.
- Future AppRole/external auth support needs a stable seam now; if local-user assumptions leak into service and router code, v2 auth will become expensive.

### Ready for Proposal
Yes — but only if the proposal narrows the first implementation slice to auth foundation + minimal admin management, rather than attempting full registry auth, full TUI administration, and storage migration in one step.

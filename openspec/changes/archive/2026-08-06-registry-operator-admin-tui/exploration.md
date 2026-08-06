## Exploration: registry-operator-admin-tui

### Current State
The backend trust boundary already exists. `/auth/token` exchanges Basic credentials for a 15-minute bearer token, `/admin/v1/*` accepts only Bearer access tokens, and `internal/app/auth/service.go` keeps the real operator safety rules (`requireAdmin`, disabled-user checks, admin-token TTL caps, and last-active-admin protection).

The TUI is NOT a client of that boundary yet. `cmd/registry/main.go` still builds the TUI against local `appregistry.NewService(...)` with `localOperatorAccessController{}` and, when auth is enabled, adds only a notice: `"Auth-backed admin actions are disabled in the local TUI until a real operator login flow exists."` `internal/tui/model.go` is still inspection-oriented and only knows local query flows for repositories, tags, manifests, blobs, and uploads.

### Affected Areas
- `cmd/registry/main.go` — `runTUI` currently wires local storage-backed services plus `localOperatorAccessController`; this is the main place that must stop treating the TUI as a privileged local path.
- `internal/tui/model.go` — current Bubble Tea model only supports inspection screens and unavailable-mutation notices; secure admin work needs authenticated client-backed state and session UX.
- `internal/tui/model_test.go` and `cmd/registry/main_test.go` — current coverage asserts inspection-only behavior and the auth-disabled notice; these tests define the regression line while the client model is introduced.
- `internal/protocol/http/router.go` — `/auth/token` is the existing login exchange the TUI should consume; bearer verification and invalid-token challenge semantics already live here.
- `internal/protocol/http/admin_handlers.go` — the TUI’s admin workflows should map to the shipped `/admin/v1/users`, `/grants`, and `/admin-tokens` routes instead of calling storage or services locally.
- `internal/ports/auth.go` — the admin DTOs and service contracts show the backend response/request shapes the TUI client must respect.
- `internal/app/auth/service.go` — remains the single enforcement boundary for admin checks, token TTLs, password validation, conflict handling, and last-admin protection.
- `internal/protocol/http/router_test.go` — already proves `/admin/v1` auth and error behavior; it is the contract reference for a future TUI HTTP client.
- `docs/architecture.md`, `docs/roadmap.md`, and `docs/verification/operator-admin-api.md` — already state that the TUI must become an authenticated API client, not a local shortcut; follow-up artifacts must stay aligned.

### Approaches
1. **Interactive password login with in-memory bearer session** — the TUI prompts for username/password, exchanges them through `/auth/token`, keeps the 15-minute bearer token in memory, and calls `/admin/v1/*` as a thin client.
   - Pros: Uses the existing backend contract exactly as designed; avoids storing long-lived admin credentials by default; keeps bearer scope/expiry semantics consistent with the registry and admin API; easiest path to preserving one trust boundary.
   - Cons: Needs session-expiry UX and either re-prompt or in-memory re-auth logic; requires a new HTTP client seam because the TUI is local-service-backed today.
   - Effort: Medium

2. **Preissued admin credential token as the primary TUI login** — the TUI asks for username plus admin credential token, exchanges that at `/auth/token`, then uses the returned bearer token for `/admin/v1/*`.
   - Pros: Reuses an existing credential type and avoids operator password entry in some environments.
   - Cons: Worse default security posture for an interactive human client because admin credential tokens are longer-lived (up to 30 days) and are effectively password-equivalent secrets; encourages persistent secret handling pressure in the TUI.
   - Effort: Medium

3. **Keep local TUI shortcuts and mirror backend rules in-process** — reintroduce local admin actions in the current TUI runtime while trying to imitate `/admin/v1` validation and auth behavior.
   - Pros: Fastest path to visible UI actions.
   - Cons: This is the WRONG boundary. It recreates the exact bug class the repo already backed away from: a privileged local operator path outside the authenticated backend.
   - Effort: Low/Medium

### Recommendation
Use **Approach 1**.

The safest operator model over the CURRENT backend is: **interactive username/password login to `/auth/token`, issue the existing 15-minute bearer token, keep session material in memory only, call `/admin/v1/*` for all admin actions, and clear the session on exit**. If the TUI later supports silent refresh, it should use only session-memory credentials and never write password or admin credential token material to disk in the first slice.

The first reviewable slice should stay narrower than “full admin TUI”: **add the authenticated client/session foundation plus read-only admin browsing**. Concretely: login screen/state, logout/session-expiry handling, an HTTP admin client, and GET-only `/admin/v1` views for users/grants/tokens while keeping create/enable/disable/reset/grant-write/token-write actions disabled in the UI. That slice proves the trust boundary, error handling, and client architecture before mutations expand blast radius. It should fit comfortably inside the requested 1200-line review budget; the mutation flows can follow as later slices.

### Risks
- The current TUI has no HTTP client seam; forcing admin API calls directly into the existing inspection model can tangle local registry browsing and remote admin-session concerns.
- If the first slice stores operator passwords or admin credential tokens on disk, it weakens the security model more than the backend does.
- Session expiry is guaranteed by the 15-minute bearer TTL; without explicit re-auth/logout UX, operators will experience confusing mid-session failures.
- Mixing local inspection data and authenticated admin data in one screen model without a clear boundary can reintroduce “thin client vs second backend” drift.
- If mutation buttons ship before authenticated read-only flows are stable, reviewers will have to judge transport, session, and destructive behavior all at once.

### Ready for Proposal
Yes — with the proposal explicitly scoped to password-based interactive session handling plus GET-only admin client flows first, and with local shortcuts remaining forbidden.

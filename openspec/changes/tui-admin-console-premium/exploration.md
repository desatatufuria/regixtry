## Exploration: tui-admin-console-premium

### Current State
The backend already supports the entire first mutation surface through authenticated `/auth/token` and `/admin/v1` routes: create user with `is_admin` at creation time, enable/disable user, reset password, put/delete grants, and create/revoke admin tokens. Those routes are backed by existing safety rules in `internal/app/auth/service.go`, including admin-only enforcement, username/password validation, disabled-user checks, admin-token TTL limits, and last-active-admin protection.

The TUI is only partially caught up. `cmd/regixtry/main.go` wires the admin console as a thin HTTP client when `--api-base-url` is present, but `internal/tui/admin_client.go` currently exposes only login, list users, list grants, list tokens, and enable/disable user. `internal/tui/model.go` still renders a plain string-based Bubble Tea UI with separate users/grants/tokens screens, selected-user context, and inline confirmation text for enable/disable. There is no premium layout system, no form system, no modal abstraction, and no client/model support yet for create user, reset password, grant writes, or token writes.

Verified user journeys already present today:
- Login through `/auth/token`, keep bearer token in memory, and return to login on expiry.
- Browse users, then open grants or admin tokens for the selected user.
- Enable or disable the selected user with confirmation.

Verified gaps for this change:
- Missing TUI client methods for create user, reset password, grant put/delete, token create/revoke.
- Missing premium visual system and sidebar/main-panel composition.
- Missing main-panel forms and short confirmation modals.
- Unsupported by current HTTP API surface: change `is_admin` after creation and delete user. The service has broader internal methods, but `PUT/DELETE /admin/v1/users/{id}` intentionally remain unavailable.

### Affected Areas
- `internal/tui/model.go` — current single-model state machine, screen routing, key handling, and plain-text rendering must absorb the premium admin workspace.
- `internal/tui/admin_client.go` — must grow the missing API-first mutation methods and keep expiry/error normalization consistent.
- `internal/tui/session.go` — already holds selected-user context and session lifecycle; it will likely need richer admin workspace state.
- `cmd/regixtry/main.go` — still composes inspection mode plus authenticated admin mode inside one Bubble Tea program.
- `internal/ports/auth.go` — defines the real request/response DTOs the TUI must honor for users, grants, password reset, and admin tokens.
- `internal/protocol/http/admin_handlers.go` — source-of-truth contract for allowed admin routes and payload shapes.
- `internal/protocol/http/router_test.go` — proves current HTTP availability and confirms out-of-scope routes still return `404`.
- `internal/tui/model_test.go` — must absorb the largest behavior delta because the admin UX shape is changing materially.
- `internal/tui/admin_client_test.go` — should expand from read/enable-disable coverage to full in-scope API usage.
- `openspec/specs/operator-admin-tui/spec.md` — current product spec still documents the narrower shipped slice and must be extended by follow-up phases.

### Approaches
1. **Extend the current Bubble Tea model with a premium admin workspace** — keep one TUI program, preserve API-first behavior, and introduce admin-specific layout/theme/form/modal substructures inside the existing model.
   - Pros: Reuses the shipped login/session/auth flow; keeps inspection and admin in one executable; lowest architectural churn; fits the existing selected-user context model.
   - Cons: `internal/tui/model.go` is already dense, so the change must deliberately extract admin view helpers/state to avoid a monolith.
   - Effort: Medium

2. **Split admin into a separate Bubble Tea subprogram before adding features** — build a more isolated premium console first, then reconnect it to the current launcher.
   - Pros: Cleaner long-term separation between local inspection and authenticated admin UX.
   - Cons: Higher refactor cost up front; bigger review surface; risks spending the first slice on architecture churn instead of delivering the requested API-backed admin actions.
   - Effort: High

### Recommendation
Use **Approach 1**, but keep the boundary disciplined: this change should redesign only the authenticated admin workspace while leaving `/auth/token` and `/admin/v1` as the only authority for admin state.

Recommended boundaries for proposal/spec/design/tasks:
- Keep the first SDD change limited to the CURRENT HTTP API surface only.
- Add a premium admin shell with **sidebar navigation + main-panel forms + short confirmation modals**.
- Preserve the existing selected-user context and make grants/tokens explicitly subordinate to that selected user.
- Add these journeys in this change: create user, create admin user (`is_admin=true` only at creation), enable/disable, reset password, add/change/remove grant, create/revoke admin token.
- Keep these explicitly out of scope: change `is_admin` after creation, delete user, silent token refresh, local mutation shortcuts, and any direct store/service bypass.

Likely screen structure:
- **Admin Login** — unchanged auth source, premium styling only.
- **Users Sidebar** — persistent list and selection state.
- **User Workspace Panel** — overview/actions for selected user plus entry points to reset password and status changes.
- **Create User Panel** — main-panel form, including `is_admin` at creation time.
- **Grants Panel** — selected-user-scoped CRUD form and list.
- **Admin Tokens Panel** — selected-user-scoped list, create form, revoke action, and one-time secret reveal state.
- **Short Confirmation Modal** — enable, disable, and revoke confirmations only.

This is ready to move straight into proposal/spec/design/tasks. The proposal should state clearly that the visual redesign is IN SERVICE of the API-first admin workflow, not a second control plane.

### Risks
- One-time admin token secrets need careful display rules so the TUI reveals them once without turning the premium redesign into a secret persistence feature.
- Reset-password and create-user flows add masked-input and validation states that the current text-only model does not yet abstract.
- Self-disable and session-expiry paths can eject the operator mid-flow; proposal/design must define fail-closed behavior for forms and modals.
- The current `internal/tui/model.go` is centralized; without planned extraction of admin-specific helpers, this change can become hard to review and maintain.
- The service layer supports broader mutations internally, so proposal/spec wording must prevent accidental scope creep into unsupported HTTP routes.

### Ready for Proposal
Yes — tell the user we verified the backend already supports the requested first-slice actions except post-creation `is_admin` changes and delete-user, and the real work now is a premium API-first TUI expansion of the existing authenticated admin client.

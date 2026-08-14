# TUI Reference

Start the console:

```bash
regixtry tui
```

Render a non-interactive snapshot:

```bash
regixtry tui -snapshot
```

## Screens

The console shows repositories, tags, manifests, blobs, and uploads. When `-api-base-url` is configured, the admin area can authenticate against `/auth/token` and load users, grants, admin tokens, and the backend-driven feature manager. **Console browsing itself requires no login and is not access-controlled** — see [`docs/security.md`](security.md#tui-local-browsing-is-not-access-controlled--by-design).

The `Read-only` toggle only exists on the **Create User** form (`screenAdminCreateUser`, beside `Admin` and `Enabled`): a read-only user pulls from every repository without an explicit grant, and never gains any admin-surface access. It is set once, at creation time. The **Edit User** screen (`screenAdminEditUser`) does not expose or re-show this flag — it only shows Username, Role, Status, and User ID, plus in-screen actions to open grants, tokens, change the password, or enable/disable the account.

The Admin Users screen (`screenAdminUsers`) supports `/` to enter a username-contains search filter (typing updates the list live, `Enter`/`Esc` leaves search mode), `n` to open the Create User form, `Enter`/`e` to open the selected user in Edit User, `f` to jump to the Features screen, `b` to jump to the Robots screen, and `r` to reload the user list.

From Edit User, `p` opens `screenAdminChangePassword` — a single new-password field; `Enter` saves it and `Esc` cancels back to Edit User. `g` opens the Grants screen and `t` opens the Tokens screen for the selected user. On the Grants screen (`screenAdminEditUserGrants`), `n` opens `screenAdminAddGrant` to add a grant, `e` opens the same form pre-filled with the selected grant to edit it, and `x` removes the selected grant (with a confirm modal). `screenAdminAddGrant` itself has a Repository field with autosuggest from known repositories: typing filters the suggestion list, `Up`/`Down` cycles the highlighted suggestion, and `Enter` commits the highlighted suggestion and advances to the Role field (`Space` cycles the role); a second `Enter` saves the grant. On the Tokens screen (`screenAdminEditUserTokens`), `n` opens `screenAdminCreateToken` (Name and TTL-seconds fields, `Enter` creates the token and reveals its one-time secret) and `x` revokes the selected token.

A repo-admin delegate (a user holding `repo-admin` on at least one repository, but not global admin) reaches a repository-scoped grants view directly from the Console Repositories screen (`g` key) instead of the global-admin user workspace: `screenRepoAdminGrants`/`screenRepoAdminAddGrant` list and mutate only the delegate's own repository's grants via `/admin/v1/repositories/{repo}/grants`, and the role picker never offers `repo-admin`. On `screenRepoAdminGrants`, `n` adds a grant, `e` edits the selected grant (refused with a status message if that grant is itself a `repo-admin` grant — those can only be changed by a global admin), and `x` removes the selected grant. `screenRepoAdminAddGrant` has no repository field (the repository is fixed context) — only Username (free text) and Role (cycled with `Space`, never offering `repo-admin`).

Global admins also get two robot-account screens, reached with `b` from the Users screen: `screenAdminRobots` lists robot accounts (repository, role, enabled state). `n` creates one, `t` reuses the existing token screens to issue/revoke its token, and `r` refreshes the list. `e` enables and `x` disables the selected robot (each is a no-op if the robot is already in that state) — but on this screen only, `d` is a separate, genuinely irreversible **delete** action that opens its own confirm modal ("This action cannot be undone"); this is the one admin screen where `x` does not mean delete. `screenAdminCreateRobot` collects name/repository/role/TTL and shows the created robot's one-time token secret exactly once, immediately after creation — the same reveal-once pattern already used for human admin tokens. Its Repository field has the same autosuggest as the Add Grant form: type to filter, `Up`/`Down` to cycle suggestions, `Enter` to commit the highlighted suggestion and advance to Role.

## Feature Manager

The admin Features screen is now a thin Bubble Tea shell:

- The left side stays a generic feature summary list built from `/admin/v1/features`.
- The selected feature page comes from `/admin/v1/features/{name}`.
- The backend declares the page header, ordered sections, and visible actions.
- Trivy narrows the backend page to runtime-oriented configuration and runtime sections, then adds two TUI-only tabs: `Runtime` and `Repository Alerts`.
- The `Runtime` tab is the default landing view after load or refresh.
- Trivy configuration edits happen in a modal that only submits the current settings fields already exposed by the backend page: schedule toggle, interval, timeout, registry reachable URL, and max concurrency.
- The `Repository Alerts` tab loads existing scan runs from `/admin/v1/scan-runs`, keeps rows ordered by highest severity and fix availability, and opens same-screen drill-down from `/admin/v1/scan-runs/{id}`.
- Alert detail keeps digest-scoped findings visible even when the current tag moved, and it renders reference freshness separately from DB freshness.
- Non-Trivy features keep the same generic feature shell with no Trivy-specific tab chrome.
- The shell only handles selection, refresh, tabs, confirmation, modal state, and operator feedback.

### Declared action behavior

- `Enter` or `r` refreshes the selected feature page.
- `e`, `x`, `i`, `u`, and `b` only appear in help when the backend declares `enable`, `disable`, `install-runtime`, `upgrade-runtime`, or `rollback-runtime` for the selected page.
- Confirmation copy for destructive actions is backend-authored.
- On Trivy, `Tab` switches between `Runtime` and `Repository Alerts`, `c` opens the configuration modal from `Runtime`, and `Enter` opens detail for the selected repository alert.
- Detail rows show compact vulnerability entries with severity, package, vulnerability ID, installed version, fixed version, and fixability so operators can stay inside the Trivy workflow.

## Key bindings

| Key | Screen | Action |
| --- | --- | --- |
| `q`, `Ctrl+C` | all | quit |
| `Tab` | inspection | open admin |
| `↑` / `k` | lists | move selection up |
| `↓` / `j` | lists | move selection down |
| `Enter` | repositories / tags | open the next detail |
| `Esc`, `Backspace` | detail views | go back |
| `b` | manifest | view blobs |
| `u` | manifest | view uploads |
| `d`, `x` | manifest / blobs / uploads | show unsupported mutation notice |
| `l` | authenticated admin | log out |
| `r` | admin users | reload users |
| `/` | admin users | enter username search filter |
| `n` | admin users | create user |
| `f` | admin users | jump to Features screen |
| `b` | admin users | jump to Robots screen |
| `p` | admin edit user | change password |
| `g` | admin edit user | open grants |
| `t` | admin edit user | open tokens |
| `n` | admin grants / repo-admin grants / tokens | add grant / create token |
| `e` | admin grants / repo-admin grants | edit selected grant |
| `x` | admin grants / repo-admin grants | remove selected grant |
| `x` | admin tokens | revoke selected token |
| `Up` / `Down` | Add Grant / Create Robot repository field | cycle repository autosuggest |
| `Enter` | Add Grant / Create Robot repository field | commit highlighted suggestion |
| `n` | admin robots | create robot |
| `e` | admin robots | enable selected robot |
| `x` | admin robots | disable selected robot |
| `d` | admin robots | delete selected robot (irreversible, confirm modal) |
| `t` | admin robots | manage selected robot's token |
| `Tab` | Trivy feature page | switch between `Runtime` and `Repository Alerts` |
| `c` | Trivy runtime tab | open the configuration modal for current Trivy settings |
| `↑` / `↓` | Trivy repository alerts tab | move between loaded scan runs |
| `Enter` | Trivy repository alerts tab | open same-screen detail for the selected scan run |
| `Esc` | Trivy repository alert detail | close detail and return to the alert list |
| `e`, `x` | admin feature page | run declared enable / disable when present |
| `i`, `u`, `b` | admin feature page | run declared install / upgrade / rollback when present |

The admin login flow keeps credentials in memory only, exchanges them for a Bearer token through `/auth/token`, and clears the session when the token expires.

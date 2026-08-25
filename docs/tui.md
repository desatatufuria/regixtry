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

The console shows repositories, tags, manifests, blobs, and uploads. When `-api-base-url` is configured, `Tab` opens the admin area, which authenticates against `/auth/token` and then lands on a domain-grouped menu. **Console browsing itself requires no login and is not access-controlled** — see [`docs/security.md`](security.md#tui-local-browsing-is-not-access-controlled--by-design).

### Navigation map

Post-login, the operator lands on `screenAdminMenu` — a bare, four-row domain menu (`↑`/`↓` to move, `Enter` to open a domain, `Esc` to leave the admin panel back to Console browsing):

```
Admin (screenAdminMenu)
├─ Browse                 -> screenRepositories (leaves the admin panel; unchanged Console drill-down)
├─ Security & Compliance  -> screenAdminFeatures (securityMenuScreen: Trivy / Gitleaks / Signing, three peers)
│   ├─ Trivy    -> trivyConfigScreen (Runtime) <-Tab-> trivyReposScreen (Repository Alerts)
│   ├─ Gitleaks -> gitleaksConfigScreen  ---'o' on a repo row--->  Gitleaks' own override editor screen
│   └─ Signing  -> signingConfigScreen   ---'o' on a repo row--->  Signing's own override editor screen
├─ Identity & Access      -> screenAdminUsers (Users, Robots, Repository Grants, Tokens — unchanged legacy screens)
└─ Operations             -> screenAdminOperations (Scan Runs / Secret Scan Findings, two peers)
    ├─ Scan Runs             -> repository picker -> Enter opens scan history, Vulnerabilities tab first
    └─ Secret Scan Findings  -> repository picker -> Enter opens scan history, Leaks tab first
```

Every domain row's own screen has an `Esc` that returns one level up to `screenAdminMenu`, except Identity & Access (`screenAdminUsers`, whose sub-screens keep their existing legacy Esc chain) and each Security & Compliance feature's own sub-screens (`Esc` there returns to the Security & Compliance list, `screenAdminFeatures`).

Each Security & Compliance feature owns two things: a **config screen** (global settings, enable/disable, runtime actions) and — for Gitleaks and Signing — its own dedicated **repository override list**, reached with `o` from the config screen. Trivy's own per-repository override is opened with `o` directly from its Repository Alerts row (`trivyReposScreen`), since that screen already lists repositories. The override editor (`overrideEditor`) is the same primitive for all three features: it opens bound to whichever feature's screen it was opened from, and no key can change which feature it edits.

**Secret Scan Findings has two independent entry points, by design (not a bug):** Operations → Secret Scan Findings reaches it directly; Trivy → Repository Alerts → `Enter` also reaches it as a drill-down, unchanged. Both open the same `scanHistoryScreen`, tracking which screen opened it so `Esc` returns to the correct caller — Trivy's drill-down defaults to the Vulnerabilities tab, Operations' Secret Scan Findings entry defaults to the Leaks tab. `m.screen` never changes while `scanHistoryScreen` is open; it composites as a floating overlay on top of whichever screen opened it, exactly like every other admin modal.

The `Read-only` toggle only exists on the **Create User** form (`screenAdminCreateUser`, beside `Admin` and `Enabled`): a read-only user pulls from every repository without an explicit grant, and never gains any admin-surface access. It is set once, at creation time. The **Edit User** screen (`screenAdminEditUser`) does not expose or re-show this flag — it only shows Username, Role, Status, and User ID, plus in-screen actions to open grants, tokens, change the password, or enable/disable the account.

The Admin Users screen (`screenAdminUsers`) supports `/` to enter a username-contains search filter (typing updates the list live, `Enter`/`Esc` leaves search mode), `n` to open the Create User form, `Enter`/`e` to open the selected user in Edit User, `b` to jump to the Robots screen, and `r` to reload the user list. `Esc` returns to the domain menu (`screenAdminMenu`).

From Edit User, `p` opens `screenAdminChangePassword` — a single new-password field; `Enter` saves it and `Esc` cancels back to Edit User. `g` opens the Grants screen and `t` opens the Tokens screen for the selected user. On the Grants screen (`screenAdminEditUserGrants`), `n` opens `screenAdminAddGrant` to add a grant, `e` opens the same form pre-filled with the selected grant to edit it, and `x` removes the selected grant (with a confirm modal). `screenAdminAddGrant` itself has a Repository field with autosuggest from known repositories: typing filters the suggestion list, `Up`/`Down` cycles the highlighted suggestion, and `Enter` commits the highlighted suggestion and advances to the Role field (`Space` cycles the role); a second `Enter` saves the grant. On the Tokens screen (`screenAdminEditUserTokens`), `n` opens `screenAdminCreateToken` (Name and TTL-seconds fields, `Enter` creates the token and reveals its one-time secret) and `x` revokes the selected token.

A repo-admin delegate (a user holding `repo-admin` on at least one repository, but not global admin) reaches a repository-scoped grants view directly from the Console Repositories screen (`g` key) instead of the global-admin user workspace: `screenRepoAdminGrants`/`screenRepoAdminAddGrant` list and mutate only the delegate's own repository's grants via `/admin/v1/repositories/{repo}/grants`, and the role picker never offers `repo-admin`. On `screenRepoAdminGrants`, `n` adds a grant, `e` edits the selected grant (refused with a status message if that grant is itself a `repo-admin` grant — those can only be changed by a global admin), and `x` removes the selected grant. `screenRepoAdminAddGrant` has no repository field (the repository is fixed context) — only Username (free text) and Role (cycled with `Space`, never offering `repo-admin`).

Global admins also get two robot-account screens, reached with `b` from the Users screen: `screenAdminRobots` lists robot accounts (repository, role, enabled state). `n` creates one, `t` reuses the existing token screens to issue/revoke its token, and `r` refreshes the list. `e` enables and `x` disables the selected robot (each is a no-op if the robot is already in that state) — but on this screen only, `d` is a separate, genuinely irreversible **delete** action that opens its own confirm modal ("This action cannot be undone"); this is the one admin screen where `x` does not mean delete. `screenAdminCreateRobot` collects name/repository/role/TTL and shows the created robot's one-time token secret exactly once, immediately after creation — the same reveal-once pattern already used for human admin tokens. Its Repository field has the same autosuggest as the Add Grant form: type to filter, `Up`/`Down` to cycle suggestions, `Enter` to commit the highlighted suggestion and advance to Role.

## Security & Compliance

Trivy, Gitleaks, and Signing are three peer feature screens (`securityMenuScreen`'s own list, `↑`/`↓` + `Enter` to open one). Each feature's screen owns its own state; none of them read or write another feature's fields (`internal/tui/screen.go`'s `adminScreen` contract — see [`docs/architecture.md`](architecture.md)).

- **Trivy** splits into two peer screens reached via `Tab`: `trivyConfigScreen` (Runtime — config, scan policy, enable/disable/install/upgrade/rollback actions) and `trivyReposScreen` (Repository Alerts — one row per repository, `o` opens the override editor for the highlighted row, `Enter` opens scan history for it, `r` refreshes).
- **Gitleaks** and **Signing** each have their own config screen (`gitleaksConfigScreen` / `signingConfigScreen`, `s` opens the edit modal) and their own dedicated repository override list (`o` from the config screen; `↑`/`↓` + `o` on a highlighted row opens the override editor, `r` refreshes).
- Every screen's rendered footer is generated from that screen's own key bindings (`bubbles/key` + `bubbles/help`); a binding that is not in the map cannot be handled, and cannot silently drift out of the rendered help.

### Declared action behavior (feature config screens)

- `Enter` or `r` refreshes the selected feature page.
- `e`, `x`, `i`, `u`, and `b` only appear in help when the backend declares `enable`, `disable`, `install-runtime`, `upgrade-runtime`, or `rollback-runtime` for the selected page.
- Confirmation copy for destructive actions is backend-authored.
- On Trivy's Runtime tab, `c` opens the configuration modal and `p` opens the scan policy modal.
- Detail rows show compact vulnerability entries with severity, package, vulnerability ID, installed version, fixed version, and fixability so operators can stay inside the workflow.

## Operations

`screenAdminOperations` is a bare two-row peer list (`↑`/`↓` + `Enter`): **Scan Runs** and **Secret Scan Findings**. Both rows open the same repository-picker shape (`scanRunsScreen`, one row per repository with a scan history), differing only in which tab `Enter` opens first (Vulnerabilities for Scan Runs, Leaks for Secret Scan Findings). `Esc` returns to Operations.

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
| `↑` / `↓` | admin domain menu | move between Browse / Security & Compliance / Identity & Access / Operations |
| `Enter` | admin domain menu | open the highlighted domain |
| `Esc` | admin domain menu | leave the admin panel back to Console browsing |
| `r` | admin users | reload users |
| `/` | admin users | enter username search filter |
| `n` | admin users | create user |
| `b` | admin users | jump to Robots screen |
| `Esc` | admin users | back to the domain menu |
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
| `↑` / `↓` | Security & Compliance list | move between Trivy / Gitleaks / Signing |
| `Enter` | Security & Compliance list | open the highlighted feature's own screen |
| `Tab` | Trivy | switch between Runtime and Repository Alerts |
| `c` | Trivy Runtime | open the configuration modal |
| `p` | Trivy Runtime | open the scan policy modal |
| `s` | Gitleaks / Signing config | open the configuration modal |
| `o` | Trivy Repository Alerts / Gitleaks repos / Signing repos | open the override editor for the highlighted repository |
| `↑` / `↓` | Trivy Repository Alerts / Gitleaks repos / Signing repos | move between repositories |
| `r` | Trivy Repository Alerts / Gitleaks repos / Signing repos | refresh |
| `Enter` | Trivy Repository Alerts | open scan history for the selected repository |
| `↑` / `↓` | Operations list | move between Scan Runs / Secret Scan Findings |
| `Enter` | Operations list | open the highlighted results screen |
| `Enter` | Scan Runs / Secret Scan Findings picker | open scan history for the selected repository |
| `Tab` / `Shift+Tab` | scan history | switch between Vulnerabilities and Leaks tabs |
| `←` / `→` | scan history | page through the repository's execution history |
| `↑` / `↓` | scan history | move the highlighted finding |
| `Enter` | scan history (Vulnerabilities tab) | open the highlighted finding's advisory link |
| `Esc` | scan history | close and return to whichever screen opened it |
| `e`, `x` | admin feature page | run declared enable / disable when present |
| `i`, `u`, `b` | admin feature page | run declared install / upgrade / rollback when present |

The admin login flow keeps credentials in memory only, exchanges them for a Bearer token through `/auth/token`, and clears the session when the token expires.

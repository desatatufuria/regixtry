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

Post-login, the operator lands on `screenAdminMenu` — a bare, five-row menu (four domains plus a shortcut to the grant screen) (`↑`/`↓` to move, `Enter` to open a domain, `Esc` to leave the admin panel back to Console browsing):

```
Admin (screenAdminMenu)
├─ Browse                 -> screenRepositories (leaves the admin panel; unchanged Console drill-down)
├─ Security & Compliance  -> screenAdminFeatures (securityMenuScreen: Trivy / Gitleaks / Signing, three peers)
│   ├─ Trivy    -> trivyConfigScreen (Runtime) <-Tab-> trivyReposScreen (Repository Alerts)
│   ├─ Gitleaks -> gitleaksConfigScreen  ---'o' on a repo row--->  Gitleaks' own override editor screen
│   └─ Signing  -> signingConfigScreen   ---'o' on a repo row--->  Signing's own override editor screen
├─ Identity & Access      -> screenAdminUsers (Users, Robots, Repository Grants, Tokens — unchanged legacy screens)
├─ Operations             -> screenAdminOperations (Scan Runs / Secret Scan Findings, two peers)
│   ├─ Scan Runs             -> repository picker -> Enter opens scan history, Vulnerabilities tab first
│   └─ Secret Scan Findings  -> repository picker -> Enter opens scan history, Leaks tab first
└─ Grant repository access -> screenAdminGrantAccess (see below)
```

Every domain row's own screen has an `Esc` that returns one level up to `screenAdminMenu`, except Identity & Access (`screenAdminUsers`, whose sub-screens keep their existing legacy Esc chain) and each Security & Compliance feature's own sub-screens (`Esc` there returns to the Security & Compliance list, `screenAdminFeatures`).

Each Security & Compliance feature owns two things: a **config screen** (global settings, enable/disable, runtime actions) and — for Gitleaks and Signing — its own dedicated **repository override list**, reached with `o` from the config screen. Trivy's own per-repository override is opened with `o` directly from its Repository Alerts row (`trivyReposScreen`), since that screen already lists repositories. The override editor (`overrideEditor`) is the same primitive for all three features: it opens bound to whichever feature's screen it was opened from, and no key can change which feature it edits.

**Secret Scan Findings has two independent entry points, by design (not a bug):** Operations → Secret Scan Findings reaches it directly; Trivy → Repository Alerts → `Enter` also reaches it as a drill-down, unchanged. Both open the same `scanHistoryScreen`, tracking which screen opened it so `Esc` returns to the correct caller — Trivy's drill-down defaults to the Vulnerabilities tab, Operations' Secret Scan Findings entry defaults to the Leaks tab. `m.screen` never changes while `scanHistoryScreen` is open; it composites as a floating overlay on top of whichever screen opened it, exactly like every other admin modal.

The `Read-only` toggle only exists on the **Create User** form (`screenAdminCreateUser`, beside `Admin` and `Enabled`): a read-only user pulls from every repository without an explicit grant, and never gains any admin-surface access. It is set once, at creation time. The **Edit User** screen (`screenAdminEditUser`) does not expose or re-show this flag — it only shows Username, Role, Status, and User ID, plus in-screen actions to open grants, tokens, change the password, or enable/disable the account.

The Admin Users screen (`screenAdminUsers`) supports `/` to enter a username-contains search filter (typing updates the list live, `Up`/`Down` pick a user, `Enter`/`Esc` leaves search mode), `n` to open the Create User form, `Enter`/`e` to open the selected user in Edit User, `b` to jump to the Robots screen, `g` to open [Grant repository access](#grant-repository-access) with the highlighted user preselected, and `r` to reload the user list. `Esc` returns to the domain menu (`screenAdminMenu`).

From Edit User, `p` opens `screenAdminChangePassword` — a single new-password field; `Enter` saves it and `Esc` cancels back to Edit User. `g` opens the Grants screen and `t` opens the Tokens screen for the selected user. On the Grants screen (`screenAdminEditUserGrants`), `n` opens `screenAdminAddGrant` to add a grant, `e` opens the same form pre-filled with the selected grant to edit it, and `x` removes the selected grant (with a confirm modal). `screenAdminAddGrant` itself has a Repository field with autosuggest from known repositories: typing filters the suggestion list, `Up`/`Down` cycles the highlighted suggestion, and `Enter` commits the highlighted suggestion and advances to the Role field (`Space` cycles the role); a second `Enter` saves the grant. On the Tokens screen (`screenAdminEditUserTokens`), `n` opens `screenAdminCreateToken` (Name and TTL-seconds fields, `Enter` creates the token and reveals its one-time secret) and `x` revokes the selected token.

A repo-admin delegate (a user holding `repo-admin` on at least one repository, but not global admin) reaches a repository-scoped grants view directly from the Console Repositories screen (`g` key) instead of the global-admin user workspace: `screenRepoAdminGrants`/`screenRepoAdminAddGrant` list and mutate only the delegate's own repository's grants via `/admin/v1/repositories/{repo}/grants`, and the role picker never offers `repo-admin`. On `screenRepoAdminGrants`, `n` adds a grant, `e` edits the selected grant (refused with a status message if that grant is itself a `repo-admin` grant — those can only be changed by a global admin), and `x` removes the selected grant. `screenRepoAdminAddGrant` has no repository field (the repository is fixed context) — only Username (free text) and Role (cycled with `Space`, never offering `repo-admin`).

Global admins also get two robot-account screens, reached with `b` from the Users screen: `screenAdminRobots` lists robot accounts (repository, role, enabled state). `n` creates one, `t` reuses the existing token screens to issue/revoke its token, `g` opens [Grant repository access](#grant-repository-access) with the highlighted robot preselected, and `r` refreshes the list. `e` enables and `x` disables the selected robot (each is a no-op if the robot is already in that state) — but on this screen only, `d` is a separate, genuinely irreversible **delete** action that opens its own confirm modal ("This action cannot be undone"); this is the one admin screen where `x` does not mean delete. `screenAdminCreateRobot` collects name/repository/role/TTL and shows the created robot's one-time token secret exactly once, immediately after creation — the same reveal-once pattern already used for human admin tokens. Its Repository field has the same autosuggest as the Add Grant form: type to filter, `Up`/`Down` to cycle suggestions, `Enter` to commit the highlighted suggestion and advance to Role.

## Grant repository access

One screen to give a user or robot a role on one or more repositories. Open it from the admin menu row **Grant repository access**, or press `g` on the Users or Robots list to start with that account preselected (focus then starts on the repositories pane). `Esc` returns to the screen it was opened from.

Three panes, switched with `Tab` / `Shift+Tab`:

| Pane | What it does |
| --- | --- |
| **Who** | Filter box plus a list of users and robots (the filter matches the name, case-insensitive). Below it, "Already has" shows the highlighted account's current grants. |
| **Which repositories** | Filter box plus a checklist of known repositories, annotated with the role the account already has. Exact names only, no wildcards. |
| **Role and review** | Pick the role: `1` Reader (pull), `2` Writer (pull, push, delete tags), `3` Admin (Writer plus manage who has access), or `Up`/`Down`. Reader is the default. The box below shows what `Enter` will do. |

Repository selection:

- Type in the filter to narrow the list; ticked repositories stay ticked while the filter changes.
- `Space` ticks or unticks the highlighted repository. `Ctrl+A` ticks every shown known repository, or unticks them all when all are already ticked.
- A valid repository name that is not known yet appears as an extra `+ name` row marked "not pushed yet". Only `Space` on that row ticks it, so `Ctrl+A` never selects it.

Applying:

- `Enter` applies from any pane. Nothing is sent before that. With no account or no repository selected, the status line says what is missing.
- Each repository gets its own grant. A failure does not stop the rest and nothing is rolled back; the review box then lists every repository as `granted` or with its error (failures first when the list does not fit), and the status line reads "Granted Reader on N of M repositories to name". Repositories that succeeded are unticked.
- While grants are being applied, every key is ignored except `Ctrl+C`; `Esc` only shows "Applying N grants, please wait". The screen cannot be left mid-apply.
- If the admin session expires, the TUI returns to the login screen with the expiry reason; repositories after the expiry are reported as "not attempted (session expired)".

Keys: `Tab`/`Shift+Tab` switch pane, `Up`/`Down` move, `Space` toggle repository, `Ctrl+A` toggle all shown, `1`/`2`/`3` role, `Enter` apply, `Esc` back. The filter fields take plain text, so `j`, `k`, `q` and `l` are typed there.

## Text entry

In text fields, letters are typed, including `j`, `k` and `q`: the login form, the user search while active, Create User username and password, the Repository field of Add Grant and Create Robot, and the Grant repository access filters. Arrow keys move suggestions and focus, `Esc` is the way out, and `Ctrl+C` always quits. `q` quits only on screens without a focused text input. Pasting works in text fields: a bracketed paste is delivered as text, so pasted key names such as "enter" stay text.

## Security & Compliance

Trivy, Gitleaks, and Signing are three peer feature screens (`securityMenuScreen`'s own list, `↑`/`↓` + `Enter` to open one). Each feature's screen owns its own state; none of them read or write another feature's fields (`internal/tui/screen.go`'s `adminScreen` contract — see [`docs/architecture.md`](architecture.md)).

- **Trivy** splits into two peer screens reached via `Tab`: `trivyConfigScreen` (Runtime — config, scan policy, enable/disable/install/upgrade/rollback actions) and `trivyReposScreen` (Repository Alerts — one row per repository, `o` opens the override editor for the highlighted row, `Enter` opens scan history for it, `r` refreshes).
- **Gitleaks** and **Signing** each have their own config screen (`gitleaksConfigScreen` / `signingConfigScreen`, `s` opens the edit modal) and their own dedicated repository override list (`o` from the config screen; `↑`/`↓` + `o` on a highlighted row opens the override editor, `r` refreshes).
- **Signing's** configuration modal (and its per-repository override editor) render trusted keys as a navigable list (`trustedKeyList`, shown as short fingerprints, never raw key bytes) rather than a single field: `n` starts adding one key (paste the PEM, `Enter` commits it), `x` deletes the selected key. Deleting first shows a usage-count advisory ("This key currently verifies N tagged image(s)... Delete it anyway?") from `GET /admin/v1/signing-policy/key-usage` — purely informational, it never blocks the deletion. A never-configured per-repository override's key list is pre-filled from the current global trusted keys the first time it loads (trusted identities are not currently included in this prefill).
- Alongside trusted keys, both the same modal and the per-repository override editor render trusted **identities** (keyless/Fulcio, `trustedIdentityList`) as their own separate navigable list, each shown as its SAN regexp and OIDC issuer: `n` starts adding one, `Tab` switches between the regexp and issuer fields, `Enter` commits the pair (validated client-side, then again authoritatively on save), `x` deletes the selected identity immediately — there is no usage-count advisory for identities, since no equivalent lookup exists. In the per-repository override editor, `Tab` cycles Enabled → trusted keys → trusted identities → Unsigned Self-Read → Clear; the trusted-key list keeps its established "reachable from any focus" shortcut for `n`/`x`/`↑`/`↓`, while the identity list only claims those same keys once the operator has explicitly tabbed onto it (the two lists cannot both claim identical keys unconditionally without ambiguity).
- Every screen's rendered footer is generated from that screen's own key bindings (`charm.land/bubbles/v2/key` bindings, rendered by the local `shortHelpView`); a binding that is not in the map cannot be handled, and cannot silently drift out of the rendered help.

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
| `Ctrl+C` | all | quit |
| `q` | screens without a focused text input | quit |
| `Tab` | inspection | open admin |
| `↑` / `k` | lists (not text fields) | move selection up |
| `↓` / `j` | lists (not text fields) | move selection down |
| `Enter` | repositories / tags | open the next detail |
| `Esc`, `Backspace` | detail views | go back |
| `b` | manifest | view blobs |
| `u` | manifest | view uploads |
| `d`, `x` | manifest / blobs / uploads | show unsupported mutation notice |
| `d` | tags | delete the selected tag (confirm modal); `UNSUPPORTED` unless `-delete-enabled`/`REGISTRY_DELETE_ENABLED` |
| `l` | authenticated admin | log out |
| `↑` / `↓` | admin domain menu | move between Browse / Security & Compliance / Identity & Access / Operations |
| `Enter` | admin domain menu | open the highlighted domain |
| `Esc` | admin domain menu | leave the admin panel back to Console browsing |
| `r` | admin users | reload users |
| `/` | admin users | enter username search filter |
| `n` | admin users | create user |
| `b` | admin users | jump to Robots screen |
| `g` | admin users / admin robots | open Grant repository access for the highlighted account |
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
| `Tab` / `Shift+Tab` | Grant repository access | switch pane |
| `Up` / `Down` | Grant repository access | move (list or role) |
| `Space` | Grant repository access (repositories) | toggle the highlighted repository |
| `Ctrl+A` | Grant repository access (repositories) | toggle all shown known repositories |
| `1` / `2` / `3` | Grant repository access (role pane) | Reader / Writer / Admin |
| `Enter` | Grant repository access | apply the grants |
| `Esc` | Grant repository access | back (ignored while applying) |
| `↑` / `↓` | Security & Compliance list | move between Trivy / Gitleaks / Signing |
| `Enter` | Security & Compliance list | open the highlighted feature's own screen |
| `Tab` | Trivy | switch between Runtime and Repository Alerts |
| `c` | Trivy Runtime | open the configuration modal |
| `p` | Trivy Runtime | open the scan policy modal |
| `s` | Gitleaks / Signing config | open the configuration modal |
| `n` | Signing config / override editor trusted-key list | add a trusted key |
| `x` | Signing config / override editor trusted-key list | delete the selected trusted key (usage-count advisory, never blocking) |
| `n` | Signing config / override editor trusted-identity list (once focused) | add a trusted identity (Tab switches between the regexp and issuer fields, Enter commits) |
| `x` | Signing config / override editor trusted-identity list (once focused) | delete the selected trusted identity (immediate, no advisory) |
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

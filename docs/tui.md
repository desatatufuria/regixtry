# TUI Reference

```bash
regixtry tui                                     # local console, no login
regixtry tui -api-base-url http://127.0.0.1:5000 # adds the admin area (Tab)
regixtry tui -snapshot                           # render the first view once and exit
```

The TUI has two parts:

| Part | Needs | Access control |
| --- | --- | --- |
| **Console** (Repositories, Projects, Tags, manifests) | nothing; reads the local storage root and database in-process | none, see [`docs/security.md`](security.md#tui-local-browsing-is-not-access-controlled--by-design) |
| **Admin** (everything inside the shell below) | `-api-base-url` and an admin login against `/auth/token` | the server's `/admin/v1` API |

Against a container, use `docker exec` ([README](../README.md#the-tui-against-a-container)). Admin screens are unreachable without `-api-base-url`.

## The admin shell

After login the operator lands on the **Overview**. Every admin screen is drawn inside one shell: a header with the breadcrumb and `signed in as <name>`, a sidebar, the screen body, and a footer of key hints with the mouse indicator (`F2 mouse on`).

```
Admin › Access › Users                                    signed in as operator
────────────────────────────────────────────────────────────────────────────
 regixtry admin    │
    Overview       │  <screen body>
  ACCESS           │
    ▸ Users        │
      Robots       │
  ...              │
────────────────────────────────────────────────────────────────────────────
  ↑↓ move   enter open   / search ...                              F2 mouse on
```

Sidebar groups, as `shellNavGroups` in `internal/tui/shell.go` defines them:

| Group | Item | Opens |
| --- | --- | --- |
| | Overview | [Overview](#overview) |
| ACCESS | Users | [Users](#users) |
| | Robots | [Robots and tokens](#robots-and-tokens) |
| | Repository access | [Repository access](#repository-access) |
| SECURITY | Image scanning | Trivy, [scanning screens](#image-secret-scanning-and-signing-policy) |
| | Secret scanning | Gitleaks |
| | Signing policy | Cosign signature policy |
| OPERATIONS | Scan runs | [Scan runs](#scan-runs-secret-findings-and-scan-history) |
| | Secret findings | the same list, Leaks tab first |
| | Update channel | [Update channel](#update-channel) |

Moving between sidebar items:

- On the Overview, `↑`/`↓` move the sidebar cursor and `Enter` opens the item.
- On any other shell screen, click a sidebar item with the mouse, or press `Esc` to climb back to the Overview.
- Breadcrumbs name the place: `Admin › Access › Users › alice › Tokens`. The Console has its own (`Console › Projects`), and the grant screen and the repository administrator's screen have no admin sidebar.

## Key conventions

| Key | Behavior |
| --- | --- |
| `Ctrl+C` | quits from anywhere |
| `q` | quits on every list and detail screen, and the footer shows `q quit` there; while a text input, modal, confirm dialog, form, search or editor holds focus it is typed or ignored instead |
| `Esc` | goes back one level, closes a dialog, form or search; on the Overview it returns to the Console |
| `↑` / `↓` | move the selection; `j` / `k` also move in lists that have no text input |
| `Tab` / `Shift+Tab` | next / previous field, pane or tab |
| `l` | log out from a list screen with no dialog or input open |
| `F2` | switch the mouse on or off |
| `c` / `v` | on a [one-time secret](#one-time-secrets) screen: `c` copies the whole secret to the clipboard, `v` shows it alone on its own row |

- `q` per screen: Overview, Users, profile, Robots, Tokens, Repository access, Image/Secret scanning and Signing policy (and their repository lists), Scan runs, Secret findings, scan history, Update channel and the delegate screen quit on `q`, but not while a modal, editor, confirm, search or form is open. The login, New user, Reset password, New robot and **Grant access** screens never quit on `q`: it is typed into the focused field (Grant access filters hold focus from the start, and its footer shows no `q quit`).
- Text inputs (login, search and filter boxes, create forms, grant filters, the signing key and identity fields) take every letter, including `j`, `k` and `q`. Use the arrows and `Tab` to leave the field; `Backspace` deletes.
- Pasting works in text fields. A bracketed paste arrives as text, so pasted key names such as "enter" stay text and never trigger a command.
- Footers are generated from each screen's own key bindings, so a hint cannot disagree with the key it names. The tables below list those hints.
- Anything about time is hours, days or years, never seconds.

## Mouse

On by default; `F2` toggles it and the footer shows `F2 mouse on` / `F2 mouse off`. When it is off the terminal's own text selection works.

- The wheel moves the selection like the arrow keys; a click selects a row.
- A second click on the selected row does what `Enter` does only when that is harmless: opening a profile, a tag list or an editor. It never opens a browser (scan history advisories) and never acts on data.
- The mouse never confirms or cancels a dialog and never triggers a destructive action; deleting, revoking and disabling stay keys.
- While a dialog, a form or a text input is in front, or a change is in flight, the mouse does nothing, the sidebar included.
- The forms (New user, Reset password, New robot), the user profile and Grant access take no mouse input at all. The configuration screens and Update channel have no list, so the mouse only reaches their sidebar.

## Confirm dialog

Every destructive or access-changing action opens the same dialog: a title, what will happen, a box naming the exact target, the consequence, and the buttons.

| Key | Effect |
| --- | --- |
| `y` or `Enter` | confirm; further confirm presses are ignored until the result arrives, so a double press runs the action once |
| `Esc` | cancel |

Any other key, `n` included, is swallowed and does nothing. The mouse never answers it.

## Too-small terminal

Under **150x24** every screen is replaced by a centered notice, `Terminal too small`, stating the minimum and the current size. Resize, or `q` to quit. The notice shrinks its own decoration down to a clipped single line in tiny terminals.

## Login

`Tab` from the Console opens the login (username and password; the password is masked and never logged). `Tab`/`↑`/`↓` switch fields, `Enter` signs in, `Esc` returns to the Console. An expired session returns here with the reason.

## Overview

`Admin › Overview`. Three quick actions and two panels:

| Part | Content |
| --- | --- |
| Quick actions | `g` grant repository access, `r` create robot, `t` issue token (opens Users to pick the person first, because tokens belong to one account) |
| NEEDS ATTENTION | tokens expiring within 7 days (count and the soonest owner), robots without a repository grant (`g grant`), and the number of images with critical findings (latest scan per repository) |
| RECENT CHANGES | the newest entries of the access-events log, as many as fit: grants, tokens, robots and user changes with who did it and how long ago |

| Key | Action |
| --- | --- |
| `↑` / `↓` | move the sidebar cursor |
| `Enter` | open the sidebar item, or the attention row once selected |
| `Tab` / `Shift+Tab` | cycle the selection over the attention rows |
| `g` `r` `t` | quick actions |
| `Esc` | back to the Console |
| `q` | quit |

Each panel loads on its own; one failing read shows an error inside its panel and leaves the other intact.

## Users

`Admin › Access › Users`: a searchable list (username, role, state, count of tokens expiring within 7 days) with the selected user's actions and attention items on the right.

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k`, `Enter` | move, open the profile |
| `/` | username-contains filter; typing filters live, `↑`/`↓` pick, `Enter` keeps the filter, `Esc` ends it |
| `n` | New user form |
| `g` | [Grant repository access](#grant-repository-access) with this user selected |
| `t` | the user's tokens |
| `p` | Reset password |
| `d` | disable (or enable, for a disabled user) after a confirm dialog; refused for your own account |
| `r` | reload |
| `Esc` | Overview |

- **Profile** (`Users › name`): profile, tokens with time left, password and repository grants panels, and a danger area. `g` grant access, `t` tokens, `p` password, `d` or `Space` disable/enable, `r` refresh, `Esc` back. Grants are not edited here; use the grant screen or [Repository access](#repository-access).
- **New user** (`Tab`/`↑`/`↓` fields, `←`/`→` role, `Space` toggles the role and `Enabled`, `Enter` creates, `Esc` cancels): username, masked password (at least 8 characters), role `admin`, `read-only` or `user`. A read-only user pulls from every repository without a grant and has no admin access. The flag is set at creation.
- **Reset password** (two masked fields, `Enter` saves, `Esc` cancels): the password is never shown or logged.

## Robots and tokens

`Admin › Access › Robots` lists robot accounts (repository, role, state, the token that ends first).

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k` | move |
| `Enter` or `t` | the robot's tokens |
| `n` | New robot form |
| `g` | grant access to this robot |
| `d` | disable (confirm); only on an enabled robot |
| `e` | enable (confirm); only on a disabled robot |
| `x` | **delete** (confirm): removes the robot with its repository access and all its tokens; anything using its tokens stops at once; cannot be undone |
| `r` | refresh |
| `Esc` | Overview |

`x` deletes on this screen. Everywhere else it revokes.

**New robot** collects name, repository (type to filter, `↑`/`↓` pick a suggestion), role (`←`/`→`: Reader, Writer, Admin) and a [lifetime](#token-lifetime), then shows the robot's token secret once.

**Tokens** (`Users › name › Tokens`, or `Robots › name › Tokens`): each token as a card with a bar for the time left out of its full lifetime.

| Key | Action |
| --- | --- |
| `n` or `Tab` | open the issue form: Name (optional), Lifetime (`←`/`→`), `Enter` creates |
| `x` | revoke the selected token (confirm); anything using it stops at once |
| `g` | grant access to this account |
| `r` | refresh |
| `↑` `↓` / `j` `k` | move |
| `Esc` | back to the profile or the robot list |

After creating a token or robot the **secret is shown once**, with its accessor and expiry; it cannot be displayed again. `Enter` or `Esc` closes it. `Esc` leaves the screen even while the create call is in flight; the result, secret included, is still shown when it arrives.

#### One-time secrets

Selecting the secret with the mouse across the shell also copies the sidebar and borders drawn on the same rows, so the secret screens offer two cleaner ways. The footer shows `c copy`, `v show alone`, `enter done`.

- `c` copies the whole secret to the system clipboard with an OSC 52 command and confirms in one line (`Copied. If nothing pastes, your terminal may not support OSC 52: press v to select it by hand.`). The secret is held only by that command until it runs; nothing about it is kept once you press `Enter` or `Esc`. Only keys copy: the mouse never does.
- `v` toggles a bare screen: a title row, the secret alone on one row starting at column 0 (split into rows of exactly the terminal width only when the terminal is narrower than the secret, with a note that `c` copies it whole), the accessor, expiry and name each on their own row below, and one hint row at the bottom. In most terminals a triple-click on the secret row should select just the secret (turn the mouse off with `F2` first so the terminal's own selection works; not verified in a real terminal). The hint row at the bottom also carries the `F2` mouse indicator. `c`, `Enter` and `Esc` work there as on the normal screen.

OSC 52 needs terminal support, and the terminal gives no answer when it ignores the request. The terminals commonly reported to support it are iTerm2, kitty, WezTerm, Windows Terminal, foot and some versions of Alacritty (according to their documentation; not verified here). Some terminals ask for permission or disable it by default. Under tmux, `set -g set-clipboard on` in `~/.tmux.conf` should be needed for the request to pass through; over SSH the sequence should travel through the connection to your local terminal, if that terminal supports it. The `Copied.` notice only says the request was sent: it cannot confirm that the terminal honored it. When nothing pastes, use `v`.

### Token lifetime

Create Token and New robot share one **Lifetime** selector. It starts on 30 days; nothing is sent until `Enter`.

| Option | Lifetime |
| --- | --- |
| 7 days, 30 days, 90 days | as named |
| 1 year, 2 years | 365 and 730 days |
| Custom | a value you type |

- The server maximum is **2 years**; nothing longer is offered. The minimum is 1 hour.
- Lifetimes are always shown in hours, days or years, never seconds.
- In **Custom**, type a whole number and an optional unit: `h`, `d` or `y` (no unit means days), for example `12h`, `30d`, `2y`. Other letters are ignored. A pasted value is used only if it is a complete valid lifetime.
- A preview line shows the exact expiry in UTC; an invalid value replaces it with a message and `Enter` sends nothing.
- A warning reminds you that a leaked token stays valid until it expires or you revoke it.

The API accepts the same format as `ttl` ([API reference](api.md)).

## Grant repository access

One screen to give a user or robot a role on one or more repositories. It has no sidebar. Open it with `g` from the Overview, Users, a profile, Robots, Tokens or [Repository access](#repository-access) (and from a robot's attention row); `Esc` returns to the screen it came from.

| Pane | What it does |
| --- | --- |
| **1 Who** | filter plus a list of users and robots; "Already has" shows the chosen account's grants |
| **2 Which repositories** | filter plus a checklist of known repositories, each with the role the account already has; exact names only |
| **3 Role and review** | `1` Reader (pull), `2` Writer (pull, push, delete tags), `3` Admin (Writer plus manage who has access); Reader is the default; the box shows the outcome per repository |

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | switch pane |
| `↑` / `↓` | move in the list or the role |
| `Space` | choose the account (Who) / tick the repository (Which) |
| `Ctrl+A` | tick all shown known repositories, or untick them when all are ticked |
| `1` `2` `3` | pick the role |
| `Enter` | in Who, choose the highlighted account if it is not chosen yet; otherwise apply |
| `Esc` | back |

- Filters take plain text, `j`, `k` and `q` included, and ticked repositories stay ticked while you filter.
- A valid name not pushed yet appears as `+ name` ("not pushed yet"); only `Space` on that row ticks it, so `Ctrl+A` never selects it.
- Nothing is sent before `Enter`. Each repository gets its own grant; a failure does not stop the rest and nothing is rolled back. The review box lists each repository as `granted` or with its error, and succeeded ones are unticked.
- While grants apply, every key but `Ctrl+C` is ignored and `Esc` only reports "Applying N grants, please wait".
- An expired session returns to the login; the remaining repositories are reported "not attempted (session expired)".

## Repository access

`Admin › Access › Repository access`: every grant of every repository, a grant per row (repository, principal, role, since), with who-has-what and the actions for the selected grant on the right. It reads **one paged endpoint**, `GET /admin/v1/repository-grants`, 200 grants per page, following its cursor: the first page on open, the next when the selection nears the end of what is loaded or a filter needs more matches. Repositories that were never pushed appear too.

| Key | Action |
| --- | --- |
| `↑` / `↓` | move (loads the next page when needed) |
| `/` | filter by repository or principal; `Enter` keeps it, `Esc` ends it |
| `Enter` | open the principal: a user's profile, or the Robots list |
| `g` | grant access |
| `x` | revoke the selected grant (confirm); the principal loses that role on that repository |
| `r` | retry the page that failed |
| `Esc` | Overview |

## Delegating access (repository administrators)

A user with `repo-admin` on a repository, who is not a global admin, manages that repository's grants without the global workspace. From the Console Repositories screen press `g` on the repository (login first if needed). The screen is `regixtry › Repository access › <repo>`, with a sidebar that shows only "Repository access" and a note that global settings belong to global admins.

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k`, `PgUp`, `PgDn` | move |
| `n` | New grant: Username (free text) and Role (`Space` cycles Reader, Writer) |
| `e` | edit the selected grant's role; refused for an Admin grant, which only a global admin can change |
| `x` | revoke the selected grant (confirm) |
| `r` | retry, only after a failed read |
| `Tab` / `Enter` / `Esc` in the form | next field / save (ignored while saving) / cancel |
| `Esc` | back to the Console |

The repository is fixed context; the screen never offers `repo-admin`, whether for a new grant or for yourself, and it never shows another repository's grants. Reads and writes use `/admin/v1/repositories/{repo}/grants` ([users.md](users.md#grants-delegated-repo-admin-flow)).

## Image, secret scanning and signing policy

Three feature screens under SECURITY, each with a configuration page and per-repository overrides.

| Screen | Configuration keys | Overrides |
| --- | --- | --- |
| **Image scanning** (Trivy) | `c` configuration modal, `p` scan policy modal | tabs `Config` and `Repositories` (`Tab`, or `o` on Config) |
| **Secret scanning** (Gitleaks) | `c` configuration modal | `o` opens the repository list |
| **Signing policy** | `p` policy modal | `o` opens the repository list |

Common keys on the configuration page: `r` refresh, `Esc` back to the Overview. `e` enable, `x` disable, `i` install runtime, `u` upgrade runtime and `b` rollback runtime appear only when the backend declares that action for the page, and the confirmation copy of those actions is backend-authored. Modals: `Tab` next field, `Space` toggles or cycles, `Enter` saves, `Esc` cancels.

**Repository lists** (Image scanning `Repositories` tab; the Secret scanning and Signing override lists): one row per repository, with status (`override` or `inherited`).

| Key | Action |
| --- | --- |
| `↑` `↓` | move |
| `o` | edit the override of the highlighted repository |
| `Enter` | (Image scanning only) the repository's scan history |
| `Tab` | (Image scanning only) back to `Config` |
| `r` | refresh |
| `Esc` | Overview |

**Override editors** are opened over the list, bound to the one repository and feature they were opened for. Nothing can be saved before the stored override has been read; saving sends the whole override. A new override starts from the current global values.

| Feature | Fields |
| --- | --- |
| Image scanning | Scanning enabled, Ignore file path, Ignore policy path |
| Secret scanning | Scanning enabled, Config path |
| Signing policy | Pull-time gate enforced, trusted keys, trusted identities, Unsigned self-read |

Every editor ends with **Clear override, use the global settings**, which asks `y` / `Enter` to confirm and `Esc` to cancel. `Tab` moves between fields, `Space` toggles, `Enter` saves.

**Signing lists.** Trusted public keys show as short fingerprints, never raw key bytes: `n` adds one (paste the PEM, `Enter` commits), `x` deletes the selected key after a usage advisory ("This key currently verifies N tagged image(s)... Delete it anyway?") that never blocks. Trusted identities (keyless, Fulcio) show a SAN regexp and OIDC issuer: `n` adds one (`Tab` switches fields, `Enter` commits and validates), `x` deletes immediately. In the override editor the key list claims `n`/`x`/`↑`/`↓` from any field, the identity list only once `Tab` focused it.

## Scan runs, secret findings and scan history

`Scan runs` and `Secret findings` list repositories with their latest scan (`↑`/`↓`, `Enter` scan history, `r` refresh, `Esc` Overview). Both open the same **scan history**, a full screen inside the shell, on a different tab: Vulnerabilities for Scan runs, Leaks for Secret findings. Image scanning `Repositories` opens it on Vulnerabilities.

| Key | Action |
| --- | --- |
| `←` / `→` | previous / next run |
| `Tab` / `Shift+Tab` | switch Vulnerabilities and Leaks |
| `↑` / `↓` | move the finding |
| `Enter` | open the finding's advisory link (Vulnerabilities only) |
| `Esc` | back to the screen that opened it |

## Update channel

`Admin › Operations › Update channel`: choose which released tags the background update check follows, plus the current version and whether a newer release is known.

| Key | Action |
| --- | --- |
| `Space` | toggle `stable` / `insider` (unsaved until saved) |
| `Enter` | save the selected channel (does nothing when unchanged) |
| `r` | refresh |
| `Esc` | Overview |
| `q` | quit |

## Console

Reached at start; no login. `Tab` opens the admin area (needs `-api-base-url`). The Console has no sidebar.

| Screen | Keys |
| --- | --- |
| **Repositories** | `↑` `↓` / `j` `k`, `PgUp`/`PgDn`, `Home`/`End` move; `Enter` tags; `p` projects; `g` delegate access ([above](#delegating-access-repository-administrators)); `d` delete repository; `Tab` admin; `Esc` show all (inside a project); `q` quit |
| **Projects** (the first path segment of a repository name) | move; `Enter` its repositories; `d` delete project; `s` sort (name, date); `Tab`; `Esc` back; `q` |
| **Tags** | move; `Enter` manifest; `d` delete tag; `s` sort (name, date); `Tab`; `Esc` back; `q` |
| **Manifest, blobs, uploads** | `b` blobs, `u` uploads, `Esc`/`Backspace` back; `d`, `x` show "Delete unavailable" |

Deletes ask the confirm dialog and need the server flag `-delete-enabled` / `REGISTRY_DELETE_ENABLED`; without it they fail with "... deletion is not enabled".

| Delete | What it does |
| --- | --- |
| Tag | removes the tag from the repository; the manifest stays, untagged, with its signatures and referrers; pulls by that tag stop, pulls by digest keep working |
| Repository | removes it with all its tags and manifests in one transaction; pulls by tag and digest stop at once |
| Project | deletes each of its repositories on its own; if one fails the others are still removed |

Blob files stay until [garbage collection](registry.md#blob-garbage-collection) in every case, and none of these can be undone.

## Geometry

At every supported size (150x24, 151x25, 160x30, 200x40, 220x60) no rendered line is wider than the terminal and no screen has more rows than its height, in every state: empty, loading, error, long names, many rows and multibyte names. Scrolling lists keep the selection visible. Colors come only from the theme roles in `admin_theme.go`.

## Admin endpoints the TUI uses

All under `/admin/v1`, details in the [API reference](api.md).

| Screen | Endpoints |
| --- | --- |
| Overview | `GET admin-tokens/expiring`, `GET access-events`, `GET robots`, `GET repository-scan-summaries` |
| Users, tokens | `GET users`, `admin-tokens/expiring`, user and token endpoints; `ttl` on create |
| Repository access | `GET repository-grants` (paged) |
| Grant access | `GET users`, `GET robots`, `GET` and `PUT users/{id}/grants/{repository}` |
| Delegate | `repositories/{repo}/grants` |
| Security, Operations | `features/...`, `scan-runs`, `signing-policy`, `update-channel` |

Developing a screen: see [`docs/architecture.md`](architecture.md#developing-a-screen).

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

The console shows repositories, tags, manifests, blobs, and uploads. When `-api-base-url` is configured, the admin area can authenticate against `/auth/token` and load users, grants, admin tokens, and the backend-driven feature manager.

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
| `Tab` | Trivy feature page | switch between `Runtime` and `Repository Alerts` |
| `c` | Trivy runtime tab | open the configuration modal for current Trivy settings |
| `↑` / `↓` | Trivy repository alerts tab | move between loaded scan runs |
| `Enter` | Trivy repository alerts tab | open same-screen detail for the selected scan run |
| `Esc` | Trivy repository alert detail | close detail and return to the alert list |
| `e`, `x` | admin feature page | run declared enable / disable when present |
| `i`, `u`, `b` | admin feature page | run declared install / upgrade / rollback when present |
| `g` | admin user detail | load grants |
| `t` | admin user detail | load tokens |

The admin login flow keeps credentials in memory only, exchanges them for a Bearer token through `/auth/token`, and clears the session when the token expires.

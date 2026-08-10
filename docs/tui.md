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
- Trivy currently exposes richer sections for configuration, runtime, recent runs, vulnerability summary, and repository alerts.
- The shell only handles selection, refresh, confirmation, and operator feedback.

### Declared action behavior

- `Enter` or `r` refreshes the selected feature page.
- `e`, `x`, `i`, `u`, and `b` only appear in help when the backend declares `enable`, `disable`, `install-runtime`, `upgrade-runtime`, or `rollback-runtime` for the selected page.
- Confirmation copy for destructive actions is backend-authored.

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
| `e`, `x` | admin feature page | run declared enable / disable when present |
| `i`, `u`, `b` | admin feature page | run declared install / upgrade / rollback when present |
| `g` | admin user detail | load grants |
| `t` | admin user detail | load tokens |

The admin login flow keeps credentials in memory only, exchanges them for a Bearer token through `/auth/token`, and clears the session when the token expires.

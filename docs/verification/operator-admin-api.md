# Verification: operator admin API

This verification guide covers the shipped `/admin/v1` operator administration slice.

## Quick path

1. Run repository-wide formatting and static checks.
2. Run the full Go suite and coverage pass.
3. Confirm the shipped admin API scope still matches the docs: API-first administration, optional Trivy rescan settings/history/manual triggers, no local shortcuts, and deferred pagination/delete-user/TUI admin flows.

## Verification checklist

- [ ] `gofmt -w .` completes without follow-up formatting changes.
- [ ] `go test ./...` passes for the full repository.
- [ ] `go test -cover ./...` passes and reports per-package coverage.
- [ ] `go vet ./...` passes.
- [ ] `README.md`, `docs/architecture.md`, and `docs/roadmap.md` all describe `/admin/v1` as the shipped admin surface.
- [ ] `README.md`, `docs/api.md`, and `docs/installation.md` describe the optional Trivy rescan setup/admin API slice truthfully.
- [ ] The docs still state that CLI-next and TUI-later must use the authenticated API rather than local mutation shortcuts.
- [ ] Deferred scope stays explicit: pagination, delete-user, broad profile edits, break-glass bootstrap flows over `/admin/v1`, and rich TUI scan management are not claimed as shipped.

## Commands

```bash
gofmt -w .

GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test ./...

GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go test -cover ./...

GOMODCACHE="/tmp/opencode/gomodcache" GOPATH="/tmp/opencode/gopath" GOSUMDB=off go vet ./...
```

## Expected results

| Step | Expected result |
| --- | --- |
| Formatting | Repository stays gofmt-clean. |
| Full test suite | All Go packages pass, including `internal/app/auth`, `internal/infra/auth/postgres`, and `internal/protocol/http`. |
| Coverage run | Coverage is reported successfully for every package; no threshold is enforced for this repository. |
| Vet | No static analysis issues are reported. |
| Docs alignment | Reader-facing docs describe the same shipped `/admin/v1` scope as the OpenSpec proposal, specs, design, and tasks, including the optional Trivy rescan slice. |

## Scope reminder

- Shipped: authenticated `/admin/v1` user, grant, admin-token, and optional Trivy rescan administration.
- Deferred: pagination, delete-user, broad profile edits, and TUI-native scan/admin workflows.
- Forbidden shortcut: any new local auth-state mutation path outside `bootstrap-admin` for initial break-glass setup.

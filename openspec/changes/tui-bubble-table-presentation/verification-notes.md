# Verification Notes: TUI Bubble Table Presentation

## Focused Table Evidence

- Command: `go test ./internal/tui -run 'TestModelFeatureTablesRenderAlignedRowsAndPreserveBackendValues|TestModelTrivyTablesPreserveEmptyStateAndBackendOrdering|TestAdminFindingSeverityStylingScopesOnlyVulnerabilityRows|TestModelAdminFeatureTablesKeepScreenShortcutsAuthoritative|TestModelTrivyRepositoryAlertsLoadSelectDetailAndRecoverEmptyState'`
- Exact result:

```text
ok   regixtry/internal/tui  (cached)
```

## Full Suite Evidence

- Command: `go test ./...`
- Exact result:

```text
ok   regixtry/cmd/regixtry  (cached)
ok   regixtry/internal/app/auth  (cached)
ok   regixtry/internal/app/regixtry  (cached)
ok   regixtry/internal/app/scanning  (cached)
?    regixtry/internal/domain/auth  [no test files]
ok   regixtry/internal/domain/regixtry  (cached)
ok   regixtry/internal/infra/auth/postgres  (cached)
ok   regixtry/internal/infra/install/linux  (cached)
ok   regixtry/internal/infra/install/releases  (cached)
ok   regixtry/internal/infra/metadata/sqlite  (cached)
ok   regixtry/internal/infra/scanning/trivy  (cached)
ok   regixtry/internal/infra/storage/fsblob  (cached)
ok   regixtry/internal/ports  (cached)
ok   regixtry/internal/protocol/http  (cached)
ok   regixtry/internal/tui  (cached)
```

## Scope Notes

- Severity styling is limited to vulnerability findings cells only.
- Feature summaries, generic `rows` sections, and scan-run tables stay neutral.
- Screen-level shortcuts remain authoritative while table highlighting tracks current feature and scan-run selections.
- This apply batch proceeded as a single PR under the maintainer-approved size exception.

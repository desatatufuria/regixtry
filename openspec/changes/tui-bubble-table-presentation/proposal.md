# Proposal: TUI Bubble Table Presentation

## Intent

Improve scanability and selection in admin TUI tabular views by replacing plain-text row rendering with real table presentation, while keeping data flow and backend contracts unchanged.

## Scope

### In Scope
- Replace plain-text row rendering for feature summaries, Trivy scan runs, vulnerability findings, and existing `FeatureSection.Kind == "rows"` sections.
- Add TUI-local table state and selection metadata inside existing admin view state.
- Apply severity-aware coloring only in vulnerability-facing tables.

### Out of Scope
- Backend, DTO, persistence, route, or scan-policy redesign.
- Generic cross-app table framework, new admin workflows, or navigation rewrites.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `operator-admin-tui`: row-oriented admin screens gain structured table presentation and severity-led vulnerability emphasis without changing backend-authoritative behavior.

## Approach

Use a localized table adapter inside `internal/tui`, preferring `github.com/Evertras/bubble-table` unless a concrete implementation blocker forces a similar TUI-local alternative. Map existing DTOs into focused table models, preserve current commands and screen structure, and keep severity styling isolated to vulnerability data.

## Affected Areas

| Area | Impact | Description |
|------|--------|-------------|
| `go.mod`, `go.sum` | Modified | Add table dependency if needed. |
| `internal/tui/admin_views.go` | Modified | Replace plain-text row rendering with table views. |
| `internal/tui/session.go` | Modified | Store presentation-only table state. |
| `internal/tui/model.go` | Modified | Keep selection and refresh flows wired to existing commands. |
| `internal/tui/model_test.go` | Modified | Add strict-TDD coverage for table-focused behavior. |

## Risks

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| Table keys clash with screen shortcuts | Med | Define explicit focus ownership and preserve current global exits. |
| UI slice leaks into backend redesign | Low | Reject DTO/API changes in design and tasks. |
| Severity colors reduce readability | Med | Validate styles against current Lip Gloss theme and empty states. |

## Rollback Plan

Revert TUI table rendering, remove any added dependency, and restore current string-based views without touching backend contracts or persisted data.

## Dependencies

- `github.com/Evertras/bubble-table` preferred; fallback only if a concrete blocker is proven.
- Strict TDD remains mandatory with `go test ./...`; authoritative `strict-tdd.md` location still needs confirmation before apply.

## Success Criteria

- [ ] Targeted admin tables render aligned columns instead of plain joined strings.
- [ ] Vulnerability-facing tables show severity-aware visual emphasis without affecting non-vulnerability lists.
- [ ] Existing admin load/refresh actions keep using current TUI and backend seams only.
- [ ] Result stays reviewable as a TUI-local single-PR slice.

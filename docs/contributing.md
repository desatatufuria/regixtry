# Contributing

Contributors work docs-first, follow GitFlow, and keep review slices aligned with the active OpenSpec tasks.

## Quick path

1. Read `README.md`, `docs/architecture.md`, and the active change under `openspec/changes/`.
2. Confirm the current work unit and branch target before editing.
3. Update code, docs, and `tasks.md` together for the same work unit.

## Workflow rules

| Topic | Rule |
| --- | --- |
| Primary planning source | `openspec/changes/<change>/` artifacts |
| Technical artifact language | English |
| Workflow model | GitFlow with chained review slices when a change exceeds the review budget |
| Documentation habit | Update reader-facing docs in the same work unit that changes behavior or workflow |
| Review budget | Default target is under 400 changed lines per PR slice |

## GitFlow branches

- `main`: release history and repository bootstrap baseline.
- `develop`: integration branch for ongoing product work.
- `feature/registry-foundation`: tracker branch for the chained `registry-foundation` work.

## Branch and PR order for `registry-foundation`

1. **PR 0 / bootstrap**: first repository commit lands on `main`.
2. Create `develop` from `main`.
3. Create `feature/registry-foundation` from `develop`.
4. **PR 1** targets `feature/registry-foundation` with the first reviewable slice.
5. **PR 2** targets the PR 1 branch.
6. **PR 3** targets the PR 2 branch.
7. After the child slices merge, open the tracker PR from `feature/registry-foundation` into `develop`.
8. Merge `develop` into `main` only through the normal GitFlow release path.

## Documentation expectations

- Keep `README.md` aligned with v1 scope and explicit non-goals.
- Keep `docs/roadmap.md` split between v1 work and post-v1 candidates.
- Keep `docs/glossary.md` explicit about registry-versus-Docker boundaries.
- Mark completed OpenSpec tasks in `openspec/changes/<change>/tasks.md` as part of the same work unit.

## Pull request checklist

- [ ] Scope matches one work unit.
- [ ] Branch target matches the documented chain order.
- [ ] Reader-facing docs were updated when scope or workflow changed.
- [ ] `tasks.md` reflects completed work with `[x]` checkboxes.
- [ ] Out-of-scope ideas stayed out of the diff.

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
| Scope discipline | Do not pull post-v1 ideas into an active v1 slice without updating the approved change artifacts first |

## GitFlow branches

- `main`: release history and repository bootstrap baseline.
- `develop`: integration branch for ongoing product work.
- `feature/registry-foundation`: tracker branch for the chained `registry-foundation` work.

## Feature-branch-chain rule

This change does NOT use independent branches targeting `main`.

- PR 1 targets `feature/registry-foundation`.
- PR 2 targets the PR 1 branch.
- PR 3 targets the PR 2 branch.
- The tracker branch remains the aggregation point until the full feature is ready for `develop`.
- If a child PR diff shows unrelated earlier slices, retarget or rebase until the review scope is clean.

## Branch and PR order for `registry-foundation`

1. **PR 0 / bootstrap**: first repository commit lands on `main`.
2. Create `develop` from `main`.
3. Create `feature/registry-foundation` from `develop`.
4. **PR 1** targets `feature/registry-foundation` with the first reviewable slice.
5. **PR 2** targets the PR 1 branch.
6. **PR 3** targets the PR 2 branch.
7. After the child slices merge, open the tracker PR from `feature/registry-foundation` into `develop`.
8. Merge `develop` into `main` only through the normal GitFlow release path.

## Work unit expectations

Each PR slice should tell one reviewable story.

- Keep docs with the user-visible or workflow-facing change they explain.
- Do not split a single behavioral unit into unrelated file-type commits.
- Keep rollback clean: a reviewer should be able to revert the slice without losing unrelated work.
- If a task forecast already recommends chaining, do not collapse the slices back into one oversized PR.

## Documentation expectations

- Keep `README.md` aligned with v1 scope and explicit non-goals.
- Keep `docs/roadmap.md` split between v1 work and post-v1 candidates.
- Keep `docs/glossary.md` explicit about registry-versus-Docker boundaries.
- Keep `docs/architecture.md` aligned with the approved layer boundaries and service/TUI separation.
- Mark completed OpenSpec tasks in `openspec/changes/<change>/tasks.md` as part of the same work unit.

## Release installer expectations

- GitHub Releases are the source of truth for the installer path.
- Release assets must keep the `regixtry_<version>_linux_<arch>.tar.gz` and `regixtry_<version>_checksums.txt` contract in sync with `install.sh`.
- Checksums are mandatory; if asset naming or checksum publication changes, update the installer, `README.md`, `docs/verification/scripts/install-release-smoke.sh`, and the active OpenSpec tasks in the same slice.
- Tag-driven release automation must fail if the expected archive or checksum assets are missing.

## Documentation update triggers

Update reader-facing documentation in the same slice when you change any of the following:

- V1 scope or non-goals.
- API or protocol boundary wording.
- Branching or PR targeting rules.
- Architecture seams or ownership boundaries.
- Roadmap sequencing between v1 and post-v1 work.

## Pull request checklist

- [ ] Scope matches one work unit.
- [ ] Branch target matches the documented chain order.
- [ ] Reader-facing docs were updated when scope or workflow changed.
- [ ] `tasks.md` reflects completed work with `[x]` checkboxes.
- [ ] Out-of-scope ideas stayed out of the diff.

## Before opening a PR slice

1. Re-read the active task unit in `openspec/changes/registry-foundation/tasks.md`.
2. Confirm the branch target matches the feature-branch-chain order.
3. Verify the docs still describe the same scope as proposal, design, and specs.
4. Keep the diff focused enough that a reviewer can validate it without reconstructing later work.

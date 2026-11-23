# Release notes: projects

Draft. Single source for an operator upgrading to the release that adds projects. Detail lives in [operations.md](operations.md#upgrade-to-the-projects-release), [users.md](users.md#projects-and-project-roles), [security.md](security.md#projects-grant-model-and-its-bounds) and [api.md](api.md).

## Read first

- **Project enforcement is on by default when auth is enabled (`-auth-postgres-dsn`) and off when anonymous.** A push to a project that does not exist is refused (`404 NAME_UNKNOWN`), and a new repository without a slash is refused (`400 NAME_INVALID`). Existing repositories keep working.
- **The scan-history purge is irreversible.** The first start permanently deletes duplicate scan history (about 12 s per 200k scan runs). Back up `metadata.db` first.
- **Anonymous deployments are not affected.** Projects are created through the admin API, which is only mounted with `-auth-postgres-dsn`, so without it enforcement stays off. An explicit `-require-project=true` without a DSN refuses to start.

## What is new

- Projects: the first path segment of a repository name (`odabank/payments/strike` belongs to `odabank`). Only a global admin creates or deletes them (`/admin/v1/projects`); a project is deleted only when empty and with `-delete-enabled`.
- Project roles: `repo-reader`, `repo-writer`, `repo-admin` on a project cover every repository inside, now and later. The effective role is the highest of the project role and the exact repository grant.
- Delegation: a project admin gives or revokes reader and writer inside its own project. It cannot grant admin, cannot touch another project admin, cannot create or delete projects.
- Robots can hold a project role (reader or writer, one grant per robot) with `project` in `POST /admin/v1/robots`.
- Project roles are matched on the whole first segment, per request. Token scopes stay exact repositories; a project grant is expanded when the token is issued.
- Access events for project grants and project create and delete.
- TUI: `Admin > Access > Projects`, project members, inherited access on Edit user, project scope for new robots, and a folder tree in the Console.
- `serve` logs the effective enforcement mode and why at start.

## Behavior changes

- `-require-project` / `REGISTRY_REQUIRE_PROJECT` default to on when auth is enabled and off otherwise; an explicit value (flag over env) always wins. Opt out with `-require-project=false` or `REGISTRY_REQUIRE_PROJECT=false`; pushes then behave as before.
- New flat repository names are refused while enforcement is on. A flat repository whose first push failed midway cannot be retried.
- Writes to SQLite wait in the write queue instead of returning `SQLITE_BUSY` to the push path.
- Cancelling or reaping an upload no longer waits for a client that stopped sending its body.
- Scan results are latest-only, with the stricter pull policy described in [operations.md](operations.md#upgrade-note-scan-history-purge-irreversible).

## Before upgrading

1. Back up `metadata.db` (stopped cleanly, or with its `-wal` file) and keep it with the `content` directory.
2. Back up the Postgres auth database. The release adds the table `auth_project_grants`; nothing existing changes.
3. Decide on enforcement. To disable: `REGISTRY_REQUIRE_PROJECT=false` in the env file (systemd), or `REGIXTRY_REQUIRE_PROJECT=false` in the Compose `.env` (the shipped compose file forwards it; empty means the default), or `-require-project=false`.
4. With enforcement on, plan which projects new customers need and create them before they push.

The first start creates the `projects` table and, once, one project per first segment of the slashed repositories that exist then (`created_by` `migration`). Flat repositories create no project.

## Rollback

Start the previous binary. It ignores the new tables and does not enforce projects, so pushes work again. It does not honor project roles: users and robots with only a project role lose access until given exact repository grants (fails closed). Data created meanwhile is not backfilled: after re-upgrading with enforcement on, repositories created under a project that has no row answer `NAME_UNKNOWN` until you create the project. The scan-history purge is undone only by restoring the `metadata.db` backup, which also loses everything pushed after it.

## Known limits

- Catalog paging worst case reads every repository once per request for a caller whose access is small.
- A project admin can discover usernames by guessing (`404` for an unknown user), as repository delegates can.
- A grant put while its project is being deleted can leave an orphan grant; the next create of that name purges it, and `DELETE .../members/{username}` removes it.
- The `projects` table (SQLite) and `auth_project_grants` (Postgres) have no foreign key between them; create and delete converge them (delete is idempotent, the grant cleanup is bounded to 15 seconds and a failure is a retryable `503`).
- `members` is not a reserved project name.
- The TUI Console, including its Projects view, is local and not access controlled.
- The backfill runs once: repositories pushed afterwards with enforcement off have no project until one is created.

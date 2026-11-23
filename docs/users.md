# Users

User management runs through `bootstrap-admin` for the first global admin, and through `/admin/v1` (or the TUI's authenticated admin area) for everything after that.

## Create a user

```bash
curl -X POST 'https://registry.example.com/admin/v1/users' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"username":"builder","password":"<PASSWORD>","is_admin":false,"is_read_only":false,"enabled":true}'
```

Usernames are normalized to lowercase. The response never echoes the password hash.

`is_read_only` grants the registry-wide read-only role: the user can pull from every repository without an explicit grant, but never push, and never reaches any admin-surface functionality. It is a ceiling: no repository or project grant raises a read-only user above read, so a writer or admin grant held by a read-only user (for example one made read-only after it was granted) has no effect, and it cannot delete repositories or manage grants. For a user that is both global admin and read-only the read-only flag has no effect (`is_admin` wins and the user keeps full admin access); for anyone else the flag means pull on every repository, catalog listing, and nothing more. To keep the data honest, giving a read-only user an exact repository grant above `repo-reader` (global route, delegate route) is refused with `422` (`read-only users can only be readers`), the same rule project grants already follow; legacy rows that predate it stay in the database and are capped at read when decisions are made. It is independent of `is_admin` — a user can be read-only, admin, both (global admin implies read access anyway), or neither. Robot accounts can be neither admin nor read-only (`User.Validate` rejects that combination); see [Robot accounts](#robot-accounts) below.

## Grants (global-admin flow)

A global admin manages any user's repository grants directly by user ID:

```bash
curl -X PUT 'https://registry.example.com/admin/v1/users/USER_ID/grants/team%2Fimage' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"role":"repo-writer"}'
```

Valid roles are `repo-reader`, `repo-writer`, and `repo-admin`. `GET /admin/v1/users/{id}/grants` lists a user's grants; `DELETE /admin/v1/users/{id}/grants/{repository}` removes one.

## Grants (delegated repo-admin flow)

A user who holds `repo-admin` on a specific repository — but is not a global admin — can manage grants scoped to that one repository, without going through the global admin surface:

```bash
curl -X PUT 'https://registry.example.com/admin/v1/repositories/team%2Fimage/grants/someuser' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"role":"repo-writer"}'
```

```bash
curl 'https://registry.example.com/admin/v1/repositories/team%2Fimage/grants' \
  -H "Authorization: Bearer ${TOKEN}"

curl -X DELETE 'https://registry.example.com/admin/v1/repositories/team%2Fimage/grants/someuser' \
  -H "Authorization: Bearer ${TOKEN}"
```

Note the delegate flow addresses users by **username**, not user ID — a delegate never needs directory access to name a grant target.

**Escalation bounds** (enforced in `Service.PutRepositoryGrant`/`DeleteRepositoryGrant`, `internal/app/auth/service.go`): a repo-admin delegate can grant or revoke `repo-reader`/`repo-writer` on their repository, but can never:

- Grant or self-assign `repo-admin` on that repository — the request is rejected outright.
- Modify or remove another repo-admin's grant on that same repository — a delegate cannot demote or replace a peer administrator.

Only a global admin can perform those two operations. The TUI exposes the same repository-scoped view as one screen, reached with `g` from the Console Repositories screen (see [Delegating access](tui.md#delegating-access-repository-administrators)); it never offers `repo-admin`, matching the HTTP-layer bound. See [security.md](security.md) for the full authorization boundary this delegate namespace sits behind.

## Projects and project roles

A project is the first path segment of a repository name (`odabank/payments/strike` belongs to `odabank`). Only a global admin creates (`POST /admin/v1/projects`) or deletes a project; a project is deleted only when it holds no repository and no open upload, and only when the server runs with `-delete-enabled`. There is no rename.

A project role gives one user or robot one role over every repository of the project, now and later. It is a project membership: the API calls it a member (`/admin/v1/projects/{project}/members/{username}`) and the TUI names the roles `Project reader`, `Project writer` and `Project admin`.

| Role (API) | TUI name | Can do inside the project |
| --- | --- | --- |
| `repo-reader` | Project reader | pull every repository |
| `repo-writer` | Project writer | pull and push; delete tags and manifests when deletion is enabled (same actions as an exact `repo-writer` grant) |
| `repo-admin` | Project admin | everything a writer can, and manage the project's members (below) |

The effective role on a repository is the highest of the project role and the exact repository grant: an exact grant can only add access, and revoking a project role removes only the access it gave. Matching is on the whole first segment, so `odabank` never covers `odabank2/...` nor a flat repository called `odabank`. Roles apply from the next request; see [authentication.md](authentication.md#project-grants).

```bash
curl -X POST 'https://registry.example.com/admin/v1/projects' \
  -H "Authorization: Bearer ${TOKEN}" -d '{"name":"odabank"}'

curl -X PUT 'https://registry.example.com/admin/v1/projects/odabank/members/ana' \
  -H "Authorization: Bearer ${TOKEN}" -d '{"role":"repo-writer"}'
```

Request and response shapes: [api.md](api.md). `GET /admin/v1/users/{id}/project-grants` lists one user's project roles (global admin only).

**Delegation rules** (enforced server-side; the caller is checked before the project is looked up). A project admin who is not a global admin can:

- list the members of their own project;
- grant or change `repo-reader` and `repo-writer` for users and robots inside their own project, and revoke those.

A project admin cannot:

- grant `repo-admin` (`403`, `project administrators cannot grant project admin`);
- change or revoke another project admin (`403`, `project administrators cannot modify another project administrator's grant`);
- act on another project (`403`, `project administrator privileges are required`; the same answer for a project that does not exist);
- create, list or delete projects, create robots, or use any other admin route (`403`, `admin access is required`).

Only a global admin gives or removes `repo-admin`. A read-only user (`is_read_only`) can only be a project reader: `repo-writer` and `repo-admin` project grants for them are refused with `422` (`read-only users can only be project readers`). Exact repository grants are not refused, but whatever their role they never give a read-only user more than read.

## Robot accounts

Robots are machine identities for CI/CD, created and managed only by a global admin — there is no delegated robot creation.

### Create a robot

```bash
curl -X POST 'https://registry.example.com/admin/v1/robots' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-builder","repository":"team/image","role":"repo-writer","ttl":"30d"}'
```

`ttl` takes a whole number followed by `h`, `d` or `y` (maximum `2y`). The older `ttl_seconds` is still accepted, but not together with `ttl`.

Response (`201`):

```json
{
  "robot": {"id":"...","username":"ci-builder","repository":"team/image","role":"repo-writer","enabled":true,"created_at":"..."},
  "secret": "<shown once>",
  "accessor": "act_...",
  "expires_at": "..."
}
```

The `secret` is a pre-issued admin-credential token (`TokenKindAdminCredential`) and is shown **exactly once**, in the create response — it is never retrievable again. Use it the same way you would a human admin token: as the Basic password with `docker login`, or as the token in `LoginWithPreissuedToken`-based tooling.

A robot can instead hold a project role: send `project` in place of `repository` (never both) and a `role` of `repo-reader` or `repo-writer` (a project robot is never `repo-admin`). It then covers every repository of that project, pushed now or later; an unknown project answers `404`. The robot's JSON shows `project` and an empty `repository`.

```bash
curl -X POST 'https://registry.example.com/admin/v1/robots' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-odabank","project":"odabank","role":"repo-writer","ttl":"30d"}'
```

A robot holds exactly one grant, whichever kind: giving a robot a project role while it has a repository grant (or the reverse) is refused with `409`, from either route.

A robot is bound to exactly one `(repository, role)` pair at creation — a product convention enforced in `Service.CreateRobot`, not a schema constraint; the underlying `auth_repo_grants` row is keyed the same as any user's grant.

### List and delete

```bash
curl 'https://registry.example.com/admin/v1/robots' \
  -H "Authorization: Bearer ${TOKEN}"

curl -X DELETE 'https://registry.example.com/admin/v1/robots/ROBOT_ID' \
  -H "Authorization: Bearer ${TOKEN}"
```

Robots are excluded from `GET /admin/v1/users` by default (`ListUsers` filters `WHERE is_robot = FALSE` in Postgres) — use `GET /admin/v1/robots` to see them.

`DELETE /admin/v1/robots/{id}` is a **real hard delete**, unlike human users, which are only ever enabled or disabled and never removed from the store. The handler rejects any target where `!IsRobot`, so this endpoint structurally cannot be used to delete a human account even by ID guesswork. Deleting a robot relies on `ON DELETE CASCADE` on the `auth_repo_grants`, `auth_project_grants` and `auth_tokens` foreign keys (`internal/infra/auth/postgres/migrations.go`) to automatically clean up its grant and any outstanding tokens.

### TUI

Global admins manage robots under **Admin › Access › Robots**: create (`n`), tokens (`t`), grant access (`g`), disable/enable (`d`/`e`) and delete (`x`, irreversible, with a confirm dialog). New robots collect name, scope (repository or project), repository or project, role and a lifetime, and the token secret is shown once. Keys and behavior: [Robots and tokens](tui.md#robots-and-tokens).

## Admin credential tokens (human users)

```bash
curl -X POST 'https://registry.example.com/admin/v1/users/USER_ID/admin-tokens' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci","ttl":"30d"}'
```

`ttl` takes a whole number followed by `h`, `d` or `y` (maximum `2y`). The older `ttl_seconds` is still accepted, but not together with `ttl`.

The secret is only returned in the creation response and should never be logged. To revoke:

```bash
curl -X DELETE 'https://registry.example.com/admin/v1/users/USER_ID/admin-tokens/ACCESSOR' \
  -H "Authorization: Bearer ${TOKEN}"
```

## Limitations

The last active global admin account can never be disabled or demoted — `Service.SetUserEnabled`/`UpdateUser` reject any operation that would leave zero active global admins. There is no delete-user endpoint for human users; disable instead.

The TUI lists, creates, enables and disables users, resets passwords, manages tokens, grants repository access (global and delegated), creates and deletes projects, gives project roles and creates, lists and deletes robot accounts. The `read-only` role is chosen on the **New user** form only; the user profile screen cannot change it after creation (see [`docs/tui.md`](tui.md#users)). Remaining mutations not yet TUI-capable must still be done over HTTP.

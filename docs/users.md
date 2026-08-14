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

`is_read_only` grants the registry-wide read-only role: the user can pull from every repository without an explicit grant, but never push, and never reaches any admin-surface functionality. It is independent of `is_admin` — a user can be read-only, admin, both (global admin implies read access anyway), or neither. Robot accounts can be neither admin nor read-only (`User.Validate` rejects that combination); see [Robot accounts](#robot-accounts) below.

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

Only a global admin can perform those two operations. This is the same repository-scoped grants view exposed in the TUI as `screenRepoAdminGrants`/`screenRepoAdminAddGrant`, reached directly from the Console Repositories screen by a repo-admin delegate; its role picker never offers `repo-admin`, matching the HTTP-layer bound. See [security.md](security.md) for the full authorization boundary this delegate namespace sits behind.

## Robot accounts

Robots are machine identities for CI/CD, created and managed only by a global admin — there is no delegated robot creation.

### Create a robot

```bash
curl -X POST 'https://registry.example.com/admin/v1/robots' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-builder","repository":"team/image","role":"repo-writer","ttl_seconds":2592000}'
```

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

A robot is bound to exactly one `(repository, role)` pair at creation — a product convention enforced in `Service.CreateRobot`, not a schema constraint; the underlying `auth_repo_grants` row is keyed the same as any user's grant.

### List and delete

```bash
curl 'https://registry.example.com/admin/v1/robots' \
  -H "Authorization: Bearer ${TOKEN}"

curl -X DELETE 'https://registry.example.com/admin/v1/robots/ROBOT_ID' \
  -H "Authorization: Bearer ${TOKEN}"
```

Robots are excluded from `GET /admin/v1/users` by default (`ListUsers` filters `WHERE is_robot = FALSE` in Postgres) — use `GET /admin/v1/robots` to see them.

`DELETE /admin/v1/robots/{id}` is a **real hard delete**, unlike human users, which are only ever enabled or disabled and never removed from the store. The handler rejects any target where `!IsRobot`, so this endpoint structurally cannot be used to delete a human account even by ID guesswork. Deleting a robot relies on `ON DELETE CASCADE` on the `auth_repo_grants` and `auth_tokens` foreign keys (`internal/infra/auth/postgres/migrations.go`) to automatically clean up its grant and any outstanding tokens.

### TUI

Global admins reach two dedicated robot screens from the Users screen with `b`:

- `screenAdminRobots` — lists robot accounts (repository, role, enabled state). `n` opens `screenAdminCreateRobot`; `e`/`x` enable/disable the selected robot via the confirm modal; `t` reuses the existing token screens to issue or revoke its token; `d` deletes it (genuinely irreversible, distinct from `x`/disable); `r` refreshes the list.
- `screenAdminCreateRobot` — collects name, repository, role, and TTL, then shows the created robot's one-time secret exactly once, immediately after creation. This is the same reveal-once pattern already used for human admin tokens.

## Admin credential tokens (human users)

```bash
curl -X POST 'https://registry.example.com/admin/v1/users/USER_ID/admin-tokens' \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci","ttl_seconds":2592000}'
```

The secret is only returned in the creation response and should never be logged. To revoke:

```bash
curl -X DELETE 'https://registry.example.com/admin/v1/users/USER_ID/admin-tokens/ACCESSOR' \
  -H "Authorization: Bearer ${TOKEN}"
```

## Limitations

The last active global admin account can never be disabled or demoted — `Service.SetUserEnabled`/`UpdateUser` reject any operation that would leave zero active global admins. There is no delete-user endpoint for human users; disable instead.

The TUI supports listing users, grants, and tokens, enabling/disabling users, and — as of the access-control-completion work — creating and managing repository grants (including the delegated repo-admin grant screens), and creating/listing/deleting robot accounts. The `Read-only` toggle (beside `Admin`) is set on the **Create User** form only — it is not exposed on the Edit User screen, which shows Username, Role, Status, and User ID and has no path to change it after creation (see [`docs/tui.md`](tui.md)). Remaining mutations not yet TUI-capable must still be done over HTTP.

# Security

## Confirmed by the code

- User passwords are verified with bcrypt.
- Stored tokens and pre-issued credentials keep only a SHA-256 hash of the secret, never the plaintext.
- Access tokens expire after 15 minutes (`TokenKindAccess`); admin credential tokens (`TokenKindAdminCredential`, used for both human admin tokens and robot secrets) last 30 days by default, can be given at most 2 years, and are revocable.
- Repository grants apply the roles `repo-reader`, `repo-writer`, `repo-admin` (`RepoRole.AllowsRead/AllowsWrite/AllowsAdmin`).
- `/admin/v1` requires a valid Bearer token for every route. Almost every route additionally requires `Principal.IsAdmin` — with one deliberate, narrow exception described below.
- TLS termination directly on the binary requires both a certificate and a key together; the configuration rejects an inconsistent combination (one present, one missing).
- The installer validates SHA-256 checksums of release artifacts.

## `/admin/v1` authority: one deliberate exception

`handleAdmin` (`internal/protocol/http/admin_handlers.go`) gates every subpath behind `requireAdminPrincipal` — global admin only — **except** the `repositories/` prefix, which it recognizes and routes *before* that gate is applied. This is intentional and narrow, not a general weakening of `/admin/v1`:

- `GET /admin/v1/repositories/{repo}/grants`
- `PUT /admin/v1/repositories/{repo}/grants/{username}`
- `DELETE /admin/v1/repositories/{repo}/grants/{username}`

These three routes only require an authenticated principal (`requireAuthenticatedPrincipal` — any valid Bearer token). The actual authorization decision is made in the service layer by `requireAdminOrRepoAdmin`, which passes for a global admin or for a principal holding a `repo-admin` grant on that exact repository. Every other route under `/admin/v1` — including every future one added to `handleAdmin`'s switch — stays behind the blanket admin gate by construction; becoming delegate-eligible requires deliberately adding a route to this allow-list.

One implementation detail worth knowing if you're reasoning about this boundary from the domain layer: authorization here reads `actor.Grants` directly rather than calling `Principal.HasRepoAdminAccess`. That method additionally requires a push-scoped token, but TUI/admin-API logins request zero scopes — using it here would make delegation permanently unreachable on exactly the tokens this endpoint is meant to serve.

### Delegated repo-admin: security boundary

A repo-admin delegate is a genuine, bounded privilege escalation *within one repository* — not a scoped-down global admin. What it can do:

- List, grant, and revoke `repo-reader`/`repo-writer` on the one repository it administers.

What it structurally cannot do, enforced server-side (not just hidden in the UI):

- Grant or self-assign `repo-admin` on that repository — rejected outright by `PutRepositoryGrant`.
- Modify or remove another repo-admin's grant on that same repository.
- Reach any other `/admin/v1` route, or manage grants on any repository it does not administer — `requireAdminOrRepoAdmin` re-checks the exact target repository on every call.

Only global `IsAdmin` can grant `repo-admin` or touch another repo-admin's grant. Treat a `repo-admin` grant as "trusted to manage membership of this one repository," not "trusted with the registry."

## Projects: grant model and its bounds

A project is the first path segment of a repository name. A project grant (`auth_project_grants`, Postgres) gives one user or robot one role over every repository of that project, now and later. Every place that matches grants against a repository name honours project grants (the access predicates, the token-scope intersection, the catalog filter, the repository-grant delegate checks); a test fails if a new exact-match site appears. How a grant is matched and evaluated is in [authentication.md](authentication.md#project-grants).

Escalation rules (server-side, not just hidden in the UI):

- Matching is `==` on the whole first segment: `odabank` never covers `odabank2/...` nor a flat repository named `odabank`.
- Effective role is the highest of project and exact grant; exact grants only add. A project grant never widens `IsReadOnly` beyond pull and never grants anything to a robot beyond reader or writer.
- Only a global admin creates or deletes projects, gives `repo-admin` on a project, or touches another project admin. A project admin (`repo-admin` on the project) can list members and give or revoke `repo-reader`/`repo-writer` inside that one project; it cannot create robots or reach any other admin route.
- A read-only user can hold only a project reader grant. A robot holds exactly one grant (repository or project) and never `repo-admin`.

Existence oracle: authorization comes before existence. A push by a caller without push permission, and a member-route call by a caller without rights on the project, get the same answer whether or not the project exists (`401`/`403`); only a caller who is allowed learns `NAME_UNKNOWN` or `404`. Pulls, tags and the catalog never consult the projects table.

Documented limits:

- Catalog paging: a project-scoped caller gets full pages, but the filter reads the repository listing in batches until the page is full, so the worst case (a caller whose projects are small among very many repositories, or with no access at all) reads every repository once per request.
- A project admin can discover whether a username exists by guessing: the answer to a member call distinguishes an unknown user (`404`) from a known one, the same way the repository grant routes do.
- Time-of-check/time-of-use on put-grant: a grant put while the project is being deleted can land just after the grant purge and leave an orphan grant. It is harmless (it matches no repository) and the next create of that project name purges it first; `DELETE /admin/v1/projects/{project}/members/{username}` also removes it.
- The project store (SQLite) and the grant store (Postgres) have no foreign key between them; see [architecture.md](architecture.md#projects-two-stores-and-how-they-converge).
- `members` is not a reserved project name: a project may be called `members`; its routes are `/admin/v1/projects/members/members`, which cannot collide with anything.
- The local TUI Console (including its Projects view) is not access controlled, see below; it is a derived view of the catalog, not the project store.

## Robot accounts

Robot accounts (`User.IsRobot`) are CI/CD machine identities with a narrower attack surface than human accounts by design:

- **Two independent layers block password login.** `Service.LoginWithPassword` rejects any `IsRobot` user before it inspects the password at all; separately, a robot's stored password hash is the `RobotPasswordHash` sentinel, which is not a valid bcrypt hash, so `bcrypt.CompareHashAndPassword` fails on it even if the `IsRobot` guard were ever removed. This is deliberate defense in depth, not redundancy by accident.
- **Admin-only, no delegation.** Creation (`POST /admin/v1/robots`), listing, and deletion (`DELETE /admin/v1/robots/{id}`) all require global `IsAdmin` — a repo-admin delegate cannot create or manage robots even on its own repository.
- **Hard delete, unlike human users.** `DeleteRobot` performs a real row delete, not a disable. The handler rejects any target where `!IsRobot`, so this endpoint cannot be repurposed to hard-delete a human account. Deletion relies on `ON DELETE CASCADE` on the robot's grant and token foreign keys, so no orphaned grant or token survives a robot deletion.
- **One grant, one secret, shown once.** A robot is bound to a single grant at creation, either `(repository, role)` or `(project, role)` with role reader or writer,, and its credential secret is a normal `TokenKindAdminCredential` token — same TTL ceiling, same revocability as a human admin token — returned only in the creation response.

## The registry-wide read-only role

`IsReadOnly` grants pull access to *every* repository with no per-repository grant needed. Its scope is deliberately narrow:

- Never grants push, on any repository — `hasGrantedRepositoryAccess`'s read-only branch is only ever evaluated against `RepoRole.AllowsRead`, so it structurally cannot widen to write or catalog-admin checks even if a caller mistakenly probes with a different predicate.
- Grants no admin-surface access — a read-only user cannot reach `/admin/v1` routes or the delegated repository-grants namespace.
- Is mutually exclusive with `IsRobot` at the domain level (`User.Validate` rejects a robot marked read-only).

## TUI local browsing is not access-controlled — by design

**This is the single most important thing an operator should understand about this system's boundaries.** The TUI's local Console — repository list, tags, manifest inspection, blobs, uploads, signature status — performs **no per-user authorization at all** when talking to the local registry. See [`docs/architecture.md`](architecture.md#3-tuis-dual-localhttp-nature) for how this local channel is wired.

- `tui.QueryService` (`internal/tui/model.go`) — the interface backing `RepositorySummaries`, `TagDetails`, `ResolveManifest`, `Uploads`, and `SignatureStatus` — takes no `Principal` or actor argument on any of its five methods. There is no identity to check access against at that layer.
- `runTUI` (`cmd/regixtry/main.go`) wires this local query path through `localOperatorAccessController{}`, whose `Authorize` method ignores both its arguments and unconditionally returns `nil`.

**Net effect:** anyone with a local TUI session can browse every repository's tags, manifests, blobs, and upload state, regardless of what grants that user actually holds. This is separate from, and does not affect, the TUI's *authenticated admin area* (users, grants, robots, tokens, features), which — when `-api-base-url` is configured — authenticates against `/auth/token` and is subject to the normal `/admin/v1` authorization rules described above.

The real, enforced access-control boundaries in this system are exactly two:

1. **The Docker registry protocol** — `/v2/...` — grant- and scope-checked via `Principal.HasReadAccess`/`HasWriteAccess`.
2. **The admin API** — `/admin/v1/...` — checked via `Principal.IsAdmin` or, for the one delegated namespace above, `requireAdminOrRepoAdmin`.

Local TUI inspection is not, and was never designed to be, a third enforcement boundary. If you need to restrict who can see which repositories, that restriction has to happen at the network/host level — e.g., not exposing local TUI access to users who shouldn't see everything — not by relying on the Console to hide anything.

## Operational recommendations

- Prefer `-password-stdin` over `-password` for `bootstrap-admin`.
- Do not expose PostgreSQL or the local HTTP listener without a controlled network boundary.
- Use direct HTTPS or a TLS-terminating reverse proxy on untrusted networks.
- Protect `regixtry.env`, bootstrap state, the SQLite database, and the blob filesystem.
- Store an admin token's or robot's secret only at creation time — it cannot be retrieved again.
- Treat local TUI access as equivalent to full read access to every repository in the registry; restrict who can reach it accordingly.

These are recommendations, not guarantees enforced by the program.

## Open-upload fairness

The global open-upload cap (`-max-open-uploads`, default 1000) is shared by every pusher, so it alone would let one writer starve the rest. Two bounds close that:

- **Per identity:** one authenticated identity (user or robot id, not the token, so extra tokens do not add quota) holds at most 100 open uploads. The check and the insert of the upload row are one SQL statement through the write queue, so parallel `POST .../blobs/uploads/` calls cannot overshoot either cap. The refusal is `429` `TOOMANYREQUESTS`, the same shape as the global and per-repository refusals. In anonymous mode all callers share one bucket, so a single anonymous client can still use up to 100 slots and the rest of the global cap stays available to authenticated identities.
- **Absolute lifetime:** an upload session expires 72 h after creation. Appending refreshes the idle timestamp (`-upload-ttl`) but never the creation time, so a writer can no longer keep slots alive indefinitely by appending a byte periodically. After 72 h append and complete are refused like an expired upload and the reaper deletes the session even if it was just written to (a write in flight is skipped until it ends). Cancel still works.

Neither value is configurable. The owner is stored in an additive `uploads.owner` column; an older binary ignores it.

## Failed logins and rate limiting

Every rejected credential (`/auth/token`, and bearer tokens on `/v2` and `/admin/v1`) writes one log line `login failed endpoint=<path> ip=<client> reason="<real reason>" account=<name>` and increments an in-process counter by reason. An unknown username is logged as `account_hash=<8 hex>`, never raw, to avoid log injection and unbounded cardinality; passwords and tokens are never logged. The client IP is the TCP peer unless `-trusted-proxies` names the proxies whose `X-Forwarded-For` to trust (see [operations.md](operations.md#reverse-proxy-and-client-ip)). Password checks are bcrypt (cost 10, about 85 ms): their concurrency is capped at `max(2, NumCPU/2)` (`-login-max-concurrency`) so a flood of bad passwords cannot starve `/v2`; excess logins wait at most 250 ms and then get `429` with `Retry-After: 1`. A valid credential token (64 hex) skips bcrypt, so CI robots keep working during a flood. A `-trusted-proxies` range of `/0` is refused at startup because it would trust every peer's `X-Forwarded-For`. There is no IP-based blocking in the registry: keep per-IP volume limits in the reverse proxy. What is kept about failures, and for how long, is in [Login failures: what is recorded and why](#login-failures-what-is-recorded-and-why).

### Password policy

A password must have at least 8 characters (after trimming) and must not be one of the roughly 340 most common passwords embedded in the binary. The match is whole-password (not substring) and ignores case, fullwidth and other compatibility forms (NFKC), zero-width/format characters and all Unicode spaces, so `ｐａｓｓｗｏｒｄ` or `pass\u00a0word` are refused too; this folding is for the match only and the stored password is never altered. The check applies wherever a password is set: `bootstrap-admin`, creating a user and resetting a password; the API answers `422` with `password is too common; choose a less guessable one`. Existing passwords are not re-checked and keep working. There is no forced complexity or rotation.

### Per-account backoff

Consecutive failed password logins are counted per normalized username (trimmed, case-insensitive; unknown usernames are tracked exactly like existing ones, so the behavior reveals nothing about which accounts exist). After 5 failures the next attempt must wait 2 s, then 4 s, 8 s, ... capped at 5 minutes (`-login-backoff-threshold`, `-login-backoff-base`, `-login-backoff-max`). A success, or 15 minutes without failures, resets the counter. State lives in memory (LRU of 10 000 accounts, per process, lost on restart; with several instances each counts separately).

- **Mode `log` (default in this version):** nothing is refused; the registry logs `login backoff would have blocked account=<name> retry_after=2s mode=log` (an unknown username appears as `account_hash=<8 hex>`). The accounts in this state are listed by the Login security screen and `GET /admin/v1/security/login-backoff` as the ones that would wait. Use it to calibrate, then set `-login-backoff-mode=enforce`.
- **Mode `enforce`:** while an account is in backoff the password is not verified, one `login failed ... reason="throttled: account backoff"` line is logged, and the answer is `429` with `Retry-After: <seconds>` and error code `TOOMANYREQUESTS`, identical for existing and unknown usernames. Outside a backoff a failure is the usual uniform `401`.
- `-login-backoff-threshold=0` (or mode `off`) disables it.

Trade-offs to know: an attacker who knows an account name can keep it in backoff, at most 5 minutes at a time (no hard lockout); during that window the account's password is refused with `429`, but credential tokens (robots, CI) are exempt. An administrator clears one account's counter with the reset action ([below](#reset-and-who-can-read)); `regixtry bootstrap-admin` changes the password but does not clear the in-memory counter, which ends with the wait (5 minutes at most), a reset or a restart of the service. Only secrets that are not token-shaped count: a wrong 64-hex secret never counts against the account, so strangers cannot put a robot in backoff. Flooding with many random usernames can evict an account's counter from the LRU: measured, 10,001 random usernames evicted a victim that was in backoff, which buys the attacker about 5 fresh guesses per ~10,000 requests, each of those requests costing a bcrypt whose concurrency is capped by the semaphore. The bcrypt concurrency cap and the proxy's per-IP limits remain the defense against volume. The per-account denial of service is bounded: an account is never blocked longer than 300 s (`-login-backoff-max`) per window, and never hard-locked.

## Login failures: what is recorded and why

Two records exist for a rejected credential. They answer different questions: the log line is the per-attempt trail, the persisted record is a bounded summary an administrator can read without shell access.

**The log line (always).** Every rejected credential on `/auth/token`, and every refused bearer token on `/v2` and `/admin/v1`, writes one line:

```
login failed endpoint=/auth/token ip=203.0.113.7 reason="wrong password or unknown admin credential token" account=alice
login failed endpoint=/auth/token ip=203.0.113.8 reason="unknown user" account_hash=be463d41
login failed endpoint=/auth/token ip=203.0.113.7 reason="throttled: account backoff" account_hash=2bd806c9
login failed endpoint=/admin/v1 ip=198.51.100.4 reason="invalid credentials"
```

A bearer failure has no account field. An enforced backoff refusal writes exactly one such line. Passwords and tokens are never logged. The hash is the first 8 hex digits of the SHA-256 of the trimmed, lowercased username; it is shown so one attacker-chosen name can be correlated without storing it. It is not a secret: anyone can check a candidate name against it (`printf %s ghost-raw | sha256sum | cut -c1-8`), so do not read it as anonymization of a guessable name.

**The aggregated record (when an auth database is configured).** The same events are also folded into counters in memory and written to the table `auth_login_failures` in the auth database. Never one row per failure: one row per

- 5-minute window (start aligned to a multiple of 5 minutes, UTC),
- endpoint (`/auth/token`, `/v2`, `/admin/v1` or `other`),
- reason (the server-side reason of the log line),
- account: an existing account by its lowercase name; an unknown username **only** as `hash:xxxxxxxx` (the log's `account_hash`); a refused (throttled) attempt is always keyed by hash, because the refusal happens before the account is looked up, so an existing account that was throttled also appears as `hash:...`; empty for a bearer failure,
- client IP, as the trusted-proxy rule extracts it (the TCP peer unless `-trusted-proxies` lists the peer and the proxy appended the real client; see [operations.md](operations.md#reverse-proxy-and-client-ip)),

with a count and the first and last time seen. The raw text of an unknown username is never stored, returned or shown by any screen; each field is cut at 100 bytes.

The numbers that bound it (fixed values, not flags; see [configuration.md](configuration.md#fixed-login-protection-values)):

| Bound | Value |
| --- | --- |
| Window | 5 minutes |
| Write rate | one flush every 60 seconds and one at shutdown, whatever the failure rate; a Postgres outage never blocks or fails a login (recording only updates a map in memory) |
| Retention | 30 days: rows whose window started earlier are purged, in batches of 1000, by the periodic flush (which runs the purge after it wrote something, so an idle registry purges nothing until the next failure) |
| Rows per window | 5000; a key that would be the 5001st folds into one `_overflow` row per window and endpoint, so totals stay exact while the table stays bounded (5000 rows plus the overflow rows) |
| Live keys in memory between flushes | 10 000; further keys fold into one `_overflow` key, and records that cannot be written during an outage are kept for retry up to 10 000 and the oldest are dropped beyond that |

A flood of distinct IPs (or distinct usernames) is therefore bounded at every step: at most 10 000 keys in memory per flush interval, at most 5000 rows per window in the table, and the remainder counted in `_overflow` rows. The exact attacker detail beyond those caps is not kept; the totals are.

**Accuracy.** Counts are at-least-once: if the database reports an error while committing a chunk that did in fact commit, the retry adds that chunk again, so a count can be too high by at most one in-flight chunk (500 records) after such an error. A count is never too low, except for the records dropped from a full retry buffer (the service logs `login failures not flushed: ...` at most once a minute, without any name or IP). Reads flush the memory first, at most once every 2 seconds, so a read can be up to 2 seconds behind. A crash, or a shutdown flush that fails or exceeds its 5-second bound, loses the failures of the last 60 seconds.

**The IP is information only.** It is recorded to help an administrator see where failures come from, never to decide anything: no refusal, delay or counter depends on it, and the registry has no IP-based blocking. Behind a proxy that is not listed in `-trusted-proxies`, every failure carries the proxy's address. Per-IP volume limits belong in the reverse proxy.

### Reset and who can read

- **Who can read:** only global administrators: the three `GET /admin/v1/security/...` routes and the reset all refuse any other principal (standard, read-only, repository admin, robot, or a scope-limited token) with `403`. The failures table is not reachable by any other route. The TUI Overview row and the Login security screen use the same routes.
- **Reset:** `POST /admin/v1/security/login-backoff/reset` with `{"account":"alice"}` clears the in-memory backoff counter of that one account (an unknown username by its `hash:xxxxxxxx` label). It does not touch tokens already issued, the password, or the aggregated records. An account with no backoff state answers `404` and records nothing.
- **Audit:** each successful reset records the access event `login.backoff.reset` (actor, and as subject the account's name or its `hash:` label, never a raw unknown username), visible in `GET /admin/v1/access-events` and the Overview's Recent changes. The reset is the only write in this area.

## Risks and limits

- `local-http` transmits credentials and tokens unencrypted if used outside a trusted network.
- The generated env file may contain the Postgres DSN; its real permissions derive from how bootstrap wrote it and should be reviewed operationally.
- There is no metrics endpoint. Failed credentials are written to the structured log with the client IP and reason, counted in memory by reason, and summarized in the persisted aggregated record described in [Login failures](#login-failures-what-is-recorded-and-why) (bounded, at-least-once counts, 30 days). It is a summary for administrators, not a per-attempt audit trail: the log is the per-attempt trail, and a deployment without an auth database has no persisted record.
- The login backoff state is per process and in memory: it is lost on restart and not shared between instances.
- ⚠️ A formal review of the complete OCI-facing attack surface has not been confirmed from the current code.

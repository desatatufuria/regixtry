# Release notes: login protection

Draft. Single source for an operator upgrading to the release that carries both login hardening waves. Detail lives in [security.md](security.md#login-failures-what-is-recorded-and-why), [operations.md](operations.md#watching-and-tuning-login-protection), [api.md](api.md), [configuration.md](configuration.md) and [tui.md](tui.md#login-security).

## Read first

- **Nothing is refused that was accepted before, except weak passwords being set.** The per-account backoff ships in `log` mode: it only logs and shows who would wait. Turning it on (`-login-backoff-mode=enforce`) is your decision, after reading the data.
- **A password among the roughly 340 most common ones is now refused with `422`** wherever a password is set (`bootstrap-admin`, creating a user, resetting a password). Existing passwords keep working.
- **Set `-trusted-proxies` if you run behind a reverse proxy**, or every failed login is attributed to the proxy's address.
- **One new table** (`auth_login_failures`) in the auth database, created at the first start. Additive.

## What is new

Wave 1 (hardening):

- **Trusted proxies** (`-trusted-proxies`, `REGISTRY_TRUSTED_PROXIES`): the client IP used in the failure log. By default `X-Forwarded-For` is ignored. A `/0` range is refused at startup.
- **The IP in the failure log:** `login failed endpoint=<path> ip=<client> reason="<real reason>" account=<name>` (an unknown username as `account_hash=<8 hex>`). The HTTP answer is unchanged: uniform `401`.
- **bcrypt concurrency limit** (`-login-max-concurrency`, default `max(2, NumCPU/2)`): a password login that cannot get a slot within 250 ms is refused with `429`, `Retry-After: 1`, `TOOMANYREQUESTS`, without checking the password.
- **64-hex fast path:** a secret of exactly 64 lowercase hex characters is tried first as a credential token and skips bcrypt, so robots and CI keep working during a flood of bad passwords. A miss falls back to the normal flow.
- **Per-account backoff** (`-login-backoff-mode` `log|enforce|off`, `-login-backoff-threshold` 5, `-login-backoff-base` 2s, `-login-backoff-max` 5m): after 5 consecutive failures the account waits 2 s, 4 s, 8 s, ... up to 5 minutes. Default mode `log`: the service logs `login backoff would have blocked ...` and refuses nothing. In `enforce` the answer is `429` with `Retry-After`, identical for existing and unknown usernames. In memory, per process.
- **Common-password blocklist:** a password must have at least 8 characters and must not be one of about 340 common passwords. The match is on the whole password, ignoring case, fullwidth and compatibility forms, zero-width characters and Unicode spaces (so `ｐａｓｓｗｏｒｄ` is refused too). Refused with `422` `password is too common; choose a less guessable one`.

Wave 2 (observability):

- **Aggregated failures:** every rejected credential is also folded into a bounded record (5-minute window, endpoint, reason, account, IP, count), written every 60 seconds, kept 30 days. An unknown username is stored only as `hash:xxxxxxxx`.
- **Admin API (global admin only):** `GET /admin/v1/security/login-failures`, `GET .../login-summary`, `GET .../login-backoff`, and `POST .../login-backoff/reset`.
- **TUI:** an Overview row in NEEDS ATTENTION (`N failed logins in 15 min; M accounts waiting`) and the screen `Admin > Security > Login security` (settings, accounts waiting, recent failures, reset behind the shared confirm).
- **Reset:** clears the backoff counter of one account and records the access event `login.backoff.reset`.

## Behavior changes

- Setting a password that is too common is refused with `422`; the minimum length is 8 characters after trimming.
- Password logins can answer `429` under load (bcrypt limit) and, only in `enforce` mode, during a backoff. Clients that retry should honor `Retry-After`.
- The failure log line now carries `ip=`; the account is `account=<name>` or `account_hash=<8 hex>`.
- Startup refuses `-trusted-proxies` with a `/0` range or an invalid entry, an unknown `-login-backoff-mode`, and an invalid threshold, base or max.
- New table `auth_login_failures`; new access event action `login.backoff.reset`; four new admin routes. Existing endpoints, default modes and the default `log` backoff are unchanged.

## Before upgrading

1. Back up the Postgres auth database (for example `pg_dump --format=custom "$DSN" > auth.dump`).
2. If you are behind nginx, Traefik or Caddy, decide the `-trusted-proxies` value now (the proxy's address, never `/0`); see [operations.md](operations.md#trusted-proxies-making-the-ip-real). For the nginx host in the verified setup the value is that host's address (it appends the real client to `X-Forwarded-For`).
3. Make sure scripts that set passwords (provisioning, `bootstrap-admin`) do not use a common password.
4. Let automation use credential tokens, not a human account's password: password logins are what the backoff and the bcrypt limiter affect.
5. Keep at least one admin credential token somewhere safe: it skips the backoff, so it can be used to reset a delayed admin.
6. With Docker Compose, add `REGISTRY_TRUSTED_PROXIES` and `REGISTRY_LOGIN_BACKOFF_MODE` under the service `environment` if you need them: the shipped file does not forward them.

Then read the data for a few days in `log` mode and switch to `enforce` using the runbook ([operations.md](operations.md#recommended-path)).

## Rollback

Stop the service and start the previous binary. It ignores `auth_login_failures` (the table stays; `DROP TABLE auth_login_failures;` is optional) and every new route disappears. Remove `-trusted-proxies` and `-login-*` flags from the unit first if the previous binary does not know them. Passwords already stored are untouched; the blocklist only applies when a password is set.

## Known limits

- **Counts are at-least-once.** After a database error during a commit, a count can be too high by at most one in-flight chunk (500 records). Counts are never too low, except records dropped from a full retry buffer (10 000) during a long outage. A crash loses the failures of the last 60 seconds.
- **Reads can be up to 2 seconds old** (a read flushes at most once per 2 seconds).
- **Bounded detail:** at most 5000 rows per window; the rest is counted in `_overflow` rows, so a flood of distinct IPs keeps exact totals but loses per-IP detail.
- **The IP is information only.** No criterion in the registry depends on it. Per-IP limits belong in the reverse proxy.
- **Backoff state is in memory:** lost on restart, not shared between instances, 10 000 accounts (LRU). A flood of random usernames can evict an account's counter.
- **An attacker who knows an account name can keep it waiting** up to 5 minutes at a time in `enforce` mode (never a hard lockout). Credential tokens are exempt.
- **The username hash is not a secret:** a guessable name can be confirmed against it.
- **Tested against SQLite only.** This project's tests run the Postgres adapter's SQL on its SQLite driver; the `auth_login_failures` statements have not been run against a real PostgreSQL server in these tests.
- The aggregated record needs the auth database; without it only the log line exists.

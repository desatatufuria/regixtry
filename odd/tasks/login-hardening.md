# Login hardening
Objective: per-account backoff, bcrypt concurrency limit, token fast-path, IP logging behind trusted proxies, common-password blocklist. Branch: `feat/login-hardening` (from `integration/audit-fixes` 631fd33, local). Delivery: ask-on-risk, nothing pushed. Runner: `go test`.

## Specs
- S1 Backoff per account, never a hard lockout: 5 free consecutive failures, base 2s, x2 per failure, cap 300s (5m), reset on success or after 15 min without failures, LRU of 10 000 keys in memory, key = normalized username (case/space), same behavior for unknown and existing users. In backoff: 429 + `Retry-After: <s>` and body `TOOMANYREQUESTS`, password not verified; outside backoff: uniform 401 unchanged.
- S2 Rollout: first version is log-only (computes and logs "would have blocked", does not block); active mode behind a flag; `-login-backoff-threshold=0` disables. User answer: "Registrar y luego activar".
- S3 bcrypt concurrency limiter `max(2, NumCPU/2)`, max wait 250 ms; `-login-max-concurrency` (0 = automatic).
- S4 Fast-path: a 64-hex secret that matches a credential token skips bcrypt, same status and body; a wrong 64-hex secret never counts against the user's account bucket (per-IP bucket only when `-trusted-proxies` is set, high threshold); robots never enter backoff.
- S5 `-trusted-proxies <CIDR,...>` (env `REGISTRY_TRUSTED_PROXIES`), empty by default = `RemoteAddr`, `X-Forwarded-For` ignored. If `RemoteAddr` is in the list, walk XFF right to left skipping trusted proxies; first untrusted = client; merge multiple headers. Verified on the real deployment: nginx 192.168.1.245 sends `X-Real-IP` and appends the real IP to `X-Forwarded-For` (a client-forged first value is preserved: only the rightmost untrusted is reliable).
- S6 Every login failure logs IP and reason in the structured log plus an internal counter; no IP-based blocking.
- S7 Common-password blocklist, minimum stays 8 (not 15).
- S8 Anti-enumeration unchanged: same status, body and approximate timing for existing/unknown/disabled/robot, before and during backoff.
- S9 Flags (env): `-login-backoff-threshold` (5), `-login-backoff-base`/`-login-backoff-max` (2s/5m), `-login-max-concurrency`, `-trusted-proxies`. Docs: security.md, api.md (429/Retry-After), configuration.md, operations.md (nginx/Traefik/Caddy recipes, per-IP limit stays in the proxy).
- Out of scope now: MFA/TOTP (user: pending), Postgres state (user: memory), U5 (login.failed access event, migration, endpoint, Overview/Security TUI) is wave 2.

## Tasks
- T1 S6,S5 trusted proxies + IP/reason logging + counter — delegated writer — done: 63e485c, verified by an independent verifier, native review approved
- T2 S3,S4 bcrypt limiter + fast-path — done: b708eef (+ c88dfc2), verified, native review approved
- T3 S1,S2,S8,S9 backoff (log mode by default, enforce behind -login-backoff-mode) + flags + docs — done: 44f8fc5, verified, native review approved
- T4 S7 password blocklist — done: 18e6151 (+ 742aa42 fixture fix, ffca182 advisories: NFKC folding, strict flags, /0 trusted proxies refused), verified, native review approved
- T5 (wave 2) moved to odd/tasks/login-w2.md: aggregated failures table, security admin API, TUI (spec S2 corrected: no login.failed access-event action)

## Log
- L1 2026-11-22 user: "que preguntas tengo qeu responder para el tema del login? para el tema de ip, tenemos que tener en cuenta que puede darse el caso de que si entro por una ip de un servidor y de este por vpn, todas las conexiones puede que aparezcan como la ip del servidor o la del vpn, no?"
- L2 2026-11-22 user answers (all recommended): "Sí, con tope de 5 min", "Registrar y luego activar", "En memoria con LRU", "Sí, vacío por defecto" (trusted-proxies), "Lista de comunes, mínimo 8", "Sí, sin cambiar respuestas" (fast-path), "No, queda pendiente" (MFA). Also: "puedes hacer tu mismo la prueba actualmente ... si necestias hacer alguna prueba en ese host, hazla" (observation done, read-only).
- L3 Evidence: registry on VM listens :51243; all connections come from 192.168.1.245; headers seen: `X-Real-IP: 79.117.14.110`, `X-Forwarded-For: 203.0.113.77, 79.117.14.110` (203.0.113.77 was forged by the tester).

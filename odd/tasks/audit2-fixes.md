# Security audit run-2 follow-ups
Objective: close the five `needs_validation` leads of the security audit run-2 (report: `~/security-audit-skill/regixtry/run-2/REPORT.md`, `NEEDS-VALIDATION.md`) that the user decided to fix. Branch: `feature/audit2-fixes` (tracker, stacked on `feature/login-w2` 9a6e577, local; NEVER merge into develop). Runner: `go test`. Each unit: independent verifier + native review (all touch authorization or the push path).

## Specs
- S1 "Sí, tope de lectura": a global READ-ONLY user (`IsReadOnly`) never gets more than read access from ANY grant (exact repository grant or project grant): no write, delete, repository administration or delegation. One decision point in the grant predicate; global admins and every non-read-only principal behave byte-identically (differential).
- S2 "Sí, 1 MB antes de autorizar": admin JSON request bodies are capped at 1 MB BEFORE decoding (413 with a clear body), applied to every admin route that decodes JSON; authorization behavior and order unchanged for bodies under the cap.
- S3 Signing candidate cap (audit lead): a writer must not be able to deny verification of a signed digest by replaying a public trusted-key bundle in 257+ referrers; a valid signature must stay verifiable.
- S4 Open-upload cap (audit lead): one writer must not be able to fill the global open-upload cap (1000) and keep the slots alive past the TTL by appending; per-identity fairness and an absolute lifetime for an upload session.
- S5 Push-scan admission (audit lead): a few writer identities must not saturate the admission pool so that the pull gate fails open; fairness per identity and a defined, safe gate behavior when a digest has not been scanned.
- S6 No behavior change for legitimate flows; documented limits updated in docs/security.md; the 4 run-1 variants (push-scan goroutines, manifest body cap, upload cap/reaper, signing shadowing) are the same family as S3-S5.

## Tasks
- T1 S1 read-only cap — done: f5b6283..c86d6bb; one decision point `Principal.cappedRole`; independent verification (30 principals x 3 targets x ~22 verbs, 0 mismatches; 4,140-decision differential identical); native review approved; exact writer/admin grants for read-only targets refused 422 at grant time
- T2 S2 admin JSON body cap 1 MiB — done: a3d2cbb, 08bc6cb; parent spot check; 192-request differential identical; delegate routes still authorize after decode (bounded by the cap)
- T3 S3 signing candidate bounds — done: f34670d, 11d949e; independent verification (lead reproduced at baseline, never `signed` for untrusted/unsigned in any flood; residual: >=1025 distinct bundles still gives an honest `unverifiable`); native review approved (warning accepted)
- T4 S4 per-identity open-upload cap (100, anonymous exempt), 72 h absolute lifetime, additive uploads.owner migration — done: 212b9ce..e0f550f; independent verification (caps hold under concurrency; regression in anonymous mode found and fixed); native review approved
- T5 S5 push-scan admission fairness and deferral — done: f008bbf..c4a73d6; independent verification (identities needed to deny scanning 4 -> 22; gate unchanged: unscanned digest allowed as at baseline); native review approved

## Log
- L1 2026-11-23 user answers: "Sí, tope de lectura (Recommended)", "Sí, 1 MB antes de autorizar (Recommended)", "Arreglarlos ahora" (the three other leads), "No, todavía no (Recommended)" (no new RC yet).
- L2 Audit facts: read-only status is ignored by the exact-repo and project grant checks (`internal/domain/auth/principal.go` grants loop; `internal/app/regixtry/service.go` ~:706); signing cap `maxSignatureCandidates = 256` in `service_signing.go` (~:229, budget ~:331); open-upload cap in `service.go` ~:307; push-scan admission in `service_push_scan.go` ~:35-106; admin JSON decode in `admin_handlers.go` ~:1711.
- L3 Standing rules: no Co-Authored-By/Claude-Session/AI attribution; Conventional Commits; never push; never `git stash`; disk above 15 GB.
- L4 2026-11-23 closing: all five units merged into feature/audit2-fixes. Residual risks documented in docs/security.md: >=1025 distinct bundles can still make a digest `unverifiable` (fail closed); ~22 writer identities can still exhaust push-scan admission; the push-scan retry list is in memory (lost on restart); the unscanned-digest gate semantics are as at baseline (no require-scan mode); Postgres-specific SQL was never run against a real Postgres.

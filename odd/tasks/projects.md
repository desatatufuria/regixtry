# Projects (Harbor-style)
Objective: first-level PROJECT entity that must exist before pushing, project-wide roles, visual-only folder tree. Branch: `feature/projects` (tracker, from develop 31c464f, local; NEVER merge into develop). Delivery: `ask-on-risk`, units merged into the tracker only after independent verification + native review where marked. Runner: `go test`. Study: scratchpad `study-projects-implementation.md` (schema, 15-site grant inventory, units).

## Specs
- S1 "primero debe de existir el proyecto como en harbor": a project is a real entity created before any push; project = FIRST path segment of the repository name (`odabank/payments/strike` -> project `odabank`); more slashes inside are just the repository name.
- S2 "el proyecto: un rol cubre todo lo de dentro": ONE role on the project covers every repository inside it; existing exact per-repository grants keep working; effective role = highest of (project role, exact repo grant); a repo grant can only widen, never narrow (user: "El más alto gana").
- S3 Matching by first segment with `==`, never string prefix: `odabank` never covers `odabank2/...` nor the flat repo named `odabank`.
- S4 Existing flat repositories (no slash) keep working and keep their exact grants; NO new flat repositories can be created once enforcement is on (user: "Seguir funcionando, sin nuevos").
- S5 Only global administrators create projects; a project is deleted only when EMPTY and with deletion enabled (`-delete-enabled`); no rename; no public/anonymous projects in v1; no quotas in v1.
- S6 Robots may hold a PROJECT grant, reader or writer only (never admin); mutually exclusive with the single repo grant.
- S7 Enforcement of "project must exist" is OFF in RC1 and ON by default at the end, behind `REGISTRY_REQUIRE_PROJECT`. A push to a missing project answers 404 `NAME_UNKNOWN` ("project \"odabank\" does not exist; ask an administrator to create it"); authorization is checked BEFORE existence so a caller without permission never learns whether a project exists.
- S8 "solo quiero que visualmente se pueda navegar de manera correcta por las carpetas": nested folders are a VISUAL-ONLY tree in the TUI with breadcrumbs and totals; no folder entity, no per-folder permission; permissions are given on the project or on the repository.
- S9 Data: additive migrations only (SQLite `projects`, Postgres `auth_project_grants`), idempotent backfill creating one project per distinct first segment of existing repositories, nothing deleted, an old binary ignores the new tables (fails closed on project grants).
- S10 Customers (odabank, gosse, metalab) are operated by the user's own team: no strong tenant isolation, no per-tenant users; global admins see everything.
- S11 Access events for project grant put/delete and project created/deleted. Admin API additive under `/admin/v1/projects`.
- S12 Security-critical: every site that matches grants by repository name must honour project grants (inventory in study 2.4); a test must fail if a new exact-match site appears.

## Tasks
- T1 U1 model + SQLite store + backfill + project service, no enforcement (S1,S3,S5,S9) — wave 1 — route: delegated (parallel unit with disjoint surfaces) — pending
- T2 U2 project grants in auth: Postgres table, Principal matching, token scopes, effective role, catalog post-limit filtering, duplicate hasRepoAdminGrant (S2,S3,S12) — wave 1 — delegated — pending
- T3 U3 admin API for projects, delegation rules, robots with project, access events, cross-store create/delete orchestration (S5,S6,S11) — wave 2 — pending
- T4 U4 extend design boards (Projects, Members, dialogs, inherited access, folder tree, revoke-grant action) — wave 2 — pending
- T5 U5 enforcement at push + flag + flat-name rule (S4,S7) — wave 2 — pending
- T6 U6 TUI screens: Console/Admin Projects, Members, dialogs, Edit user inherited access — wave 3 — pending
- T7 U7 visual folder tree with breadcrumbs (S8) — wave 3 — pending
- T8 U8 docs — wave 4 — pending
- T9 Login hardening wave 2 (`login.failed` event + TUI), separate tracker after projects — pending

## Log
- L1 2026-11-22 user: "quiero que generes un estudio de cómo implementarlo. sí, primero debe de existir el proyecto como en harbor."
- L2 2026-11-22 user: "lo que queiro es poder tener proyectos diferentes separados por clientes en los que se trabaja es decir: odabank, gosse, metalab, etc..."
- L3 2026-11-22 user: "solo quiero que visualmente se pueda navegar de manera correcta por las carpetas es algo visual sería la filosofía de harbor pero llevado a anidamiento de carpetas pero los permisos se dan sobre la imagen en sí como hace harbor"
- L4 2026-11-22 user decisions (all recommended): "El más alto gana", "Seguir funcionando, sin nuevos", "No, todo privado", "Solo administradores globales", "Solo vacío y con borrado habilitado", "Sí, solo lector o escritor", "Apagado en la RC1, activo al final".
- L5 2026-11-22 user: "En una rama tracker nueva, Ponte con el tema de proyecto que hemos preparado. Pon varias unidades en paralelo. Tienes autorización para hacer las review necesarias y continuar. Siempre que puedas y sea seguro, agrega unidades en paralelo. Continua de manera automática hasta terminar. Cuando termines, te pones con el tema de login que ya hablamos. Con los mismos permisos de review y de continuar de manera automática. No mergees nada en develop."
- L6 Standing rules: no Co-Authored-By/Claude-Session/AI attribution; Conventional Commits; never push; never `git stash`; remote operations only with explicit authorization; disk above 15 GB; user-facing times in hours/days.
- L7 Wave plan: W1 = U1 || U2 (disjoint surfaces); W2 = U3 || U5 || U4 after W1 merged; W3 = U6 || U7 after W2; W4 = U8 then login wave 2. Verification: independent verifier + native review for U1, U2, U3, U5.

# Glossary

## Quick path

- **Regixtry API**: the OCI Distribution / Docker Registry HTTP API used to push and pull content.
- **Docker Engine API**: daemon management APIs for containers, images, and runtime operations; not this product boundary.
- **Thin client**: a UI that requests data and actions from services instead of owning domain rules.
- **Feature-branch chain**: a chained PR strategy where each slice targets the previous slice branch instead of `main`.

## Terms

| Term | Meaning |
| --- | --- |
| OCI Distribution API | The protocol boundary for registry push/pull, manifests, blobs, catalog, and tags |
| Repository | A named image namespace that contains tags and digest-addressed manifests |
| Manifest | Metadata document that references configuration and layer blobs |
| Blob | Content-addressed binary object stored by digest |
| Upload staging | Temporary storage for in-progress blob uploads before digest validation and promotion |
| Metadata store | SQLite-backed index for repositories, tags, manifests, and upload state |
| Blob store | Filesystem-backed content store for durable blobs and upload staging |
| Access controller | Authorization seam that can challenge or permit actions without hard-wiring full RBAC into v1 |
| Tenant resolver | Boundary that returns the active tenant context; v1 uses a single-tenant default |
| Operator console | Bubble Tea TUI that inspects registry state through application services |
| Maintenance basics | Small v1-safe operator actions or visibility features that do not expand into a full control plane |
| Feature tracker branch | The long-lived feature branch that aggregates chained PR slices before merging into `develop` |
| Chained PR slice | One reviewable work unit in a larger feature sequence |
| Registry-wide read-only role | A user flag (`is_read_only`) that grants pull access across all repositories independent of per-repository grants |
| Delegated repo-admin grant | An admin-issued grant that scopes repo-admin authority to exactly one repository, managed via `/admin/v1/repositories/{repo}/grants` |
| Robot account | A bounded-TTL, revocable non-human account (`is_robot`) permanently excluded from password login and from the default human user listing |
| Feature runtime kind | The classification of a managed feature as either built-in (runs in-process, e.g. signing) or backed by an external binary/service the feature runtime installs and supervises (e.g. Trivy, Gitleaks) |
| Scan policy | The stored settings (`scan_settings`) governing whether and how a repository is scanned, including severity thresholds and enablement |
| Scan run | A single persisted execution of a scanner (Trivy or Gitleaks) against a repository, with its status and result summary |
| Signing policy | The fail-closed pull gate configuration, global or per-repository, requiring a valid cosign signature from one of one or more trusted public keys before a manifest may be pulled, managed via `GET`/`PUT /admin/v1/signing-policy` |
| Trusted key | An ECDSA P-256 public key (PEM) a signing policy accepts as valid; a policy may hold several, and a signature verifying against any one of them satisfies the gate |
| Key fingerprint | A short, read-only display identifier for a trusted key: the first 12 hex characters of the SHA-256 hash of its trimmed PEM text (`signing.Fingerprint`) |
| Blob garbage collection | The report-then-delete admin flow (`/admin/v1/gc/reports`) that finds blobs no manifest references and are past their grace window, then unlinks them from disk when explicitly enabled |
| GC report | A computed, time-bounded snapshot of garbage-collection candidates that a delete request re-checks against fresh state before acting on it |

## Boundary distinctions

| Distinction | What it means here |
| --- | --- |
| Regixtry API vs Docker Engine API | Regixtry content exchange is in scope; daemon/runtime control is not |
| Thin client vs second backend | The TUI calls services for truth instead of rebuilding rules locally |
| Local durability vs platform storage | V1 stores blobs locally and defers remote storage abstractions beyond the initial seam |
| Auth seam vs full auth product | V1 prepares challenge/authorization boundaries without shipping broad RBAC |

## Boundary reminder

If a feature depends on Docker daemon control, container lifecycle management, or host runtime orchestration, it is outside the registry contract unless explicitly introduced as a separate post-v1 capability.

If a proposed contribution cannot be explained with the terms above, the contributor should update this glossary and the matching reader-facing docs in the same work unit.

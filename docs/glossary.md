# Glossary

## Quick path

- **Registry API**: the OCI Distribution / Docker Registry HTTP API used to push and pull content.
- **Docker Engine API**: daemon management APIs for containers, images, and runtime operations; not this product boundary.
- **Thin client**: a UI that requests data and actions from services instead of owning domain rules.

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

## Boundary reminder

If a feature depends on Docker daemon control, container lifecycle management, or host runtime orchestration, it is outside the registry contract unless explicitly introduced as a separate post-v1 capability.

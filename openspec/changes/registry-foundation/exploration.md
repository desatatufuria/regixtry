## Exploration: Registry foundation

### Current State
The repository is not an application yet. It currently contains only SDD/bootstrap artifacts (`openspec/`) and a Go-oriented devcontainer (`.devcontainer/`). There is no Go module, no registry server, no TUI, no tests, and no existing product code to inspect, so this exploration is based on current repo state plus industry/OSS references.

Industry signals point to a clear split:
- **Distribution / OCI Distribution** is the protocol-and-storage baseline: stateless registry service, content-addressed blobs, resumable uploads, manifest/blob APIs, token auth challenge flow, filesystem/S3-style storage drivers, and explicit garbage collection.
- **Harbor** is a platform product built on top of registry capabilities: projects, RBAC, scanning, signing, replication, audit, UI, admin APIs, and enterprise policy.
- **zot** shows the value of a **single-binary, OCI-native, low-friction** registry with optional richer features, which is closer to the desired product direction than Harbor’s platform weight.
- **Dragonfly** is adjacent infrastructure for content distribution acceleration, not a substitute for registry core behavior.

Critical API boundary:
- **Registry HTTP API / OCI Distribution API** is the product’s main protocol surface for push/pull/catalog/tags/manifests/blobs/auth challenge.
- **Docker Engine API** talks to the local Docker daemon for image/build/container operations. It is useful for local workflows and TUI operator conveniences, but it is **not** the registry protocol and should not define registry storage semantics.

TUI signals from Lazydocker/Bubble Tea patterns:
- One-screen operational visibility, fast keyboard-first navigation, obvious key help, progressive disclosure, low-noise defaults.
- Bubble Tea’s MVU model plus Bubbles `list`, `table`, `viewport`, `help`, `spinner`, and `paginator` fit a low-resource operator console.
- Avoid high-frequency redraws, over-wrapped logs, and heavy background polling; Lazydocker explicitly calls out CPU tradeoffs around wrapping/log behavior.

Recommended v1 boundary:
- Single-tenant registry instance.
- Local filesystem storage only.
- OCI/Docker-compatible push and pull.
- Minimal repository and tag browsing.
- Basic manifest/blob inspection and storage visibility.
- Simple auth mode(s) only if they keep operations lightweight.
- Explicit omission of Harbor-class features: multi-tenancy, replication, vulnerability scanning, signing workflow orchestration, remote object storage, policy engines, and broad admin surface.

Required day-one seams for future evolution:
- `authn/authz` boundary separate from registry request handling.
- `tenant` resolution abstraction even if v1 hardcodes a single tenant.
- `storage` interface with local filesystem implementation first.
- `metadata/index` boundary distinct from blob store.
- `registry API` service separated from `operator TUI` service/client.
- background jobs boundary for GC, retention, and sync tasks.

Documentation and roadmap should exist before implementation:
- product vision / non-goals
- glossary and API-boundary explainer
- v1 scope + future boundaries
- architecture overview with seams
- operator UX principles and TUI information architecture
- roadmap grouped by foundation, protocol, storage, UX, and future expansion

### Affected Areas
- `openspec/config.yaml` — already establishes benchmark-first, doc-first, API-boundary-first constraints for future phases.
- `.devcontainer/devcontainer.json` — current runtime context confirms Docker socket access and a Go-oriented local development environment, but not an existing app architecture.
- `openspec/changes/registry-foundation/exploration.md` — persisted exploration artifact for proposal/design/spec follow-up.

### Approaches
1. **Distribution-first minimal registry** — implement a lean OCI/Docker-compatible core around registry API semantics, then add a separate operator TUI.
   - Pros: Closest to product goals; respects protocol fundamentals; keeps v1 small; preserves future seams for storage/auth/tenancy.
   - Cons: Requires disciplined scope control; fewer built-in features than platform registries; metadata/search/admin ergonomics must be designed intentionally.
   - Effort: Medium

2. **Harbor-like integrated platform from day one** — include projects, RBAC, scanning, replication, admin APIs, and broader governance early.
   - Pros: Rich feature set; familiar enterprise positioning; strong long-term story.
   - Cons: Misaligned with minimal single-tenant v1; much higher operational and cognitive cost; likely to delay useful release.
   - Effort: High

3. **Engine-centric local tool with registry veneer** — lean on Docker Engine behavior for core flows and add limited registry compatibility around it.
   - Pros: Faster prototyping for local workflows; easy to demo with Docker users.
   - Cons: Architectural trap; confuses daemon concerns with registry concerns; weak portability beyond Docker; risks breaking OCI-first design.
   - Effort: Medium

### Recommendation
Use **Approach 1: Distribution-first minimal registry**.

Frame the product as a **single-binary, single-tenant, local-storage OCI registry with a polished operator TUI**, not as a reduced Harbor clone and not as a Docker-daemon wrapper. The next phase should lock a roadmap around protocol compliance, local storage correctness, low-resource operations, and a thin Bubble Tea console that observes and manages the registry rather than owning core registry semantics.

Practical v1 product line:
- MUST support OCI/Docker push/pull flows, manifests, blobs, tags, and repository listing.
- SHOULD support digest-first inspection, upload state visibility, and safe garbage-collection workflows.
- MAY support simple local auth in v1 if it does not force multi-tenant design.
- MUST NOT implement multi-tenant policy, replication, scanning, remote object storage, or Harbor-scale administration in v1.

### Risks
- Confusing Docker Engine API with Registry HTTP API and coupling the product to a local daemon.
- Underestimating content-addressed storage rules, manifest/blob integrity, resumable upload edge cases, and GC safety.
- Treating catalog/tags browsing as authoritative inventory when registry implementations may paginate, filter, or omit results.
- Letting the TUI own domain logic instead of keeping it as a thin operator client.
- Adding Harbor-class features too early and losing the minimal/low-resource product identity.
- Skipping explicit seams for storage/auth/tenancy and making future multi-tenant or remote-storage work expensive.
- Forgetting auth challenge/token flow details (`401` + `WWW-Authenticate` + token service) and shipping incompatible client behavior.

### Ready for Proposal
Yes — the next phase should create a proposal for `registry-foundation` that fixes the v1 product boundary, names the architectural seams, and defines the roadmap/documentation set required before implementation begins.

package ports

import (
	"context"
	"encoding/json"
	"io"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
)

type BlobStore interface {
	BeginUpload(ctx context.Context, repository domain.RepositoryRef) (domain.UploadState, error)
	GetUpload(ctx context.Context, uploadID string) (domain.UploadState, error)
	PutUploadChunk(ctx context.Context, uploadID string, content io.Reader) (domain.UploadState, error)
	CommitUpload(ctx context.Context, uploadID string, expected domain.Digest) (domain.Descriptor, error)
	CancelUpload(ctx context.Context, uploadID string) error
	BlobExists(ctx context.Context, digest domain.Digest) (bool, error)
	OpenBlob(ctx context.Context, digest domain.Digest) (io.ReadSeekCloser, domain.Descriptor, error)

	// ListBlobs returns one entry per committed blob file in the store.
	// It is GLOBAL by construction: blob files are content-addressed with
	// zero tenant or repository namespacing (fsblob stores them at
	// blobsRoot/<algorithm>/<hex>), so one file is shared by every tenant
	// that pushed that content and there is nothing to scope by.
	//
	// Implementations MUST NOT enumerate in-flight uploads. Those live in a
	// separate tree (uploads/<id>/) and are not blobs until CommitUpload
	// renames one into blobsRoot; enumerating them would expose a
	// half-written push to the garbage collector.
	ListBlobs(ctx context.Context) ([]BlobFileInfo, error)

	// DeleteBlob unlinks exactly one committed blob file. Idempotent: an
	// already-absent file returns (false, nil), never an error.
	// Implementations MUST validate the digest before resolving any path and
	// MUST refuse to touch anything outside their own blob root -- in
	// particular, never anything under an uploads/ tree.
	DeleteBlob(ctx context.Context, digest domain.Digest) (removed bool, err error)
}

// BlobFileInfo is one blob file as it exists on disk right now. ModTime is
// the file's mtime, which is write time and not commit time -- the 24h grace
// window (gcGraceWindow) is sized to absorb that difference.
type BlobFileInfo struct {
	Digest  domain.Digest
	Size    int64
	ModTime time.Time
}

type MetadataStore interface {
	SaveUpload(ctx context.Context, tenant string, state domain.UploadState) error
	GetUpload(ctx context.Context, tenant string, uploadID string) (domain.UploadState, error)
	ListUploads(ctx context.Context, tenant string, repository *domain.RepositoryRef) ([]domain.UploadState, error)
	DeleteUpload(ctx context.Context, tenant string, uploadID string) error
	PublishManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, tag string, manifest domain.Manifest, blobs []domain.Descriptor) error
	ResolveManifest(ctx context.Context, tenant string, repository domain.RepositoryRef, reference string) (domain.Manifest, error)
	Catalog(ctx context.Context, tenant string, limit int, after string) ([]domain.RepositoryRef, error)
	ListTags(ctx context.Context, tenant string, repository domain.RepositoryRef, limit int, after string) ([]string, error)
	// ListTagsWithCreatedAt is ListTags plus each tag's manifest created_at
	// (console-tags-table change), joined from the manifests table rather
	// than the tags table's own created_at -- a retag of an existing digest
	// must still report the manifest's original push time, not when the tag
	// pointer itself was last written.
	ListTagsWithCreatedAt(ctx context.Context, tenant string, repository domain.RepositoryRef, limit int, after string) ([]TagSummary, error)
	// ListRepositoriesWithSummary is Catalog plus each repository's tag
	// count and most recent manifest created_at across its tags
	// (console-repositories-table change), aggregated in a single query
	// (COUNT(tags)/MAX(manifests.created_at) GROUP BY repository) rather
	// than one round trip per repository -- unlike ListTagsWithCreatedAt's
	// per-tag consumer (TagDetails), which layers per-item signature
	// verification that cannot be expressed in SQL, tag-count/last-pushed
	// is pure aggregation with no per-item business logic, so a single
	// query scales correctly against a potentially large catalog.
	ListRepositoriesWithSummary(ctx context.Context, tenant string, limit int, after string) ([]RepositorySummary, error)
	ListManifestBlobs(ctx context.Context, tenant string, repository domain.RepositoryRef, manifestDigest domain.Digest) ([]domain.Descriptor, error)
	// ListReferrers returns every manifest in ONE tenant's ONE repository
	// whose subject.digest equals subjectDigest, ordered by digest ASC
	// (oci-referrers-api design.md Decision 3/4). It is scoped exactly like
	// ResolveManifest/ListTags (tenant AND repository) and explicitly NOT
	// like ListReferencedBlobDigests, which is global by design -- see that
	// method's doc comment for why cross-tenant scoping is the anti-pattern
	// here. An absent subject digest is an empty slice, never a
	// domain.ErrorCodeNotFound: this is a list query, not a single-row
	// lookup. The adapter never parses the returned Payload as OCI JSON --
	// that derivation belongs to the app layer (parseManifestPayload).
	ListReferrers(ctx context.Context, tenant string, repository domain.RepositoryRef, subjectDigest domain.Digest) ([]ReferrerRow, error)
	GetScanSettings(ctx context.Context, tenant string, feature string) (ScanSettings, error)
	UpsertScanSettings(ctx context.Context, tenant string, feature string, settings ScanSettings) error
	GetScanPolicySettings(ctx context.Context, tenant string) (ScanPolicySettings, error)
	UpsertScanPolicySettings(ctx context.Context, tenant string, settings ScanPolicySettings) error
	GetSigningPolicySettings(ctx context.Context, tenant string) (SigningPolicySettings, error)
	UpsertSigningPolicySettings(ctx context.Context, tenant string, settings SigningPolicySettings) error
	// GetUpdateChannel/UpsertUpdateChannel mirror GetScanPolicySettings/
	// UpsertScanPolicySettings' shape exactly (row absence is a typed
	// domain.ErrorCodeNotFound; the code-level default lives one layer up,
	// at the service) even though the setting itself is genuinely global,
	// not per-repository -- tenant is threaded through only because every
	// other settings row in this store already is, and this is a
	// single-tenant deployment today regardless.
	GetUpdateChannel(ctx context.Context, tenant string) (UpdateChannelSettings, error)
	UpsertUpdateChannel(ctx context.Context, tenant string, settings UpdateChannelSettings) error
	GetFeatureRuntimeState(ctx context.Context, tenant string, feature string) (FeatureRuntimeState, error)
	UpsertFeatureRuntimeState(ctx context.Context, tenant string, feature string, state FeatureRuntimeState) error
	GetActiveScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ScanRun, error)
	GetLatestScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ScanRun, error)
	GetScanRun(ctx context.Context, tenant string, runID string) (ScanRun, error)
	GetScanRunDetail(ctx context.Context, tenant string, runID string) (ScanRunDetail, error)
	UpsertScanRun(ctx context.Context, tenant string, run ScanRun) error
	UpsertScanRunDetail(ctx context.Context, tenant string, detail ScanRunDetail) error
	ListScanRuns(ctx context.Context, tenant string, repository string, limit int) ([]ScanRun, error)
	// ListLatestScanRunPerRepository returns each repository's most recent
	// scan run plus its total run count, one row per repository, ordered
	// severity-first -- collapsed BEFORE limit is applied so no repository
	// can be crowded out by another's rescans (see RepositoryScanSummary).
	ListLatestScanRunPerRepository(ctx context.Context, tenant string, limit int) ([]RepositoryScanSummary, error)
	TryAcquireScanSchedulerLease(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) (bool, ScanSchedulerState, error)
	HeartbeatScanScheduler(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) error
	GetScanSchedulerState(ctx context.Context, tenant string) (ScanSchedulerState, error)
	UpsertScanSchedulerState(ctx context.Context, tenant string, state ScanSchedulerState) error
	GetActiveSecretScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (SecretScanRun, error)
	GetSecretScanRun(ctx context.Context, tenant string, runID string) (SecretScanRun, error)
	GetSecretScanRunDetail(ctx context.Context, tenant string, runID string) (SecretScanRunDetail, error)
	UpsertSecretScanRun(ctx context.Context, tenant string, run SecretScanRun) error
	UpsertSecretScanRunDetail(ctx context.Context, tenant string, detail SecretScanRunDetail) error
	ListSecretScanRuns(ctx context.Context, tenant string, repository string, limit int) ([]SecretScanRun, error)
	// GetRepositoryFeatureOverride returns the stored JSON payload for one
	// (tenant, repository, feature) override row. Row absence is a typed
	// domain.ErrorCodeNotFound, never a found bool (design.md Decision 2).
	GetRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string) ([]byte, error)
	// ListRepositoryFeatureOverrides returns every override row for a
	// (tenant, feature) pair, used by the list/API projection only — the
	// resolution path never uses it.
	ListRepositoryFeatureOverrides(ctx context.Context, tenant string, feature string) ([]RepositoryFeatureOverride, error)
	// UpsertRepositoryFeatureOverride creates or replaces the payload for one
	// (tenant, repository, feature) row.
	UpsertRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string, payload []byte) error
	// DeleteRepositoryFeatureOverride removes one (tenant, repository,
	// feature) row. It returns a typed domain.ErrorCodeNotFound when no row
	// was affected, mirroring DeleteUpload.
	DeleteRepositoryFeatureOverride(ctx context.Context, tenant string, repository string, feature string) error
	// DeleteManifestByDigest removes one manifests row; the ON DELETE CASCADE FKs
	// remove its tags and manifest_blobs rows in the same transaction. It returns
	// the names of the tags that pointed at that digest, selected inside that
	// transaction before the delete, so the caller can report exactly what the
	// cascade removed. Zero rows affected is a typed domain.ErrorCodeNotFound,
	// mirroring DeleteUpload. It never touches blob files on disk.
	DeleteManifestByDigest(ctx context.Context, tenant string, repository domain.RepositoryRef, digest domain.Digest) ([]string, error)
	// DeleteTag removes one tags row, leaving the manifest and every other tag on
	// it intact. Zero rows affected is a typed domain.ErrorCodeNotFound.
	DeleteTag(ctx context.Context, tenant string, repository domain.RepositoryRef, tag string) error
	// DeleteRepository removes one repositories row; the ON DELETE CASCADE FKs
	// remove every manifests/tags/manifest_blobs row scoped to it in the same
	// transaction (delete-entire-repository feature, mirroring
	// DeleteManifestByDigest's own shape). It returns every removed tag name
	// and the number of manifests removed, selected inside that transaction
	// before the delete. Zero rows affected is a typed domain.ErrorCodeNotFound,
	// mirroring DeleteManifestByDigest. It never touches blob files on disk.
	DeleteRepository(ctx context.Context, tenant string, repository domain.RepositoryRef) (tagsRemoved []string, manifestsRemoved int, err error)

	// ListReferencedBlobDigests returns every distinct digest referenced by
	// ANY manifest in the deployment: SELECT DISTINCT digest FROM
	// manifest_blobs, with NO tenant predicate and no join that could
	// introduce one.
	//
	// !!! THIS METHOD TAKES NO tenant ARGUMENT, AND THAT IS DELIBERATE !!!
	// Every other method on this interface is tenant-scoped. This one must
	// never be. It is the mark set of a mark-and-sweep over the blob store,
	// and blob files carry no tenant namespacing at all: one file is shared
	// by every tenant that pushed that content. The manifest_blobs table has
	// no tenant column. Scoping this query -- directly, or by joining
	// manifests to reach a tenant -- shrinks the mark set, so blobs that
	// another tenant still references are reported as garbage and unlinked.
	// The result is silent, irreversible, cross-tenant registry corruption
	// with no recovery short of a re-push or a blobsRoot restore.
	ListReferencedBlobDigests(ctx context.Context) ([]string, error)

	// IsBlobDigestReferenced is a narrow, single-digest counterpart to
	// ListReferencedBlobDigests: SELECT EXISTS(SELECT 1 FROM manifest_blobs
	// WHERE digest = ?), with NO tenant predicate for the same reason
	// ListReferencedBlobDigests has none (design.md Decision C). It exists
	// so DeleteByGCReport can re-verify one candidate immediately before
	// unlinking it, without re-running the full global mark query per
	// digest (JD-1: the batch-wide freshSet snapshot alone leaves a window
	// between the snapshot and an individual digest's own turn in the
	// delete loop).
	IsBlobDigestReferenced(ctx context.Context, digest string) (bool, error)

	// CreateGCReport inserts one gc_reports row plus its
	// gc_report_candidates children in a single transaction; a partially
	// written report is never observable. GLOBAL: no tenant argument.
	// report.TriggeredInTenant is provenance only and is never a predicate.
	CreateGCReport(ctx context.Context, report GCReport, candidates []GCReportCandidate) error

	// GetGCReport returns one report with its candidates in stored position
	// order. GLOBAL: a report created under one tenant's request context is
	// readable and usable from any other. An absent row is a typed
	// domain.ErrorCodeNotFound, mirroring DeleteUpload.
	GetGCReport(ctx context.Context, reportID string) (GCReportDetail, error)

	// PruneExpiredGCReports deletes gc_reports rows still in "reported"
	// state whose expires_at <= now; children go with the ON DELETE CASCADE.
	// "deleted"-state rows are the audit trail of irreversible deletions and
	// are NEVER pruned. This is hygiene, not safety (D8) -- correctness is
	// the recompute-and-intersect in DeleteByGCReport.
	PruneExpiredGCReports(ctx context.Context, now time.Time) (int, error)

	// MarkGCReportDeleted transitions one report row from "reported" to
	// "deleted" and records its terminal outcome (design.md Decision E):
	// UPDATE gc_reports ... WHERE id = ? AND status = 'reported', plus one
	// per-candidate outcome UPDATE. Zero rows affected by the report UPDATE
	// is a typed domain.ErrorCodeConflict -- the SQL WHERE clause is the
	// actual single-use guard, settling the two-concurrent-deletes race, not
	// just a Go status check made in advance.
	MarkGCReportDeleted(ctx context.Context, reportID string, outcome GCDeleteOutcome) error
}

type GCReportStatus string

const (
	GCReportStatusReported GCReportStatus = "reported" // preview; usable once
	GCReportStatusDeleted  GCReportStatus = "deleted"  // terminal, frozen, never replayable
)

type GCReport struct {
	ID             string         `json:"id"`
	Status         GCReportStatus `json:"status"`
	ComputedAt     time.Time      `json:"computed_at"`
	ExpiresAt      time.Time      `json:"expires_at"`
	GraceCutoff    time.Time      `json:"grace_cutoff"`
	CandidateCount int            `json:"candidate_count"`
	CandidateBytes int64          `json:"candidate_bytes"`
	DurationMillis int64          `json:"duration_ms"`
	RequestedBy    string         `json:"requested_by,omitempty"`
	// TriggeredInTenant is PROVENANCE ONLY. It is deliberately not named
	// "tenant": reports are global, and no query may ever filter on it.
	TriggeredInTenant string     `json:"triggered_in_tenant,omitempty"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
	DeletedBy         string     `json:"deleted_by,omitempty"`
	DeletedCount      int        `json:"deleted_count"`
	BytesReclaimed    int64      `json:"bytes_reclaimed"`
	Error             string     `json:"error,omitempty"`
}

type GCReportCandidate struct {
	Digest  string    `json:"digest"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	Outcome string    `json:"outcome,omitempty"`
	Error   string    `json:"error,omitempty"`
}

type GCReportDetail struct {
	Report     GCReport            `json:"report"`
	Candidates []GCReportCandidate `json:"candidates"`
}

// GC candidate/outcome constants. "" (GCCandidateOutcomePending) is a
// candidate row's outcome before any delete has run against its report.
const (
	GCCandidateOutcomePending  = ""
	GCCandidateOutcomeDeleted  = "deleted"
	GCCandidateOutcomeRetained = "retained"
	GCCandidateOutcomeMissing  = "missing"
	GCCandidateOutcomeFailed   = "failed"
)

// GCCandidateOutcome is one digest's terminal delete-time result, part of
// GCDeleteOutcome.
type GCCandidateOutcome struct {
	Digest  string `json:"digest"`
	Outcome string `json:"outcome"`
	Error   string `json:"error,omitempty"`
}

// GCDeleteOutcome is the result of one DeleteByGCReport call, passed to
// MarkGCReportDeleted to freeze the terminal report row. Unlink failures are
// per-digest and never abort the whole delete (design.md Testing Strategy
// T10): the report still reaches "deleted" with Error summarizing how many
// of the candidates failed.
type GCDeleteOutcome struct {
	DeletedAt      time.Time            `json:"deleted_at"`
	DeletedBy      string               `json:"deleted_by,omitempty"`
	DeletedCount   int                  `json:"deleted_count"`
	BytesReclaimed int64                `json:"bytes_reclaimed"`
	Error          string               `json:"error,omitempty"`
	Candidates     []GCCandidateOutcome `json:"candidates"`
}

// TagSummary is one tag's name, its manifest's created_at, and who pushed
// that manifest (console-tags-table / console-tags-pushed-by changes'
// backend shape), returned by ListTagsWithCreatedAt. PushedBy is the raw
// principal UserID from manifests.pushed_by -- "" for a legacy manifest
// pushed before that column existed, or one pushed with no principal in
// context. Resolving it to a human-readable username is the app layer's job
// (Service.UsernameResolver), not this layer's.
type TagSummary struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	PushedBy  string    `json:"pushed_by,omitempty"`
}

// RepositorySummary is one repository's name, its tag count, and the most
// recent manifest created_at across all of its tags (console-repositories-
// table change's backend shape), returned by ListRepositoriesWithSummary.
// LastPushed is zero when the repository has no tags.
type RepositorySummary struct {
	Name       string    `json:"name"`
	TagCount   int       `json:"tag_count"`
	LastPushed time.Time `json:"last_pushed"`
}

// ReferrerRow is one manifests row matched by subject_digest, returned by
// ListReferrers (oci-referrers-api design.md Decision 3). It is a row, not a
// domain object: the SQLite adapter never parses OCI JSON, so Payload is
// returned verbatim and the app layer derives artifactType/annotations via
// parseManifestPayload. Digest is domain.Digest, not string, so an
// unvalidated digest is unrepresentable at this port boundary -- the same
// reasoning ListReferrers' own subjectDigest parameter follows, matching
// DeleteManifestByDigest.
type ReferrerRow struct {
	Digest    domain.Digest
	MediaType string
	Size      int64
	Payload   []byte
}

const (
	ScanRunStatusQueued    = "queued"
	ScanRunStatusRunning   = "running"
	ScanRunStatusCompleted = "completed"
	ScanRunStatusFailed    = "failed"

	ScanTriggerManual    = "manual"
	ScanTriggerScheduled = "scheduled"
	ScanTriggerPush      = "push"

	ScanPolicyThresholdCritical     = "critical"
	ScanPolicyThresholdCriticalHigh = "critical_high"
)

// ScanPolicySettings is the global vulnerability policy gate configuration:
// whether the gate is enforced at all, and the severity threshold a
// completed scan's findings must meet or exceed to block a pull
// (design.md Decision 1).
type ScanPolicySettings struct {
	Enabled           bool      `json:"enabled"`
	SeverityThreshold string    `json:"severity_threshold"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// TrustedIdentity is one keyless (Fulcio/OIDC) trust anchor: a certificate
// Subject Alternative Name regexp paired with the required OIDC issuer
// (signing-keyless-verification design.md Interfaces/Contracts). A signature
// matches this anchor only when BOTH the SAN regexp and the issuer match --
// unlike TrustedPublicKeys, which needs no such pairing.
type TrustedIdentity struct {
	CertificateIdentityRegexp string `json:"certificate_identity_regexp"`
	CertificateOIDCIssuer     string `json:"certificate_oidc_issuer"`
}

// SigningPolicySettings is the global image-signature verification policy:
// whether the pull-time content-trust gate is enforced, and the set of
// public keys and/or trusted identities a signature may verify against (any
// one anchor of either kind is sufficient -- anchors never combine with AND).
type SigningPolicySettings struct {
	Enabled           bool              `json:"enabled"`
	TrustedPublicKeys []string          `json:"trusted_public_keys"` // canonical PEM, ECDSA P-256
	TrustedIdentities []TrustedIdentity `json:"trusted_identities"`
	UpdatedAt         time.Time         `json:"updated_at"`

	// UnsignedSelfRead is an explicitly opt-in exemption from the pull-time
	// fail-closed gate, letting a principal read back a manifest it cannot
	// yet prove is signed -- the cosign bootstrap chicken-and-egg (cosign
	// must GET the manifest to know what to sign). One of "" / "off" (no
	// exemption, the default -- unchanged current behavior), "pusher" (only
	// the exact principal who pushed this exact digest, by
	// domainauth.Principal.UserID), or "repo_push" (any principal with push
	// access to the repository). See ValidUnsignedSelfRead.
	UnsignedSelfRead string `json:"unsigned_self_read,omitempty"`
}

// SigningOverride is one repository's full replacement of the global signing
// policy (full-row-replace: present -> all fields apply). TrustedIdentities
// mirrors TrustedPublicKeys exactly: an override saved with keys but no
// identities clears any inherited global identities for that repository
// (signing-keyless-verification spec: "Per-Repository Trusted-Identity
// Override Is Full-Row-Replace").
type SigningOverride struct {
	Enabled           bool              `json:"enabled"`
	TrustedPublicKeys []string          `json:"trusted_public_keys,omitempty"`
	TrustedIdentities []TrustedIdentity `json:"trusted_identities,omitempty"`

	// UnsignedSelfRead mirrors SigningPolicySettings.UnsignedSelfRead --
	// full-row-replace, so an override with UnsignedSelfRead: "" legitimately
	// forces "off" for this repository, exactly like Enabled: false already
	// does for that field.
	UnsignedSelfRead string `json:"unsigned_self_read,omitempty"`
}

// UpdateChannelStable/UpdateChannelInsider mirror
// internal/infra/release.ChannelStable/ChannelInsider byte-for-byte.
// internal/ports cannot import internal/infra/release (that direction
// would invert this codebase's layering: infra depends on ports, never the
// reverse), so the two literal values are duplicated here rather than
// imported -- keep them in sync if either changes.
const (
	UpdateChannelStable  = "stable"
	UpdateChannelInsider = "insider"
)

// UpdateChannelSettings is the global (single-row, no per-tenant/repository
// concept) setting selecting which released regixtry tags the TUI's
// background update-check banner considers. Read is always public/
// unauthenticated (GET /update-channel, outside /admin/v1) so an
// unauthenticated pre-login TUI session can still know which channel to
// check against; write is admin-gated (PUT /admin/v1/update-channel).
type UpdateChannelSettings struct {
	Channel   string    `json:"channel"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValidUpdateChannel reports whether value is exactly UpdateChannelStable or
// UpdateChannelInsider -- never coerced, never case-insensitive, mirroring
// ValidUnsignedSelfRead's own exact-match discipline.
func ValidUpdateChannel(value string) bool {
	switch value {
	case UpdateChannelStable, UpdateChannelInsider:
		return true
	default:
		return false
	}
}

// ValidUnsignedSelfRead reports whether value is one of
// SigningPolicySettings.UnsignedSelfRead / SigningOverride.UnsignedSelfRead's
// exact allowed values: "" and "off" both mean no exemption, "pusher" and
// "repo_push" are the two opt-in exemption modes. Any other value is
// rejected at write time -- never silently coerced or ignored.
func ValidUnsignedSelfRead(value string) bool {
	switch value {
	case "", "off", "pusher", "repo_push":
		return true
	default:
		return false
	}
}

type ScanSettings struct {
	Enabled               bool          `json:"enabled"`
	ScheduleEnabled       bool          `json:"schedule_enabled"`
	Interval              time.Duration `json:"-"`
	Timeout               time.Duration `json:"-"`
	ServiceURL            string        `json:"service_url"`
	RegistryReachableURL  string        `json:"registry_reachable_url"`
	AuthToken             string        `json:"-"`
	TLSCACertPath         string        `json:"tls_ca_cert_path,omitempty"`
	TLSInsecureSkipVerify bool          `json:"tls_insecure_skip_verify,omitempty"`
	CacheDir              string        `json:"-"`
	BinaryPath            string        `json:"-"`
	LegacyCacheDir        string        `json:"-"`
	LegacyBinaryPath      string        `json:"-"`
	MaxConcurrency        int           `json:"max_concurrency"`
	UpdatedAt             time.Time     `json:"updated_at,omitempty"`

	// Per-repository override projection (resolved per run, never
	// persisted): design.md Decision 4. Absent from every SQL statement in
	// this store, so a stray UpsertScanSettings of a resolved struct cannot
	// leak them into the global row.
	IgnoreFilePath   string `json:"-"` // trivy --ignorefile
	IgnorePolicyPath string `json:"-"` // trivy --ignore-policy
	ConfigPath       string `json:"-"` // gitleaks --config
}

// TrivyOverride is one repository's full replacement of the global Trivy
// scan configuration (full-row-replace: present -> all fields apply).
type TrivyOverride struct {
	Enabled          bool   `json:"enabled"`
	IgnoreFilePath   string `json:"ignore_file_path,omitempty"`
	IgnorePolicyPath string `json:"ignore_policy_path,omitempty"`
}

// GitleaksOverride is one repository's full replacement of the global
// gitleaks scan configuration (full-row-replace: present -> all fields
// apply).
type GitleaksOverride struct {
	Enabled    bool   `json:"enabled"`
	ConfigPath string `json:"config_path,omitempty"`
}

// RepositoryFeatureOverride is the list/API projection of one stored
// override row. The resolution path never uses it — it decodes the raw
// payload directly via the codec registry (design.md Decision 3).
type RepositoryFeatureOverride struct {
	Repository string          `json:"repository"`
	Feature    string          `json:"feature"`
	Payload    json.RawMessage `json:"payload"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// RepositoryOverrideDetails is the flattened wire projection of one
// repository override row, used by the TUI admin client (design.md Decision
// 8): the union of TrivyOverride's, GitleaksOverride's, and SigningOverride's
// own JSON fields plus the repository/feature identity, matching exactly
// what repositoryOverrideResponse (admin_handlers.go) puts on the wire for
// GET/PUT/the list endpoint. Only the fields relevant to Feature are
// populated by the server.
type RepositoryOverrideDetails struct {
	Repository        string   `json:"repository"`
	Feature           string   `json:"feature"`
	Enabled           bool     `json:"enabled"`
	IgnoreFilePath    string   `json:"ignore_file_path,omitempty"`
	IgnorePolicyPath  string   `json:"ignore_policy_path,omitempty"`
	ConfigPath        string   `json:"config_path,omitempty"`
	TrustedPublicKeys []string `json:"trusted_public_keys,omitempty"`
	// TrustedIdentities mirrors TrustedPublicKeys -- signing feature only,
	// see SigningOverride's doc comment (signing-keyless-verification
	// design.md's File Changes table: "TrustedIdentity; field on both
	// settings types + RepositoryOverrideDetails").
	TrustedIdentities []TrustedIdentity `json:"trusted_identities,omitempty"`
	// UnsignedSelfRead mirrors SigningOverride.UnsignedSelfRead -- signing
	// feature only, see that type's doc comment.
	UnsignedSelfRead string    `json:"unsigned_self_read,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ScanResult struct {
	Critical     int
	High         int
	Medium       int
	Low          int
	TrivyVersion string
	DBUpdatedAt  *time.Time
	Findings     []ScanRunFinding
	DBFreshness  ScanRunDBFreshness
}

const (
	ScanRunDBFreshnessStateFresh   = "fresh"
	ScanRunDBFreshnessStateStale   = "stale"
	ScanRunDBFreshnessStateUnknown = "unknown"

	ScanReferenceFreshnessCurrent = "current"
	ScanReferenceFreshnessMoved   = "moved"
	ScanReferenceFreshnessMissing = "missing"
	ScanReferenceFreshnessUnknown = "unknown"
)

type ScanRunFinding struct {
	Target           string     `json:"target,omitempty"`
	Class            string     `json:"class,omitempty"`
	Type             string     `json:"type,omitempty"`
	Severity         string     `json:"severity,omitempty"`
	VulnerabilityID  string     `json:"vulnerability_id,omitempty"`
	PackageName      string     `json:"package_name,omitempty"`
	InstalledVersion string     `json:"installed_version,omitempty"`
	FixedVersion     string     `json:"fixed_version,omitempty"`
	Title            string     `json:"title,omitempty"`
	PrimaryURL       string     `json:"primary_url,omitempty"`
	Fixable          bool       `json:"fixable"`
	Status           string     `json:"status,omitempty"`
	DataSource       string     `json:"data_source,omitempty"`
	DataSourceURL    string     `json:"data_source_url,omitempty"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	ModifiedAt       *time.Time `json:"modified_at,omitempty"`
}

type ScanRunDBFreshness struct {
	ReportSchemaVersion int        `json:"report_schema_version"`
	ReportCreatedAt     *time.Time `json:"report_created_at,omitempty"`
	TrivyVersion        string     `json:"trivy_version,omitempty"`
	DBVersion           int        `json:"db_version"`
	DBUpdatedAt         *time.Time `json:"db_updated_at,omitempty"`
	DBDownloadedAt      *time.Time `json:"db_downloaded_at,omitempty"`
	DBNextUpdateAt      *time.Time `json:"db_next_update_at,omitempty"`
	FreshnessState      string     `json:"freshness_state,omitempty"`
}

type ScanRunDetail struct {
	Run                ScanRun            `json:"run"`
	Findings           []ScanRunFinding   `json:"findings,omitempty"`
	DBFreshness        ScanRunDBFreshness `json:"db_freshness"`
	ReferenceFreshness string             `json:"reference_freshness,omitempty"`
}

type ScanRun struct {
	ID           string     `json:"id"`
	Repository   string     `json:"repository"`
	RequestedRef string     `json:"requested_ref"`
	Digest       string     `json:"digest"`
	Status       string     `json:"status"`
	Trigger      string     `json:"trigger"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at,omitempty"`
	Critical     int        `json:"critical"`
	High         int        `json:"high"`
	Medium       int        `json:"medium"`
	Low          int        `json:"low"`
	TrivyVersion string     `json:"trivy_version,omitempty"`
	DBUpdatedAt  *time.Time `json:"db_updated_at,omitempty"`
	Error        string     `json:"error,omitempty"`
	HasFixable   bool       `json:"-"`
}

// RepositoryScanSummary is one repository's most recent scan run plus its
// total run count -- exactly one row per repository, returned by
// ListLatestScanRunPerRepository. Unlike ListScanRuns (whose limit applies
// to raw scan_runs rows, letting a few heavily-rescanned repositories crowd
// every other repository out of the window entirely),
// ListLatestScanRunPerRepository collapses to one row per repository BEFORE
// applying limit, so no repository can be hidden by another's rescans.
type RepositoryScanSummary struct {
	Run      ScanRun `json:"run"`
	RunCount int     `json:"run_count"`
}

type ScanSchedulerState struct {
	OwnerID         string     `json:"owner_id"`
	LeaseExpiresAt  time.Time  `json:"lease_expires_at"`
	LastHeartbeatAt time.Time  `json:"last_heartbeat_at"`
	BatchStartedAt  *time.Time `json:"batch_started_at,omitempty"`
}

type ScanRunner interface {
	Run(ctx context.Context, imageRef string, settings ScanSettings) (ScanResult, error)
}

// SecretScanTarget describes the manifest blobs a SecretScanRunner must
// stage and scan for one run: the config blob plus every layer referenced by
// the manifest currently under scan, in manifest order. No file path is
// carried here — BlobStore exposes no local path, so the runner stages each
// blob itself (design.md decision 7).
type SecretScanTarget struct {
	Repository string
	Digest     string
	Blobs      []domain.Descriptor
}

// SecretFinding is a redacted secret-scan finding: rule ID and location
// only. It deliberately has no field capable of holding the matched secret
// text, a match snippet, a fingerprint, or an entropy score (design.md
// decision 10) — a caller cannot leak what the type cannot represent.
type SecretFinding struct {
	RuleID      string   `json:"rule_id"`
	Description string   `json:"description,omitempty"`
	BlobDigest  string   `json:"blob_digest"`
	Path        string   `json:"path,omitempty"`
	StartLine   int      `json:"start_line"`
	EndLine     int      `json:"end_line"`
	Tags        []string `json:"tags,omitempty"`
}

type SecretScanResult struct {
	GitleaksVersion string          `json:"gitleaks_version,omitempty"`
	Findings        []SecretFinding `json:"findings,omitempty"`
	SkippedBlobs    []string        `json:"skipped_blobs,omitempty"`
}

type SecretScanRunner interface {
	Run(ctx context.Context, target SecretScanTarget, settings ScanSettings) (SecretScanResult, error)
}

const (
	SecretScanRunStatusQueued    = "queued"
	SecretScanRunStatusRunning   = "running"
	SecretScanRunStatusCompleted = "completed"
	SecretScanRunStatusFailed    = "failed"
)

type SecretScanRun struct {
	ID              string     `json:"id"`
	Repository      string     `json:"repository"`
	Digest          string     `json:"digest"`
	Status          string     `json:"status"`
	Trigger         string     `json:"trigger"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at,omitempty"`
	GitleaksVersion string     `json:"gitleaks_version,omitempty"`
	FindingCount    int        `json:"finding_count"`
	Error           string     `json:"error,omitempty"`
}

type SecretScanRunDetail struct {
	Run      SecretScanRun   `json:"run"`
	Findings []SecretFinding `json:"findings,omitempty"`
}

type FeatureKind string

const (
	FeatureKindBuiltin         FeatureKind = "builtin"
	FeatureKindExternalBinary  FeatureKind = "external_binary"
	FeatureKindExternalService FeatureKind = "external_service"
)

type FeatureSummary struct {
	Name           string      `json:"name"`
	Kind           FeatureKind `json:"kind"`
	Enabled        bool        `json:"enabled"`
	Configured     bool        `json:"configured"`
	CurrentVersion string      `json:"current_version,omitempty"`
	LatestVersion  string      `json:"latest_version,omitempty"`
	UpdateStatus   string      `json:"update_status,omitempty"`
}

type FeaturePage struct {
	Summary  FeatureSummary   `json:"summary"`
	Header   []FeatureField   `json:"header,omitempty"`
	Sections []FeatureSection `json:"sections,omitempty"`
	Actions  []FeatureAction  `json:"actions,omitempty"`
}

type FeatureField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type FeatureSection struct {
	ID     string         `json:"id"`
	Title  string         `json:"title"`
	Kind   string         `json:"kind"`
	Fields []FeatureField `json:"fields,omitempty"`
	Rows   []FeatureRow   `json:"rows,omitempty"`
}

type FeatureRow struct {
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type FeatureAction struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	ConfirmTitle   string `json:"confirm_title,omitempty"`
	ConfirmMessage string `json:"confirm_message,omitempty"`
}

type FeatureActionResult struct {
	Message string `json:"message"`
}

type FeatureRuntime struct {
	Mode              string     `json:"mode,omitempty"`
	Status            string     `json:"status,omitempty"`
	Health            string     `json:"health,omitempty"`
	Version           string     `json:"version,omitempty"`
	LatestVersion     string     `json:"latest_version,omitempty"`
	UpdateStatus      string     `json:"update_status,omitempty"`
	Detail            string     `json:"detail,omitempty"`
	RollbackAvailable bool       `json:"rollback_available,omitempty"`
	ActiveBinaryPath  string     `json:"active_binary_path,omitempty"`
	ReceiptPath       string     `json:"receipt_path,omitempty"`
	LastVerifiedAt    *time.Time `json:"last_verified_at,omitempty"`
	LastHealthCheckAt *time.Time `json:"last_health_check_at,omitempty"`
	LastDBUpdatedAt   *time.Time `json:"last_db_updated_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
}

type FeatureRuntimeProgress struct {
	Stage  string `json:"stage"`
	Detail string `json:"detail,omitempty"`
}

type FeatureDetails struct {
	Name                  string         `json:"name"`
	Kind                  FeatureKind    `json:"kind"`
	Enabled               bool           `json:"enabled"`
	Configured            bool           `json:"configured"`
	ScheduleEnabled       bool           `json:"schedule_enabled"`
	Interval              time.Duration  `json:"-"`
	Timeout               time.Duration  `json:"-"`
	ServiceURL            string         `json:"service_url"`
	RegistryReachableURL  string         `json:"registry_reachable_url"`
	AuthToken             string         `json:"-"`
	TLSCACertPath         string         `json:"tls_ca_cert_path,omitempty"`
	TLSInsecureSkipVerify bool           `json:"tls_insecure_skip_verify,omitempty"`
	CacheDir              string         `json:"-"`
	BinaryPath            string         `json:"-"`
	MaxConcurrency        int            `json:"max_concurrency"`
	Runtime               FeatureRuntime `json:"runtime,omitempty"`
}

type FeatureRuntimeStatus string

const (
	FeatureRuntimeStatusUninstalled       FeatureRuntimeStatus = "uninstalled"
	FeatureRuntimeStatusInstalling        FeatureRuntimeStatus = "installing"
	FeatureRuntimeStatusReady             FeatureRuntimeStatus = "ready"
	FeatureRuntimeStatusDegraded          FeatureRuntimeStatus = "degraded"
	FeatureRuntimeStatusMigrationRequired FeatureRuntimeStatus = "migration-required"
)

const FeatureRuntimeModeManaged = "managed"

type FeatureRuntimeState struct {
	Status            FeatureRuntimeStatus `json:"status"`
	ActiveVersion     string               `json:"active_version,omitempty"`
	PreviousVersion   string               `json:"previous_version,omitempty"`
	ActiveBinaryPath  string               `json:"active_binary_path,omitempty"`
	CacheDir          string               `json:"cache_dir,omitempty"`
	ReceiptPath       string               `json:"receipt_path,omitempty"`
	MigrationHint     string               `json:"migration_hint,omitempty"`
	LastVerifiedAt    *time.Time           `json:"last_verified_at,omitempty"`
	LastHealthCheckAt *time.Time           `json:"last_health_check_at,omitempty"`
	LastDBUpdatedAt   *time.Time           `json:"last_db_updated_at,omitempty"`
	LastError         string               `json:"last_error,omitempty"`
	UpdatedAt         time.Time            `json:"updated_at,omitempty"`
}

type FeatureConfigureInput struct {
	Enabled               *bool
	ScheduleEnabled       *bool
	Interval              *time.Duration
	Timeout               *time.Duration
	ServiceURL            *string
	RegistryReachableURL  *string
	AuthToken             *string
	TLSCACertPath         *string
	TLSInsecureSkipVerify *bool
	CacheDir              *string
	BinaryPath            *string
	MaxConcurrency        *int
}

type ActionVerb string

const (
	ActionPull    ActionVerb = "pull"
	ActionPush    ActionVerb = "push"
	ActionDelete  ActionVerb = "delete"
	ActionCatalog ActionVerb = "catalog"
	ActionInspect ActionVerb = "inspect"
)

type Action struct {
	Verb       ActionVerb
	Repository string
	Principal  *domainauth.Principal
}

type Challenge struct {
	Scheme  string
	Realm   string
	Service string
	Scope   string
	Error   string
}

type AccessController interface {
	Authorize(ctx context.Context, action Action) error
	Challenge(action Action) Challenge
}

type principalContextKey struct{}

func ContextWithPrincipal(ctx context.Context, principal domainauth.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) *domainauth.Principal {
	principal, ok := ctx.Value(principalContextKey{}).(domainauth.Principal)
	if !ok {
		return nil
	}

	copy := principal
	return &copy
}

func (a Action) WithPrincipal(principal *domainauth.Principal) Action {
	a.Principal = principal
	return a
}

func (a Action) Scope() string {
	switch a.Verb {
	case ActionCatalog:
		return "regixtry:catalog:*"
	case ActionPull, ActionInspect:
		if a.Repository == "" {
			return ""
		}
		return "repository:" + a.Repository + ":pull"
	case ActionPush:
		if a.Repository == "" {
			return ""
		}
		return "repository:" + a.Repository + ":pull,push"
	case ActionDelete:
		if a.Repository == "" {
			return ""
		}
		return "repository:" + a.Repository + ":delete"
	default:
		return ""
	}
}

type TenantResolver interface {
	Resolve(ctx context.Context) string
}

type JobRunner interface {
	Run(ctx context.Context, jobName string, fn func(context.Context) error) error
}

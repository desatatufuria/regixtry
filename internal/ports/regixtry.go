package ports

import (
	"context"
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
	ListManifestBlobs(ctx context.Context, tenant string, repository domain.RepositoryRef, manifestDigest domain.Digest) ([]domain.Descriptor, error)
	GetScanSettings(ctx context.Context, tenant string, feature string) (ScanSettings, error)
	UpsertScanSettings(ctx context.Context, tenant string, feature string, settings ScanSettings) error
	GetScanPolicySettings(ctx context.Context, tenant string) (ScanPolicySettings, error)
	UpsertScanPolicySettings(ctx context.Context, tenant string, settings ScanPolicySettings) error
	GetFeatureRuntimeState(ctx context.Context, tenant string, feature string) (FeatureRuntimeState, error)
	UpsertFeatureRuntimeState(ctx context.Context, tenant string, feature string, state FeatureRuntimeState) error
	GetActiveScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ScanRun, error)
	GetLatestScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ScanRun, error)
	GetScanRun(ctx context.Context, tenant string, runID string) (ScanRun, error)
	GetScanRunDetail(ctx context.Context, tenant string, runID string) (ScanRunDetail, error)
	UpsertScanRun(ctx context.Context, tenant string, run ScanRun) error
	UpsertScanRunDetail(ctx context.Context, tenant string, detail ScanRunDetail) error
	ListScanRuns(ctx context.Context, tenant string, repository string, limit int) ([]ScanRun, error)
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
		return "registry:catalog:*"
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

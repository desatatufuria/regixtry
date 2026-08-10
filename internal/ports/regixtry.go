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
	GetScanSettings(ctx context.Context, tenant string) (ScanSettings, error)
	UpsertScanSettings(ctx context.Context, tenant string, settings ScanSettings) error
	GetTrivyRuntimeState(ctx context.Context, tenant string) (TrivyRuntimeState, error)
	UpsertTrivyRuntimeState(ctx context.Context, tenant string, state TrivyRuntimeState) error
	GetActiveScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ScanRun, error)
	GetScanRun(ctx context.Context, tenant string, runID string) (ScanRun, error)
	UpsertScanRun(ctx context.Context, tenant string, run ScanRun) error
	ListScanRuns(ctx context.Context, tenant string, repository string, limit int) ([]ScanRun, error)
	TryAcquireScanSchedulerLease(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) (bool, ScanSchedulerState, error)
	HeartbeatScanScheduler(ctx context.Context, tenant string, owner string, now time.Time, leaseTTL time.Duration) error
	GetScanSchedulerState(ctx context.Context, tenant string) (ScanSchedulerState, error)
	UpsertScanSchedulerState(ctx context.Context, tenant string, state ScanSchedulerState) error
}

const (
	ScanRunStatusQueued    = "queued"
	ScanRunStatusRunning   = "running"
	ScanRunStatusCompleted = "completed"
	ScanRunStatusFailed    = "failed"

	ScanTriggerManual    = "manual"
	ScanTriggerScheduled = "scheduled"
)

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

type TrivyRuntimeStatus string

const (
	TrivyRuntimeStatusUninstalled       TrivyRuntimeStatus = "uninstalled"
	TrivyRuntimeStatusInstalling        TrivyRuntimeStatus = "installing"
	TrivyRuntimeStatusReady             TrivyRuntimeStatus = "ready"
	TrivyRuntimeStatusDegraded          TrivyRuntimeStatus = "degraded"
	TrivyRuntimeStatusMigrationRequired TrivyRuntimeStatus = "migration-required"
)

const FeatureRuntimeModeManaged = "managed"

type TrivyRuntimeState struct {
	Status            TrivyRuntimeStatus `json:"status"`
	ActiveVersion     string             `json:"active_version,omitempty"`
	PreviousVersion   string             `json:"previous_version,omitempty"`
	ActiveBinaryPath  string             `json:"active_binary_path,omitempty"`
	CacheDir          string             `json:"cache_dir,omitempty"`
	ReceiptPath       string             `json:"receipt_path,omitempty"`
	MigrationHint     string             `json:"migration_hint,omitempty"`
	LastVerifiedAt    *time.Time         `json:"last_verified_at,omitempty"`
	LastHealthCheckAt *time.Time         `json:"last_health_check_at,omitempty"`
	LastDBUpdatedAt   *time.Time         `json:"last_db_updated_at,omitempty"`
	LastError         string             `json:"last_error,omitempty"`
	UpdatedAt         time.Time          `json:"updated_at,omitempty"`
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

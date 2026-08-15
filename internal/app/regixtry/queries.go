package regixtry

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/domain/signing"
	"regixtry/internal/ports"
)

type CatalogResult struct {
	Repositories []string `json:"repositories"`
}

type TagsResult struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type ManifestDetails struct {
	Repository  string            `json:"repository"`
	Reference   string            `json:"reference"`
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Blobs       []BlobDetails     `json:"blobs"`
}

// DeletionDetails names what a delete removed (design.md Decision 1).
// Digest is omitted on the tag path deliberately: resolving it would add a
// lookup that buys the caller nothing it did not already send.
// ManifestRemoved distinguishes the two semantics without the caller having
// to re-parse its own reference.
type DeletionDetails struct {
	Repository      string   `json:"repository"`
	Reference       string   `json:"reference"`
	Digest          string   `json:"digest,omitempty"`
	ManifestRemoved bool     `json:"manifestRemoved"`
	TagsRemoved     []string `json:"tagsRemoved"`
}

type BlobDetails struct {
	Repository string `json:"repository,omitempty"`
	MediaType  string `json:"mediaType,omitempty"`
	Digest     string `json:"digest"`
	Size       int64  `json:"size"`
}

type UploadDetails struct {
	Repository string    `json:"repository"`
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Size       int64     `json:"size"`
	StartedAt  time.Time `json:"startedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Location   string    `json:"location"`
}

func (s *Service) ResolveManifest(ctx context.Context, repositoryName string, reference string) (ManifestDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return ManifestDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return ManifestDetails{}, err
	}

	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
	if err != nil {
		return ManifestDetails{}, err
	}

	blobs, err := s.metadata.ListManifestBlobs(ctx, s.tenant(ctx), repository, manifest.Digest)
	if err != nil {
		return ManifestDetails{}, err
	}

	return newManifestDetails(repository.String(), reference, manifest, blobs), nil
}

func (s *Service) OpenManifest(ctx context.Context, repositoryName string, reference string) (domain.Manifest, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return domain.Manifest{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return domain.Manifest{}, err
	}

	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
	if err != nil {
		return domain.Manifest{}, err
	}

	if err := s.enforceScanPolicy(ctx, repository.String(), manifest.Digest.String()); err != nil {
		return domain.Manifest{}, err
	}
	if err := s.enforceSigningPolicy(ctx, repository.String(), manifest.Digest.String()); err != nil {
		return domain.Manifest{}, err
	}

	return manifest, nil
}

// ScanStatusResult is the CI-facing scan verdict for one digest
// (design.md Decision 5). Unlike OpenManifest, this never gates the
// request — it always returns 200 with the current verdict, one of five
// distinct states, so a polling caller can distinguish "wait" from "give
// up" instead of receiving a single boolean.
type ScanStatusResult struct {
	Repository     string           `json:"repository"`
	Reference      string           `json:"reference"`
	Digest         string           `json:"digest"`
	State          string           `json:"state"`
	WouldBlockPull bool             `json:"would_block_pull"`
	Policy         ScanStatusPolicy `json:"policy"`
	Scan           *ScanStatusScan  `json:"scan,omitempty"`
}

type ScanStatusPolicy struct {
	Enabled           bool   `json:"enabled"`
	SeverityThreshold string `json:"severity_threshold"`
}

type ScanStatusScan struct {
	Status     string     `json:"status"`
	Critical   int        `json:"critical"`
	High       int        `json:"high"`
	Medium     int        `json:"medium"`
	Low        int        `json:"low"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

const (
	ScanStatusUnscanned  = "unscanned"
	ScanStatusInProgress = "in_progress"
	ScanStatusFailed     = "failed"
	ScanStatusClean      = "clean"
	ScanStatusBlocked    = "blocked"
)

// ScanStatus resolves the same digest and policy the pull gate would
// (GetScanPolicySettings + GetLatestScanRunByDigest), but always returns a
// verdict rather than gating the caller — it reports whether a pull would
// be blocked, it is never subject to the gate itself. Authorization is
// ActionPull, the same action OpenManifest itself uses: "if you may pull
// it, you may learn why you cannot."
func (s *Service) ScanStatus(ctx context.Context, repositoryName string, reference string) (ScanStatusResult, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return ScanStatusResult{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return ScanStatusResult{}, err
	}

	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
	if err != nil {
		return ScanStatusResult{}, err
	}
	digest := manifest.Digest.String()

	settings, err := s.GetScanPolicySettings(ctx)
	if err != nil {
		return ScanStatusResult{}, err
	}
	result := ScanStatusResult{
		Repository: repository.String(),
		Reference:  reference,
		Digest:     digest,
		Policy:     ScanStatusPolicy{Enabled: settings.Enabled, SeverityThreshold: settings.SeverityThreshold},
	}

	run, err := s.metadata.GetLatestScanRunByDigest(ctx, s.tenant(ctx), repository.String(), digest)
	if err != nil {
		if !domain.IsCode(err, domain.ErrorCodeNotFound) {
			return ScanStatusResult{}, err
		}
		result.State = ScanStatusUnscanned
		return result, nil
	}

	result.Scan = &ScanStatusScan{Status: run.Status, Critical: run.Critical, High: run.High, Medium: run.Medium, Low: run.Low, FinishedAt: run.FinishedAt}
	switch run.Status {
	case ports.ScanRunStatusQueued, ports.ScanRunStatusRunning:
		result.State = ScanStatusInProgress
	case ports.ScanRunStatusFailed:
		result.State = ScanStatusFailed
	case ports.ScanRunStatusCompleted:
		if scanPolicyViolated(settings, run) {
			result.State = ScanStatusBlocked
			result.WouldBlockPull = true
		} else {
			result.State = ScanStatusClean
		}
	}
	return result, nil
}

// SignatureStatusResult is the CI-facing signature verdict for one digest
// (design.md Decision 9). Like ScanStatusResult, this never gates the
// request -- it always returns 200 with the current verdict, one of five
// distinct states, computed independently of whether the resolved policy is
// enabled. Only WouldBlockPull reflects the toggle.
type SignatureStatusResult struct {
	Repository     string                 `json:"repository"`
	Reference      string                 `json:"reference"`
	Digest         string                 `json:"digest"`
	State          string                 `json:"state"`
	WouldBlockPull bool                   `json:"would_block_pull"`
	Policy         SignatureStatusPolicy  `json:"policy"`
	Signature      *SignatureStatusDetail `json:"signature,omitempty"`
}

// SignatureStatusPolicy never carries key material -- TrustedKeys is a
// count only, the registry-scoped counterpart of the admin-only signing
// policy resource that echoes canonical PEM (design.md Decision 8/9).
type SignatureStatusPolicy struct {
	Enabled     bool `json:"enabled"`
	TrustedKeys int  `json:"trusted_keys"`
}

// SignatureStatusDetail never carries a raw signature -- Reason is drawn
// from enforceSigningPolicy's own fixed-vocabulary messages (design.md
// Decision 6), stripped of its "pull of <repo>@<digest> is blocked by the
// signing policy: " prefix.
type SignatureStatusDetail struct {
	Tag            string `json:"tag"`
	SignatureCount int    `json:"signature_count"`
	Reason         string `json:"reason,omitempty"`
}

const (
	SignatureStatusUnsigned     = "unsigned"
	SignatureStatusUnverifiable = "unverifiable"
	SignatureStatusUntrusted    = "untrusted"
	SignatureStatusMismatched   = "mismatched"
	SignatureStatusVerified     = "verified"
)

// SignatureStatus resolves the same digest and policy the signing gate
// would (GetSigningPolicySettings + applySigningRepositoryOverride +
// verifySignature), but always returns a verdict rather than gating the
// caller -- it reports whether a pull would be blocked, it is never subject
// to the gate itself. Authorization is ActionPull, the same action
// OpenManifest itself uses, exactly like ScanStatus: "if you may pull it,
// you may learn why you cannot." The state is computed unconditionally,
// even when the resolved policy is disabled (design.md Decision 9) --
// verifySignature is called directly, bypassing enforceSigningPolicy's
// `!policy.Enabled` short-circuit.
func (s *Service) SignatureStatus(ctx context.Context, repositoryName string, reference string) (SignatureStatusResult, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return SignatureStatusResult{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return SignatureStatusResult{}, err
	}

	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
	if err != nil {
		return SignatureStatusResult{}, err
	}
	digest := manifest.Digest.String()

	policy, err := s.GetSigningPolicySettings(ctx)
	if err != nil {
		return SignatureStatusResult{}, err
	}
	policy, err = s.applySigningRepositoryOverride(ctx, s.tenant(ctx), repository.String(), policy)
	if err != nil {
		return SignatureStatusResult{}, err
	}

	result := SignatureStatusResult{
		Repository: repository.String(),
		Reference:  reference,
		Digest:     digest,
		Policy:     SignatureStatusPolicy{Enabled: policy.Enabled, TrustedKeys: len(policy.TrustedPublicKeys)},
	}

	state, verifyErr := s.verifySignature(ctx, repository.String(), digest, policy)
	if verifyErr != nil && !domain.IsCode(verifyErr, domain.ErrorCodePolicyViolation) {
		return SignatureStatusResult{}, verifyErr // infrastructure error propagates unchanged, same as the gate
	}
	result.State = state
	result.WouldBlockPull = policy.Enabled && state != SignatureStatusVerified

	if state != SignatureStatusUnsigned {
		tag, entries, resolveErr := s.resolveSignatureManifestEntries(ctx, repository.String(), digest)
		if resolveErr != nil {
			return SignatureStatusResult{}, resolveErr
		}
		result.Signature = &SignatureStatusDetail{
			Tag:            tag,
			SignatureCount: len(entries),
			Reason:         signatureStatusReason(repository.String(), digest, verifyErr),
		}
	}

	return result, nil
}

// resolveSignatureManifestEntries re-resolves the .sig manifest's parsed
// entries for SignatureStatusDetail's Tag/SignatureCount fields -- a small,
// deliberate duplication of verifySignature's own resolution (this is a
// status read, not the hot pull path, so re-reading is an acceptable cost
// for keeping verifySignature's signature unchanged from Work Unit 3). A
// missing or unparseable manifest is reported as zero entries, never an
// error, mirroring verifySignature's own tolerance for the same conditions.
func (s *Service) resolveSignatureManifestEntries(ctx context.Context, repository string, digest string) (string, []signing.SignatureEntry, error) {
	tag, err := signing.SignatureTag(digest)
	if err != nil {
		return "", nil, nil
	}

	repositoryRef, err := parseRepository(repository)
	if err != nil {
		return tag, nil, nil
	}

	sigManifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repositoryRef, tag)
	if err != nil {
		if domain.IsCode(err, domain.ErrorCodeNotFound) {
			return tag, nil, nil
		}
		return tag, nil, err // infrastructure error propagates unchanged
	}

	entries, err := signing.ParseSignatureManifest(sigManifest.Payload)
	if err != nil {
		return tag, nil, nil
	}
	return tag, entries, nil
}

// signatureStatusReason extracts the fixed-vocabulary reason from
// verifySignature's own error message, trimming the
// "pull of <repo>@<digest> is blocked by the signing policy: " prefix every
// verifySignature error carries. Reusing that message (rather than
// duplicating its vocabulary) keeps the gate and the status report unable
// to drift apart; none of verifySignature's messages ever embed key,
// payload, or signature bytes.
func signatureStatusReason(repository string, digest string, err error) string {
	if err == nil {
		return ""
	}
	prefix := fmt.Sprintf("pull of %s@%s is blocked by the signing policy: ", repository, digest)
	return strings.TrimPrefix(err.Error(), prefix)
}

// SecretScanStatusResult is the CI-facing secret-scan verdict for one digest
// (design.md Decision 1-4). Like ScanStatusResult/SignatureStatusResult, this
// never gates the request -- gitleaks findings never block a pull or a push
// (service_scanning.go:324-328), so there is no WouldBlockPull field and no
// severity-threshold field.
type SecretScanStatusResult struct {
	Repository string                 `json:"repository"`
	Reference  string                 `json:"reference"`
	Digest     string                 `json:"digest"`
	State      string                 `json:"state"`
	Policy     SecretScanStatusPolicy `json:"policy"`
	Scan       *SecretScanStatusScan  `json:"scan,omitempty"`
}

// SecretScanStatusPolicy carries only the enabled toggle -- gitleaks has no
// severity-threshold gate to configure, unlike ScanStatusPolicy's Trivy
// counterpart (design.md Decision 2).
type SecretScanStatusPolicy struct {
	Enabled bool `json:"enabled"`
}

// SecretScanStatusScan never carries per-finding detail -- ports.SecretFinding
// is never referenced here (design.md threat matrix, capability/data
// disclosure boundary).
type SecretScanStatusScan struct {
	Status       string     `json:"status"`
	FindingCount int        `json:"finding_count"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

const (
	SecretScanStatusUnscanned       = "unscanned"
	SecretScanStatusInProgress      = "in_progress"
	SecretScanStatusFailed          = "failed"
	SecretScanStatusClean           = "clean"
	SecretScanStatusFindingsPresent = "findings_present"
)

// SecretScanStatus resolves the verdict via the same query path
// GetSecretScanFindings uses -- ListSecretScanRuns followed by a digest
// match -- and deliberately never calls GetActiveSecretScanRunByDigest,
// which filters to queued/running rows only and would misreport a completed
// scan as unscanned (design.md Decision 1). Authorization is ActionPull, the
// same action ScanStatus/SignatureStatus use: "if you may pull it, you may
// learn why you cannot."
func (s *Service) SecretScanStatus(ctx context.Context, repositoryName string, reference string) (SecretScanStatusResult, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return SecretScanStatusResult{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repository.String()}); err != nil {
		return SecretScanStatusResult{}, err
	}

	manifest, err := s.metadata.ResolveManifest(ctx, s.tenant(ctx), repository, reference)
	if err != nil {
		return SecretScanStatusResult{}, err
	}
	digest := manifest.Digest.String()

	// GetScanSettings(gitleaks) returns domain.ErrorCodeNotFound on zero
	// rows -- a real case in fresh installs/tests before EnsureScanSettings
	// has run for gitleaks. Unlike GetScanPolicySettings's fail-open-to-true
	// default (a security gate that must never silently turn off), gitleaks
	// is informational only, so NotFound defaults to Enabled: false: safer
	// to under-report than to claim scanning is on when no row says so
	// (design.md Decision 2).
	settings, err := s.metadata.GetScanSettings(ctx, s.tenant(ctx), gitleaksFeatureName)
	if err != nil {
		if !domain.IsCode(err, domain.ErrorCodeNotFound) {
			return SecretScanStatusResult{}, err
		}
		settings = ports.ScanSettings{Enabled: false}
	}
	settings, err = s.applyRepositoryOverride(ctx, s.tenant(ctx), repository.String(), gitleaksFeatureName, settings)
	if err != nil {
		return SecretScanStatusResult{}, err
	}

	result := SecretScanStatusResult{
		Repository: repository.String(),
		Reference:  reference,
		Digest:     digest,
		Policy:     SecretScanStatusPolicy{Enabled: settings.Enabled},
	}

	// Deliberately ListSecretScanRuns + digest-match, not
	// GetActiveSecretScanRunByDigest -- the latter filters to queued/running
	// rows only and would misreport a completed scan as unscanned
	// (design.md Decision 1). Mirrors GetSecretScanFindings's own lookup.
	runs, err := s.metadata.ListSecretScanRuns(ctx, s.tenant(ctx), repository.String(), 50)
	if err != nil {
		return SecretScanStatusResult{}, err
	}
	for _, run := range runs {
		if run.Digest != digest {
			continue
		}
		result.Scan = &SecretScanStatusScan{Status: run.Status, FindingCount: run.FindingCount, FinishedAt: run.FinishedAt}
		switch run.Status {
		case ports.SecretScanRunStatusQueued, ports.SecretScanRunStatusRunning:
			result.State = SecretScanStatusInProgress
		case ports.SecretScanRunStatusFailed:
			result.State = SecretScanStatusFailed
		case ports.SecretScanRunStatusCompleted:
			if run.FindingCount > 0 {
				result.State = SecretScanStatusFindingsPresent
			} else {
				result.State = SecretScanStatusClean
			}
		}
		return result, nil
	}

	result.State = SecretScanStatusUnscanned
	return result, nil
}

func (s *Service) Catalog(ctx context.Context, limit int, after string) (CatalogResult, error) {
	action := ports.Action{Verb: ports.ActionCatalog}
	if err := s.authorize(ctx, action); err != nil {
		return CatalogResult{}, err
	}

	repositories, err := s.metadata.Catalog(ctx, s.tenant(ctx), limit, after)
	if err != nil {
		return CatalogResult{}, err
	}

	principal := ports.PrincipalFromContext(ctx)
	result := CatalogResult{Repositories: make([]string, 0, len(repositories))}
	for _, repository := range repositories {
		if principal != nil && !principal.HasCatalogAccess(repository.String()) {
			continue
		}
		result.Repositories = append(result.Repositories, repository.String())
	}

	return result, nil
}

func (s *Service) Tags(ctx context.Context, repositoryName string, limit int, after string) (TagsResult, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return TagsResult{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect, Repository: repository.String()}); err != nil {
		return TagsResult{}, err
	}

	tags, err := s.metadata.ListTags(ctx, s.tenant(ctx), repository, limit, after)
	if err != nil {
		return TagsResult{}, err
	}

	return TagsResult{Name: repository.String(), Tags: tags}, nil
}

// TagDetails is one tag's row on the Console TUI's Tags screen: its name,
// its manifest's created_at, and its computed signature state (design.md
// intentionally minimal 3-column set: Tag, Created, Signed -- vulnerability
// and size data already live elsewhere in this app). Kept distinct from
// TagsResult (the OCI Distribution API's `_tags/list` shape, `{name, tags:
// []string}`), which existing docker/skopeo clients depend on and must
// never gain extra fields.
type TagDetails struct {
	Name           string    `json:"name"`
	CreatedAt      time.Time `json:"created_at"`
	SignatureState string    `json:"signature_state"`
	SigningEnabled bool      `json:"signing_enabled"`
}

// isCosignSignatureArtifactTag reports whether name is the cosign legacy
// signature tag for some digest -- i.e. matches SignatureTag's own
// "sha256-<hex>.sig" format. It reuses SignatureTag itself for the actual
// hex/length validation (round-tripping name back through the digest form
// and confirming SignatureTag reproduces it exactly) rather than hand-
// rolling a second pattern matcher, per this change's own scope: a
// signature's accessory `.sig` tag is not a version a Console user
// browses/pulls, and its own "Signed" status would be nonsensical (a
// signature isn't itself signed).
func isCosignSignatureArtifactTag(name string) bool {
	const prefix = "sha256-"
	const suffix = ".sig"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}

	hexPart := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	produced, err := signing.SignatureTag("sha256:" + hexPart)
	return err == nil && produced == name
}

// TagDetails resolves the same tag list Tags() does, plus each tag's
// manifest created_at and computed signature state (console-tags-table
// change) -- authorized identically to Tags() (ActionInspect), which shares
// ActionPull's scope (Action.Scope()), so the per-tag SignatureStatus call
// below composes without a second, different authorization boundary.
// SignatureStatus is reused verbatim rather than reimplemented (design
// intent): each tag pays its own ResolveManifest + verifySignature cost,
// mirroring this codebase's existing per-item lookup pattern rather than a
// bespoke batch-verification query.
//
// Unlike Tags() (the OCI Distribution API's `_tags/list` endpoint, which
// existing docker/cosign/skopeo clients depend on and must keep seeing
// every tag including `.sig` artifacts unfiltered), TagDetails excludes
// cosign `.sig` signature-artifact tags: this is the human-facing Console
// browsing view, and a signature's own accessory artifact is not a version
// a user browses or pulls like `latest` or `v1.2.3`.
func (s *Service) TagDetails(ctx context.Context, repositoryName string, limit int, after string) ([]TagDetails, error) {
	repository, err := parseRepository(repositoryName)
	if err != nil {
		return nil, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect, Repository: repository.String()}); err != nil {
		return nil, err
	}

	tags, err := s.metadata.ListTagsWithCreatedAt(ctx, s.tenant(ctx), repository, limit, after)
	if err != nil {
		return nil, err
	}

	details := make([]TagDetails, 0, len(tags))
	for _, tag := range tags {
		if isCosignSignatureArtifactTag(tag.Name) {
			continue
		}
		status, err := s.SignatureStatus(ctx, repository.String(), tag.Name)
		if err != nil {
			return nil, err
		}
		details = append(details, TagDetails{
			Name:           tag.Name,
			CreatedAt:      tag.CreatedAt,
			SignatureState: status.State,
			SigningEnabled: status.Policy.Enabled,
		})
	}

	return details, nil
}

// RepositorySummary is the Console TUI's top-level Repositories screen's
// per-row shape (mirrors TagDetails' own per-tag shape, one level up):
// deliberately NOT part of the OCI Distribution API's CatalogResult wire
// format (`{repositories: []string}`), which existing docker/skopeo clients
// depend on and must never gain extra fields.
type RepositorySummary struct {
	Name       string    `json:"name"`
	TagCount   int       `json:"tag_count"`
	LastPushed time.Time `json:"last_pushed"`
}

// RepositorySummaries returns every repository's name, tag count, and most
// recent push time, backing the Console TUI's Repositories table (Name |
// Tags | Last Pushed columns). Authorization exactly mirrors Catalog's own
// (ActionCatalog plus per-repository HasCatalogAccess filtering) -- this is
// Catalog's own data with two extra aggregate columns, not a different
// access surface, so it must never be stricter or looser than Catalog.
func (s *Service) RepositorySummaries(ctx context.Context, limit int, after string) ([]RepositorySummary, error) {
	action := ports.Action{Verb: ports.ActionCatalog}
	if err := s.authorize(ctx, action); err != nil {
		return nil, err
	}

	summaries, err := s.metadata.ListRepositoriesWithSummary(ctx, s.tenant(ctx), limit, after)
	if err != nil {
		return nil, err
	}

	principal := ports.PrincipalFromContext(ctx)
	result := make([]RepositorySummary, 0, len(summaries))
	for _, summary := range summaries {
		if principal != nil && !principal.HasCatalogAccess(summary.Name) {
			continue
		}
		result = append(result, RepositorySummary{
			Name:       summary.Name,
			TagCount:   summary.TagCount,
			LastPushed: summary.LastPushed,
		})
	}

	return result, nil
}

func (s *Service) InspectBlob(ctx context.Context, repositoryName string, digestValue string) (BlobDetails, error) {
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return BlobDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repositoryName}); err != nil {
		return BlobDetails{}, err
	}

	reader, descriptor, err := s.blobs.OpenBlob(ctx, digest)
	if err != nil {
		return BlobDetails{}, err
	}
	defer reader.Close()

	return newBlobDetails(repositoryName, descriptor), nil
}

func (s *Service) OpenBlob(ctx context.Context, repositoryName string, digestValue string) (io.ReadSeekCloser, BlobDetails, error) {
	digest, err := domain.ParseDigest(digestValue)
	if err != nil {
		return nil, BlobDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPull, Repository: repositoryName}); err != nil {
		return nil, BlobDetails{}, err
	}

	reader, descriptor, err := s.blobs.OpenBlob(ctx, digest)
	if err != nil {
		return nil, BlobDetails{}, err
	}

	return reader, newBlobDetails(repositoryName, descriptor), nil
}

func (s *Service) UploadStatus(ctx context.Context, repositoryName string, uploadID string) (UploadDetails, error) {
	state, err := s.ensureUpload(ctx, repositoryName, uploadID)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.authorize(ctx, ports.Action{Verb: ports.ActionPush, Repository: state.Repository.String()}); err != nil {
		return UploadDetails{}, err
	}

	state, err = s.blobs.GetUpload(ctx, uploadID)
	if err != nil {
		return UploadDetails{}, err
	}

	if err := s.metadata.SaveUpload(ctx, s.tenant(ctx), state); err != nil {
		return UploadDetails{}, err
	}

	return newUploadDetails(state), nil
}

func (s *Service) Uploads(ctx context.Context, repositoryName string) ([]UploadDetails, error) {
	var repository *domain.RepositoryRef
	if repositoryName != "" {
		parsed, err := parseRepository(repositoryName)
		if err != nil {
			return nil, err
		}
		repository = &parsed

		if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect, Repository: parsed.String()}); err != nil {
			return nil, err
		}
	} else if err := s.authorize(ctx, ports.Action{Verb: ports.ActionInspect}); err != nil {
		return nil, err
	}

	uploads, err := s.metadata.ListUploads(ctx, s.tenant(ctx), repository)
	if err != nil {
		return nil, err
	}

	result := make([]UploadDetails, 0, len(uploads))
	for _, upload := range uploads {
		result = append(result, newUploadDetails(upload))
	}

	return result, nil
}

func newManifestDetails(repository string, reference string, manifest domain.Manifest, blobs []domain.Descriptor) ManifestDetails {
	details := ManifestDetails{
		Repository:  repository,
		Reference:   reference,
		MediaType:   manifest.MediaType,
		Digest:      manifest.Digest.String(),
		Size:        manifest.Size,
		Annotations: manifest.Annotations,
		Blobs:       make([]BlobDetails, 0, len(blobs)),
	}

	for _, blob := range blobs {
		details.Blobs = append(details.Blobs, newBlobDetails(repository, blob))
	}

	return details
}

// newDeletionDetailsForDigest builds the digest-path DeletionDetails:
// ManifestRemoved is always true (the manifests row is gone), and
// tagsRemoved is the exact cascade blast radius the store selected inside
// the same transaction, immediately before the delete (design.md Decision 1).
func newDeletionDetailsForDigest(repository string, reference string, digest string, tagsRemoved []string) DeletionDetails {
	return DeletionDetails{
		Repository:      repository,
		Reference:       reference,
		Digest:          digest,
		ManifestRemoved: true,
		TagsRemoved:     tagsRemoved,
	}
}

// newDeletionDetailsForTag builds the tag-path DeletionDetails: the manifest
// and every other tag on it survive, so ManifestRemoved is always false and
// TagsRemoved names only the one tag that was untagged.
func newDeletionDetailsForTag(repository string, reference string) DeletionDetails {
	return DeletionDetails{
		Repository:      repository,
		Reference:       reference,
		ManifestRemoved: false,
		TagsRemoved:     []string{reference},
	}
}

func newBlobDetails(repository string, descriptor domain.Descriptor) BlobDetails {
	return BlobDetails{
		Repository: repository,
		MediaType:  descriptor.MediaType,
		Digest:     descriptor.Digest.String(),
		Size:       descriptor.Size,
	}
}

func newUploadDetails(state domain.UploadState) UploadDetails {
	return UploadDetails{
		Repository: state.Repository.String(),
		ID:         state.ID,
		Status:     string(state.Status),
		Size:       state.Size,
		StartedAt:  state.StartedAt,
		UpdatedAt:  state.UpdatedAt,
		Location:   state.Location,
	}
}

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

package regixtry

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// gcDeleteDisabledMessage names the exact flag (design.md D9): admin routes
// carry no OCI error code vocabulary, so this message is the only
// disambiguator available to an operator reading the response body.
const gcDeleteDisabledMessage = "blob garbage collection delete is disabled (REGISTRY_GC_DELETE_ENABLED)"

// gcGraceWindow (D3) is the ONLY thing standing between a blob written by an
// in-flight push and an irreversible unlink: a push writes blobs first and
// publishes the manifest last, so between CommitUpload and PublishManifest a
// perfectly live blob is unreferenced. It is a fixed constant on purpose --
// it must never become settable to 0.
const gcGraceWindow = 24 * time.Hour

// gcReportTTL (D8) is HYGIENE, NOT SAFETY. It stops months-old reports from
// being submitted and stops preview rows accumulating. The safety mechanism
// is DeleteByGCReport's recompute-and-intersect, which can only under-delete.
const gcReportTTL = 24 * time.Hour

// gcCandidates is THE mark-and-sweep computation. Report and delete both call
// it; there is deliberately no second implementation that could drift, which
// is what makes the report a faithful preview by construction (D1). A blob
// is a candidate iff it is unmarked (not in ListReferencedBlobDigests, a
// GLOBAL query with no tenant predicate -- design.md Decision C) AND its
// mtime is older than the grace cutoff. Candidates are sorted by digest for
// a deterministic gc_report_candidates position.
func (s *Service) gcCandidates(ctx context.Context, now time.Time) ([]ports.GCReportCandidate, time.Time, error) {
	cutoff := now.Add(-gcGraceWindow)

	blobs, err := s.blobs.ListBlobs(ctx)
	if err != nil {
		return nil, cutoff, err
	}

	marked, err := s.metadata.ListReferencedBlobDigests(ctx)
	if err != nil {
		return nil, cutoff, err
	}
	markSet := make(map[string]struct{}, len(marked))
	for _, digest := range marked {
		markSet[digest] = struct{}{}
	}

	candidates := make([]ports.GCReportCandidate, 0)
	for _, blob := range blobs {
		if _, referenced := markSet[blob.Digest.String()]; referenced {
			continue
		}
		if !blob.ModTime.Before(cutoff) {
			continue
		}
		candidates = append(candidates, ports.GCReportCandidate{
			Digest:  blob.Digest.String(),
			Size:    blob.Size,
			ModTime: blob.ModTime,
		})
	}

	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Digest < candidates[j].Digest })

	return candidates, cutoff, nil
}

// ComputeGCReport is the report call: gcGate bounds it to a single in-flight
// run (D2), expired preview reports are pruned first (D8 hygiene), then
// gcCandidates computes the candidate set and CreateGCReport persists it
// (report + candidates) in one transaction before this returns -- the report
// endpoint MUST be reachable regardless of REGISTRY_GC_DELETE_ENABLED, so
// this method never consults gcDeleteEnabled.
func (s *Service) ComputeGCReport(ctx context.Context) (ports.GCReportDetail, error) {
	if err := s.gcGate.acquire(ctx, 1); err != nil {
		return ports.GCReportDetail{}, err
	}
	defer s.gcGate.release()

	now := s.now()
	started := time.Now()

	if _, err := s.metadata.PruneExpiredGCReports(ctx, now); err != nil {
		return ports.GCReportDetail{}, err
	}

	candidates, cutoff, err := s.gcCandidates(ctx, now)
	if err != nil {
		return ports.GCReportDetail{}, err
	}

	var candidateBytes int64
	for _, candidate := range candidates {
		candidateBytes += candidate.Size
	}

	requestedBy := ""
	if principal := ports.PrincipalFromContext(ctx); principal != nil {
		requestedBy = principal.UserID
	}

	report := ports.GCReport{
		ID:                uuid.NewString(),
		Status:            ports.GCReportStatusReported,
		ComputedAt:        now,
		ExpiresAt:         now.Add(gcReportTTL),
		GraceCutoff:       cutoff,
		CandidateCount:    len(candidates),
		CandidateBytes:    candidateBytes,
		DurationMillis:    time.Since(started).Milliseconds(),
		RequestedBy:       requestedBy,
		TriggeredInTenant: s.tenant(ctx),
	}

	if err := s.metadata.CreateGCReport(ctx, report, candidates); err != nil {
		return ports.GCReportDetail{}, err
	}

	return ports.GCReportDetail{Report: report, Candidates: candidates}, nil
}

// GetGCReport fetches a persisted report by id. GLOBAL (design.md Decision
// D): no tenant filtering -- a report computed under one tenant's request
// context is readable and usable from any other.
func (s *Service) GetGCReport(ctx context.Context, reportID string) (ports.GCReportDetail, error) {
	return s.metadata.GetGCReport(ctx, reportID)
}

// DeleteByGCReport is the delete call. Ordering is flag FIRST (design.md
// Decision F): a flag-off deployment performs zero reads and zero writes on
// this path -- GetGCReport is not even called. Authorization is the caller's
// responsibility (handleAdmin's requireAdminPrincipal runs before this
// method is ever reached), so this never discloses flag state to an
// unauthorized caller either.
//
// Once the flag is on: GetGCReport -> status must still be "reported" (else
// ErrorCodeConflict, single-use) -> must not be expired (else validation) ->
// gcGate (D2, bounds this to a single in-flight run, shared with
// ComputeGCReport) -> gcCandidates is recomputed from scratch (D5) -> only
// digests present in BOTH the stored report's candidates AND the fresh
// recomputation are ever attempted for a real unlink; a report-only digest
// is recorded "retained" and its file is never touched. Per-digest unlink
// failures are recorded individually and never abort the delete (T10): the
// report still reaches terminal "deleted" state, with Error summarizing how
// many candidates failed. MarkGCReportDeleted's own WHERE status='reported'
// is the actual single-use guard (Decision E), settling the
// two-concurrent-deletes race that the earlier status check above only
// short-circuits for the common case.
func (s *Service) DeleteByGCReport(ctx context.Context, reportID string) (ports.GCReportDetail, error) {
	if !s.gcDeleteEnabled {
		return ports.GCReportDetail{}, domain.NewUnsupportedError(gcDeleteDisabledMessage)
	}

	detail, err := s.metadata.GetGCReport(ctx, reportID)
	if err != nil {
		return ports.GCReportDetail{}, err
	}

	if detail.Report.Status != ports.GCReportStatusReported {
		return ports.GCReportDetail{}, domain.NewConflictError(fmt.Sprintf("gc report %q is not in reported state", reportID))
	}

	now := s.now()
	if !now.Before(detail.Report.ExpiresAt) {
		return ports.GCReportDetail{}, domain.NewValidationError(fmt.Sprintf("gc report %q has expired", reportID))
	}

	if err := s.gcGate.acquire(ctx, 1); err != nil {
		return ports.GCReportDetail{}, err
	}
	defer s.gcGate.release()

	fresh, _, err := s.gcCandidates(ctx, now)
	if err != nil {
		return ports.GCReportDetail{}, err
	}
	freshSet := make(map[string]struct{}, len(fresh))
	for _, candidate := range fresh {
		freshSet[candidate.Digest] = struct{}{}
	}

	candidateOutcomes := make([]ports.GCCandidateOutcome, 0, len(detail.Candidates))
	var deletedCount int
	var bytesReclaimed int64
	var failureCount int

	for _, candidate := range detail.Candidates {
		if _, stillCandidate := freshSet[candidate.Digest]; !stillCandidate {
			candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
				Digest:  candidate.Digest,
				Outcome: ports.GCCandidateOutcomeRetained,
			})
			continue
		}

		digest, parseErr := domain.ParseDigest(candidate.Digest)
		if parseErr != nil {
			failureCount++
			candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
				Digest:  candidate.Digest,
				Outcome: ports.GCCandidateOutcomeFailed,
				Error:   parseErr.Error(),
			})
			continue
		}

		// JD-1: freshSet above is a single snapshot taken before this loop
		// started. For a large batch, a legitimate push can publish a
		// manifest referencing THIS exact digest after that snapshot but
		// before this digest's own turn here. Re-verify narrowly,
		// immediately before the unlink, rather than trusting the
		// batch-wide snapshot alone.
		referenced, refErr := s.metadata.IsBlobDigestReferenced(ctx, candidate.Digest)
		if refErr != nil {
			failureCount++
			candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
				Digest:  candidate.Digest,
				Outcome: ports.GCCandidateOutcomeFailed,
				Error:   refErr.Error(),
			})
			continue
		}
		if referenced {
			candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
				Digest:  candidate.Digest,
				Outcome: ports.GCCandidateOutcomeRetained,
			})
			continue
		}

		removed, deleteErr := s.blobs.DeleteBlob(ctx, digest)
		if deleteErr != nil {
			failureCount++
			candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
				Digest:  candidate.Digest,
				Outcome: ports.GCCandidateOutcomeFailed,
				Error:   deleteErr.Error(),
			})
			continue
		}
		if !removed {
			candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
				Digest:  candidate.Digest,
				Outcome: ports.GCCandidateOutcomeMissing,
			})
			continue
		}

		deletedCount++
		bytesReclaimed += candidate.Size
		candidateOutcomes = append(candidateOutcomes, ports.GCCandidateOutcome{
			Digest:  candidate.Digest,
			Outcome: ports.GCCandidateOutcomeDeleted,
		})
	}

	deletedBy := ""
	if principal := ports.PrincipalFromContext(ctx); principal != nil {
		deletedBy = principal.UserID
	}

	outcomeError := ""
	if failureCount > 0 {
		outcomeError = fmt.Sprintf("%d of %d unlinks failed", failureCount, len(detail.Candidates))
	}

	outcome := ports.GCDeleteOutcome{
		DeletedAt:      now,
		DeletedBy:      deletedBy,
		DeletedCount:   deletedCount,
		BytesReclaimed: bytesReclaimed,
		Error:          outcomeError,
		Candidates:     candidateOutcomes,
	}

	if err := s.metadata.MarkGCReportDeleted(ctx, reportID, outcome); err != nil {
		return ports.GCReportDetail{}, err
	}

	return s.metadata.GetGCReport(ctx, reportID)
}

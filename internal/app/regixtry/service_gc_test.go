package regixtry

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
)

// TestComputeGCReportExcludesBlobInsideGraceWindow is T3's service half
// (design.md Testing Strategy): a freshly committed, unreferenced blob is
// inside the 24h grace window and must not be a candidate; advancing s.now
// past gcGraceWindow makes it one (design's fixed gcGraceWindow constant,
// D3).
func TestComputeGCReportExcludesBlobInsideGraceWindow(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()
	payload := []byte("unreferenced-layer")
	upload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest(payload), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	detail, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	if detail.Report.CandidateCount != 0 {
		t.Fatalf("CandidateCount = %d, want 0 while blob is inside the grace window", detail.Report.CandidateCount)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }

	later, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() (past grace) error = %v", err)
	}
	if later.Report.CandidateCount != 1 {
		t.Fatalf("CandidateCount = %d, want 1 once past the grace window", later.Report.CandidateCount)
	}
	if len(later.Candidates) != 1 || later.Candidates[0].Digest != blob.Digest {
		t.Fatalf("Candidates = %#v, want exactly [%s]", later.Candidates, blob.Digest)
	}
}

// TestGCRespectsBlobsReferencedOnlyByAnotherTenant is T2's report-time half:
// tenant B publishes a manifest referencing X; ComputeGCReport under tenant
// A's context must never list X as a candidate, even once X is past the
// grace window (design.md Decision C -- the mark query has no tenant
// predicate at all).
func TestGCRespectsBlobsReferencedOnlyByAnotherTenant(t *testing.T) {
	t.Parallel()

	serviceA, serviceB, cleanup := newTestServicePairSharingStorage(t)
	defer cleanup()

	ctx := context.Background()

	sharedPayload := []byte("shared-referenced-layer")
	uploadShared, err := serviceB.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(shared) error = %v", err)
	}
	sharedBlob, err := serviceB.CompleteUpload(ctx, "library/alpine", uploadShared.ID, digestForTest(sharedPayload), strings.NewReader(string(sharedPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(shared) error = %v", err)
	}
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + sharedBlob.Digest + `","size":` + strconv.Itoa(len(sharedPayload)) + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + sharedBlob.Digest + `","size":` + strconv.Itoa(len(sharedPayload)) + `}]}`)
	if _, err := serviceB.PublishManifest(ctx, "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest(tenant-b) error = %v", err)
	}

	unreferencedPayload := []byte("tenant-a-owned-garbage")
	uploadUnreferenced, err := serviceA.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(unreferenced) error = %v", err)
	}
	unreferencedBlob, err := serviceA.CompleteUpload(ctx, "library/alpine", uploadUnreferenced.ID, digestForTest(unreferencedPayload), strings.NewReader(string(unreferencedPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(unreferenced) error = %v", err)
	}

	future := func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	serviceA.now = future
	serviceA.WaitForBackgroundWork()

	detail, err := serviceA.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport(tenant-a) error = %v", err)
	}

	for _, candidate := range detail.Candidates {
		if candidate.Digest == sharedBlob.Digest {
			t.Fatalf("candidates = %#v, must never include %s (referenced only by tenant B)", detail.Candidates, sharedBlob.Digest)
		}
	}

	foundUnreferenced := false
	for _, candidate := range detail.Candidates {
		if candidate.Digest == unreferencedBlob.Digest {
			foundUnreferenced = true
		}
	}
	if !foundUnreferenced {
		t.Fatalf("candidates = %#v, want %s (genuinely unreferenced, past grace) to be present", detail.Candidates, unreferencedBlob.Digest)
	}
}

// TestGCReportIsReadableAndUsableFromAnotherTenantContext is T8: reports are
// global rows (design.md Decision D) -- a report computed while tenant A's
// context was active must be fetchable via a service resolving a different
// tenant.
func TestGCReportIsReadableAndUsableFromAnotherTenantContext(t *testing.T) {
	t.Parallel()

	serviceA, serviceB, cleanup := newTestServicePairSharingStorage(t)
	defer cleanup()

	ctx := context.Background()

	detail, err := serviceA.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport(tenant-a) error = %v", err)
	}

	fromB, err := serviceB.GetGCReport(ctx, detail.Report.ID)
	if err != nil {
		t.Fatalf("GetGCReport(tenant-b context) error = %v", err)
	}
	if fromB.Report.ID != detail.Report.ID {
		t.Fatalf("fromB.Report.ID = %s, want %s", fromB.Report.ID, detail.Report.ID)
	}
	if fromB.Report.TriggeredInTenant != "tenant-a" {
		t.Fatalf("fromB.Report.TriggeredInTenant = %s, want tenant-a (provenance only, D6)", fromB.Report.TriggeredInTenant)
	}
}

// TestDeleteByGCReportRefusesWhenFlagOff is T4's service half: with
// gcDeleteEnabled false (the default), DeleteByGCReport must refuse with
// ErrorCodeUnsupported and never read the report row at all (design.md
// Decision F: the flag is checked BEFORE GetGCReport, so a flag-off
// deployment performs zero reads and zero writes on the destructive path).
// zeroReadsMetadataStore fails the test outright if GetGCReport is ever
// invoked, proving the ordering rather than merely asserting the outcome.
func TestDeleteByGCReportRefusesWhenFlagOff(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	guarded := zeroReadsMetadataStore{Store: metadataStore, t: t}
	service := NewService(blobs, guarded, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())

	_, err = service.DeleteByGCReport(context.Background(), "any-report-id")
	if !domain.IsCode(err, domain.ErrorCodeUnsupported) {
		t.Fatalf("DeleteByGCReport() error = %v, want ErrorCodeUnsupported", err)
	}
}

// TestDeleteByGCReportRejectsEmptyReportID pins spec.md's "Delete without a
// report reference is rejected" scenario (Requirement: Delete Requires a
// Valid Prior Report) for a delete request that omits a report id entirely.
// The flag is deliberately ON so a NotFound here proves the ID-validation
// path itself, distinct from the flag-off Unsupported path already covered
// by TestDeleteByGCReportRefusesWhenFlagOff.
func TestDeleteByGCReportRejectsEmptyReportID(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetGCDeleteEnabled(true)

	_, err := service.DeleteByGCReport(context.Background(), "")
	if !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("DeleteByGCReport(\"\") error = %v, want ErrorCodeNotFound", err)
	}
}

// TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest is T1
// (design.md Testing Strategy): a candidate that was unreferenced when the
// report was computed but gains a manifest reference before delete runs
// must never be unlinked -- DeleteByGCReport's recompute-and-intersect (D5)
// is the sole delete-safety mechanism. A genuinely-still-garbage sibling
// candidate should reach terminal outcome "deleted"; today it instead
// surfaces "failed" because Phase 7's DeleteBlob stub unconditionally
// errors before any real unlink exists -- an honest RED, not a compile
// failure, confirmed against the stub and expected to flip once Phase 8
// replaces it with a real os.Remove.
func TestDeleteByGCReportNeverUnlinksBlobReferencedByAManifest(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetGCDeleteEnabled(true)

	ctx := context.Background()

	protectedPayload := []byte("later-referenced-layer")
	protectedUpload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(protected) error = %v", err)
	}
	protectedBlob, err := service.CompleteUpload(ctx, "library/alpine", protectedUpload.ID, digestForTest(protectedPayload), strings.NewReader(string(protectedPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(protected) error = %v", err)
	}

	siblingPayload := []byte("genuinely-garbage-layer")
	siblingUpload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(sibling) error = %v", err)
	}
	siblingBlob, err := service.CompleteUpload(ctx, "library/alpine", siblingUpload.ID, digestForTest(siblingPayload), strings.NewReader(string(siblingPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(sibling) error = %v", err)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	service.WaitForBackgroundWork()

	detail, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	if detail.Report.CandidateCount != 2 {
		t.Fatalf("CandidateCount = %d, want 2 (both blobs unreferenced, past grace)", detail.Report.CandidateCount)
	}

	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + protectedBlob.Digest + `","size":` + strconv.Itoa(len(protectedPayload)) + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + protectedBlob.Digest + `","size":` + strconv.Itoa(len(protectedPayload)) + `}]}`)
	if _, err := service.PublishManifest(ctx, "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest() error = %v", err)
	}
	service.WaitForBackgroundWork()

	result, err := service.DeleteByGCReport(ctx, detail.Report.ID)
	if err != nil {
		t.Fatalf("DeleteByGCReport() error = %v", err)
	}

	var protectedOutcome, siblingOutcome string
	for _, candidate := range result.Candidates {
		switch candidate.Digest {
		case protectedBlob.Digest:
			protectedOutcome = candidate.Outcome
		case siblingBlob.Digest:
			siblingOutcome = candidate.Outcome
		}
	}
	if protectedOutcome != ports.GCCandidateOutcomeRetained {
		t.Fatalf("protected candidate outcome = %q, want %q (now referenced by a manifest)", protectedOutcome, ports.GCCandidateOutcomeRetained)
	}

	exists, err := service.blobs.BlobExists(ctx, domain.MustParseDigest(protectedBlob.Digest))
	if err != nil {
		t.Fatalf("BlobExists(protected) error = %v", err)
	}
	if !exists {
		t.Fatal("protected blob was removed from disk despite being referenced by a manifest before delete")
	}
	if result.Report.BytesReclaimed >= int64(len(protectedPayload))+int64(len(siblingPayload)) {
		t.Fatalf("BytesReclaimed = %d, must exclude the protected blob's size", result.Report.BytesReclaimed)
	}

	if siblingOutcome != ports.GCCandidateOutcomeDeleted {
		t.Fatalf("sibling candidate outcome = %q, want %q (still genuinely unreferenced, past grace)", siblingOutcome, ports.GCCandidateOutcomeDeleted)
	}
}

// TestGCRespectsBlobsReferencedOnlyByAnotherTenantAtDeleteTime is T2's
// delete-survival half: under tenant A's context, a digest referenced only
// by tenant B must never be attempted for unlink at delete time either --
// proven end-to-end through DeleteByGCReport's OWN recompute (D5), not just
// gcCandidates/ComputeGCReport. The genuinely-unreferenced sibling candidate
// is asserted to reach "deleted"; today it surfaces "failed" via Phase 7's
// stub -- an honest RED confirmed against the stub, expected to flip once
// Phase 8 lands a real unlink.
func TestGCRespectsBlobsReferencedOnlyByAnotherTenantAtDeleteTime(t *testing.T) {
	t.Parallel()

	serviceA, serviceB, cleanup := newTestServicePairSharingStorage(t)
	defer cleanup()
	serviceA.SetGCDeleteEnabled(true)

	ctx := context.Background()

	sharedPayload := []byte("delete-time-shared-layer")
	uploadShared, err := serviceB.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(shared) error = %v", err)
	}
	sharedBlob, err := serviceB.CompleteUpload(ctx, "library/alpine", uploadShared.ID, digestForTest(sharedPayload), strings.NewReader(string(sharedPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(shared) error = %v", err)
	}
	manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + sharedBlob.Digest + `","size":` + strconv.Itoa(len(sharedPayload)) + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + sharedBlob.Digest + `","size":` + strconv.Itoa(len(sharedPayload)) + `}]}`)
	if _, err := serviceB.PublishManifest(ctx, "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
		t.Fatalf("PublishManifest(tenant-b) error = %v", err)
	}

	unreferencedPayload := []byte("tenant-a-genuinely-garbage-at-delete-time")
	uploadUnreferenced, err := serviceA.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(unreferenced) error = %v", err)
	}
	unreferencedBlob, err := serviceA.CompleteUpload(ctx, "library/alpine", uploadUnreferenced.ID, digestForTest(unreferencedPayload), strings.NewReader(string(unreferencedPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(unreferenced) error = %v", err)
	}

	future := func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	serviceA.now = future
	serviceA.WaitForBackgroundWork()
	serviceB.WaitForBackgroundWork()

	detail, err := serviceA.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport(tenant-a) error = %v", err)
	}
	for _, candidate := range detail.Candidates {
		if candidate.Digest == sharedBlob.Digest {
			t.Fatalf("report candidates = %#v, must never include %s (referenced only by tenant B)", detail.Candidates, sharedBlob.Digest)
		}
	}

	result, err := serviceA.DeleteByGCReport(ctx, detail.Report.ID)
	if err != nil {
		t.Fatalf("DeleteByGCReport() error = %v", err)
	}

	for _, candidate := range result.Candidates {
		if candidate.Digest == sharedBlob.Digest {
			t.Fatalf("delete outcomes = %#v, must never attempt to unlink %s (referenced only by tenant B)", result.Candidates, sharedBlob.Digest)
		}
	}

	exists, err := serviceA.blobs.BlobExists(ctx, domain.MustParseDigest(sharedBlob.Digest))
	if err != nil {
		t.Fatalf("BlobExists(shared) error = %v", err)
	}
	if !exists {
		t.Fatal("cross-tenant-referenced blob was removed from disk")
	}

	var unreferencedOutcome string
	for _, candidate := range result.Candidates {
		if candidate.Digest == unreferencedBlob.Digest {
			unreferencedOutcome = candidate.Outcome
		}
	}
	if unreferencedOutcome != ports.GCCandidateOutcomeDeleted {
		t.Fatalf("unreferenced candidate outcome = %q, want %q (genuinely garbage, past grace, must actually be deleted at delete time)", unreferencedOutcome, ports.GCCandidateOutcomeDeleted)
	}
}

// raceInjectingBlobStore wraps a real ports.BlobStore and fires onTrigger
// immediately after the real unlink of triggerAfterDigest completes -- used
// to simulate a manifest publish that lands mid-batch, strictly after the
// pre-loop freshSet snapshot but strictly before the victim digest's own
// turn in the loop, proving the gap JD-1 identified: recompute-and-intersect
// runs exactly once before the loop, with no per-digest recheck immediately
// before each individual unlink.
type raceInjectingBlobStore struct {
	ports.BlobStore
	triggerAfterDigest string
	onTrigger          func()
	fired              bool
}

func (r *raceInjectingBlobStore) DeleteBlob(ctx context.Context, digest domain.Digest) (bool, error) {
	removed, err := r.BlobStore.DeleteBlob(ctx, digest)
	if !r.fired && digest.String() == r.triggerAfterDigest {
		r.fired = true
		r.onTrigger()
	}
	return removed, err
}

// TestDeleteByGCReportDoesNotUnlinkBlobReferencedDuringBatchProcessing is
// JD-1: DeleteByGCReport's recompute-and-intersect is documented as "can
// only under-delete" (gcReportTTL doc comment), but the fresh mark-and-sweep
// set (freshSet) is computed exactly ONCE before the per-digest delete loop
// starts, with no re-check immediately before each individual
// s.blobs.DeleteBlob call. This test proves the consequence directly: while
// the first candidate's blob is being unlinked, a manifest publish lands
// referencing the SECOND (not-yet-processed) candidate's digest -- a
// legitimate push racing the GC batch, not a crafted attack. Without a
// per-digest recheck, the second candidate still gets deleted despite being
// referenced at the moment of its own deletion, contradicting the code's
// own stated invariant.
func TestDeleteByGCReportDoesNotUnlinkBlobReferencedDuringBatchProcessing(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	racing := &raceInjectingBlobStore{BlobStore: blobs}

	var service *Service
	service = NewService(racing, metadataStore, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	defer service.WaitForBackgroundWork()
	service.SetGCDeleteEnabled(true)

	ctx := context.Background()

	payloadA := []byte("race-batch-candidate-a-genuinely-garbage")
	uploadA, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(a) error = %v", err)
	}
	if _, err := service.CompleteUpload(ctx, "library/alpine", uploadA.ID, digestForTest(payloadA), strings.NewReader(string(payloadA))); err != nil {
		t.Fatalf("CompleteUpload(a) error = %v", err)
	}

	payloadB := []byte("race-batch-candidate-b-genuinely-garbage")
	uploadB, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(b) error = %v", err)
	}
	if _, err := service.CompleteUpload(ctx, "library/alpine", uploadB.ID, digestForTest(payloadB), strings.NewReader(string(payloadB))); err != nil {
		t.Fatalf("CompleteUpload(b) error = %v", err)
	}

	payloadByDigest := map[string][]byte{
		digestForTest(payloadA): payloadA,
		digestForTest(payloadB): payloadB,
	}

	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	service.WaitForBackgroundWork()

	detail, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	if detail.Report.CandidateCount != 2 {
		t.Fatalf("CandidateCount = %d, want 2 (both blobs unreferenced, past grace)", detail.Report.CandidateCount)
	}
	if len(detail.Candidates) != 2 {
		t.Fatalf("len(Candidates) = %d, want 2", len(detail.Candidates))
	}

	// gcCandidates sorts by digest (D1); the loop in DeleteByGCReport walks
	// detail.Candidates in that same stored order. Whichever candidate sorts
	// first (index 0) is the one whose DeleteBlob call fires the mid-batch
	// publish; the SECOND one (index 1, "victim") is what the publish
	// references and what the delete loop has not yet reached -- decided
	// dynamically here rather than by literal payload identity, since digest
	// sort order is content-dependent.
	triggerDigest := detail.Candidates[0].Digest
	victimDigest := detail.Candidates[1].Digest
	victimPayload, ok := payloadByDigest[victimDigest]
	if !ok {
		t.Fatalf("victimDigest = %s not among the payloads this test uploaded; candidates = %#v", victimDigest, detail.Candidates)
	}

	racing.triggerAfterDigest = triggerDigest
	racing.onTrigger = func() {
		manifestPayload := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + victimDigest + `","size":` + strconv.Itoa(len(victimPayload)) + `},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"` + victimDigest + `","size":` + strconv.Itoa(len(victimPayload)) + `}]}`)
		if _, err := service.PublishManifest(ctx, "library/alpine", "latest", "application/vnd.oci.image.manifest.v1+json", manifestPayload); err != nil {
			t.Fatalf("PublishManifest(mid-batch race) error = %v", err)
		}
	}

	result, err := service.DeleteByGCReport(ctx, detail.Report.ID)
	if err != nil {
		t.Fatalf("DeleteByGCReport() error = %v", err)
	}

	var victimOutcome string
	for _, candidate := range result.Candidates {
		if candidate.Digest == victimDigest {
			victimOutcome = candidate.Outcome
		}
	}
	if victimOutcome != ports.GCCandidateOutcomeRetained {
		t.Fatalf("victim candidate outcome = %q, want %q (became referenced mid-batch, before its own turn in the delete loop)", victimOutcome, ports.GCCandidateOutcomeRetained)
	}

	exists, err := service.blobs.BlobExists(ctx, domain.MustParseDigest(victimDigest))
	if err != nil {
		t.Fatalf("BlobExists(victim) error = %v", err)
	}
	if !exists {
		t.Fatal("victim blob was removed from disk despite becoming referenced by a manifest before its own turn in the delete loop (JD-1)")
	}
}

// TestDeleteByGCReportRejectsAlreadyUsedReport is T5's service half: a
// second delete request against a report already transitioned to "deleted"
// must be rejected with a conflict, and must not attempt to unlink anything
// (the service-level status check runs before recompute/DeleteBlob at all).
func TestDeleteByGCReportRejectsAlreadyUsedReport(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetGCDeleteEnabled(true)

	ctx := context.Background()

	payload := []byte("single-use-report-body")
	upload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest(payload), strings.NewReader(string(payload))); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	service.WaitForBackgroundWork()

	detail, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}

	if _, err := service.DeleteByGCReport(ctx, detail.Report.ID); err != nil {
		t.Fatalf("DeleteByGCReport() (first) error = %v", err)
	}

	_, err = service.DeleteByGCReport(ctx, detail.Report.ID)
	if !domain.IsCode(err, domain.ErrorCodeConflict) {
		t.Fatalf("DeleteByGCReport() (second) error = %v, want ErrorCodeConflict", err)
	}
}

// TestDeleteByGCReportRejectsExpiredReport is T9's expiry half: a report
// past its expires_at must be rejected without unlinking anything, even
// though it is still in "reported" state -- expiry is checked independently
// of single-use (design.md D8, hygiene, distinct from D5 safety).
func TestDeleteByGCReportRejectsExpiredReport(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetGCDeleteEnabled(true)

	ctx := context.Background()

	payload := []byte("expired-report-candidate-body")
	upload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest(payload), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	now := time.Now().UTC()
	expiredReport := ports.GCReport{
		ID:             "expired-delete-target",
		Status:         ports.GCReportStatusReported,
		ComputedAt:     now.Add(-48 * time.Hour),
		ExpiresAt:      now.Add(-24 * time.Hour),
		GraceCutoff:    now.Add(-72 * time.Hour),
		CandidateCount: 1,
		CandidateBytes: int64(len(payload)),
	}
	candidates := []ports.GCReportCandidate{
		{Digest: blob.Digest, Size: int64(len(payload)), ModTime: now.Add(-72 * time.Hour)},
	}
	if err := service.metadata.CreateGCReport(ctx, expiredReport, candidates); err != nil {
		t.Fatalf("CreateGCReport() error = %v", err)
	}

	_, err = service.DeleteByGCReport(ctx, expiredReport.ID)
	if !domain.IsCode(err, domain.ErrorCodeValidation) {
		t.Fatalf("DeleteByGCReport() error = %v, want ErrorCodeValidation for an expired report", err)
	}

	exists, err := service.blobs.BlobExists(ctx, domain.MustParseDigest(blob.Digest))
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}
	if !exists {
		t.Fatal("blob was removed from disk despite the report being expired")
	}
}

// flakyBlobStore wraps a real ports.BlobStore but forces DeleteBlob to fail
// for one specific digest, injected via NewService -- T10's fake store.
type flakyBlobStore struct {
	ports.BlobStore
	failDigest string
}

func (f flakyBlobStore) DeleteBlob(ctx context.Context, digest domain.Digest) (bool, error) {
	if digest.String() == f.failDigest {
		return false, errors.New("simulated unlink failure")
	}
	return f.BlobStore.DeleteBlob(ctx, digest)
}

// TestDeleteByGCReportRecordsPerDigestFailureAndPartialReclaim is T10: one
// candidate's unlink fails while a sibling succeeds. The delete must not
// abort -- outcomes mix "deleted"/"failed", bytes_reclaimed counts only the
// successful unlink, and the report still reaches terminal "deleted" state
// with a non-empty Error summarizing the failure count.
func TestDeleteByGCReportRecordsPerDigestFailureAndPartialReclaim(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}
	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}
	defer metadataStore.Close()

	failingPayload := []byte("this-unlink-will-fail")
	failingDigest := digestForTest(failingPayload)
	flaky := flakyBlobStore{BlobStore: blobs, failDigest: failingDigest}

	service := NewService(flaky, metadataStore, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	defer service.WaitForBackgroundWork()
	service.SetGCDeleteEnabled(true)

	ctx := context.Background()

	failingUpload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(failing) error = %v", err)
	}
	if _, err := service.CompleteUpload(ctx, "library/alpine", failingUpload.ID, failingDigest, strings.NewReader(string(failingPayload))); err != nil {
		t.Fatalf("CompleteUpload(failing) error = %v", err)
	}

	succeedingPayload := []byte("this-unlink-will-succeed")
	succeedingUpload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload(succeeding) error = %v", err)
	}
	succeedingBlob, err := service.CompleteUpload(ctx, "library/alpine", succeedingUpload.ID, digestForTest(succeedingPayload), strings.NewReader(string(succeedingPayload)))
	if err != nil {
		t.Fatalf("CompleteUpload(succeeding) error = %v", err)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	service.WaitForBackgroundWork()

	detail, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	if detail.Report.CandidateCount != 2 {
		t.Fatalf("CandidateCount = %d, want 2", detail.Report.CandidateCount)
	}

	result, err := service.DeleteByGCReport(ctx, detail.Report.ID)
	if err != nil {
		t.Fatalf("DeleteByGCReport() error = %v", err)
	}

	var failingOutcome, succeedingOutcome string
	for _, candidate := range result.Candidates {
		switch candidate.Digest {
		case failingDigest:
			failingOutcome = candidate.Outcome
		case succeedingBlob.Digest:
			succeedingOutcome = candidate.Outcome
		}
	}
	if failingOutcome != ports.GCCandidateOutcomeFailed {
		t.Fatalf("failing candidate outcome = %q, want %q", failingOutcome, ports.GCCandidateOutcomeFailed)
	}
	if succeedingOutcome != ports.GCCandidateOutcomeDeleted {
		t.Fatalf("succeeding candidate outcome = %q, want %q", succeedingOutcome, ports.GCCandidateOutcomeDeleted)
	}
	if result.Report.BytesReclaimed != int64(len(succeedingPayload)) {
		t.Fatalf("BytesReclaimed = %d, want %d (only the successful unlink)", result.Report.BytesReclaimed, len(succeedingPayload))
	}
	if result.Report.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.Report.DeletedCount)
	}
	if result.Report.Status != ports.GCReportStatusDeleted {
		t.Fatalf("Status = %q, want %q (partial failure must still reach terminal state)", result.Report.Status, ports.GCReportStatusDeleted)
	}
	if result.Report.Error == "" {
		t.Fatal("Report.Error is empty, want a summary naming the failure count")
	}
}

// TestDeleteByGCReportRecordsFullAuditFieldSetOnCompletion asserts the
// complete audit field set on the terminal report row -- deleted_at,
// deleted_by, deleted_count, bytes_reclaimed, AND error -- for both an
// all-success case and a partial-failure case, not just bytes_reclaimed
// (which every other GC test only asserts incidentally). This is the spec's
// "terminal row records audit fields" scenario, which design's 12 test
// groups otherwise leave without dedicated coverage.
func TestDeleteByGCReportRecordsFullAuditFieldSetOnCompletion(t *testing.T) {
	t.Parallel()

	t.Run("all success", func(t *testing.T) {
		t.Parallel()

		service, cleanup := newTestService(t, allowAllAccessController{})
		defer cleanup()
		service.SetGCDeleteEnabled(true)

		ctx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{UserID: "usr_audit"})

		payload := []byte("audit-trail-all-success-body")
		upload, err := service.BeginUpload(ctx, "library/alpine")
		if err != nil {
			t.Fatalf("BeginUpload() error = %v", err)
		}
		if _, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest(payload), strings.NewReader(string(payload))); err != nil {
			t.Fatalf("CompleteUpload() error = %v", err)
		}

		service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
		service.WaitForBackgroundWork()

		detail, err := service.ComputeGCReport(ctx)
		if err != nil {
			t.Fatalf("ComputeGCReport() error = %v", err)
		}

		result, err := service.DeleteByGCReport(ctx, detail.Report.ID)
		if err != nil {
			t.Fatalf("DeleteByGCReport() error = %v", err)
		}

		if result.Report.DeletedAt == nil || result.Report.DeletedAt.IsZero() {
			t.Fatalf("DeletedAt = %v, want a non-zero timestamp", result.Report.DeletedAt)
		}
		if result.Report.DeletedBy != "usr_audit" {
			t.Fatalf("DeletedBy = %q, want %q", result.Report.DeletedBy, "usr_audit")
		}
		if result.Report.DeletedCount != 1 {
			t.Fatalf("DeletedCount = %d, want 1", result.Report.DeletedCount)
		}
		if result.Report.BytesReclaimed != int64(len(payload)) {
			t.Fatalf("BytesReclaimed = %d, want %d", result.Report.BytesReclaimed, len(payload))
		}
		if result.Report.Error != "" {
			t.Fatalf("Error = %q, want empty on full success", result.Report.Error)
		}

		fromDB, err := service.metadata.GetGCReport(ctx, detail.Report.ID)
		if err != nil {
			t.Fatalf("GetGCReport() error = %v", err)
		}
		if fromDB.Report.DeletedBy != "usr_audit" || fromDB.Report.DeletedCount != 1 {
			t.Fatalf("persisted report = %#v, want the same audit fields to survive a fresh read", fromDB.Report)
		}
	})

	t.Run("partial failure", func(t *testing.T) {
		t.Parallel()

		rootDir := t.TempDir()
		blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
		if err != nil {
			t.Fatalf("fsblob.New() error = %v", err)
		}
		metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
		if err != nil {
			t.Fatalf("sqlite.New() error = %v", err)
		}
		defer metadataStore.Close()

		failingPayload := []byte("audit-trail-partial-failure-body")
		failingDigest := digestForTest(failingPayload)
		flaky := flakyBlobStore{BlobStore: blobs, failDigest: failingDigest}

		service := NewService(flaky, metadataStore, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
		defer service.WaitForBackgroundWork()
		service.SetGCDeleteEnabled(true)

		ctx := ports.ContextWithPrincipal(context.Background(), domainauth.Principal{UserID: "usr_audit_partial"})

		upload, err := service.BeginUpload(ctx, "library/alpine")
		if err != nil {
			t.Fatalf("BeginUpload() error = %v", err)
		}
		if _, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, failingDigest, strings.NewReader(string(failingPayload))); err != nil {
			t.Fatalf("CompleteUpload() error = %v", err)
		}

		service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
		service.WaitForBackgroundWork()

		detail, err := service.ComputeGCReport(ctx)
		if err != nil {
			t.Fatalf("ComputeGCReport() error = %v", err)
		}

		result, err := service.DeleteByGCReport(ctx, detail.Report.ID)
		if err != nil {
			t.Fatalf("DeleteByGCReport() error = %v", err)
		}

		if result.Report.DeletedAt == nil || result.Report.DeletedAt.IsZero() {
			t.Fatalf("DeletedAt = %v, want a non-zero timestamp even on partial failure", result.Report.DeletedAt)
		}
		if result.Report.DeletedBy != "usr_audit_partial" {
			t.Fatalf("DeletedBy = %q, want %q", result.Report.DeletedBy, "usr_audit_partial")
		}
		if result.Report.DeletedCount != 0 {
			t.Fatalf("DeletedCount = %d, want 0 (the only candidate failed)", result.Report.DeletedCount)
		}
		if result.Report.BytesReclaimed != 0 {
			t.Fatalf("BytesReclaimed = %d, want 0", result.Report.BytesReclaimed)
		}
		if result.Report.Error == "" {
			t.Fatal("Error is empty, want a summary naming the failure even though the report still reached terminal state")
		}
	})
}

// TestStillUnreferencedDigestIsDeletedAtDeleteTime is the spec's dedicated
// "Still-unreferenced digest is deleted" scenario: a report candidate that
// remains unreferenced and past grace at delete time must actually be
// unlinked from disk, its outcome recorded "deleted", and its size counted
// in bytes_reclaimed (Phase 8 GREEN, real DeleteBlob).
func TestStillUnreferencedDigestIsDeletedAtDeleteTime(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetGCDeleteEnabled(true)

	ctx := context.Background()

	payload := []byte("still-unreferenced-at-delete-time-body")
	upload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest(payload), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	service.WaitForBackgroundWork()

	detail, err := service.ComputeGCReport(ctx)
	if err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	if detail.Report.CandidateCount != 1 {
		t.Fatalf("CandidateCount = %d, want 1", detail.Report.CandidateCount)
	}

	result, err := service.DeleteByGCReport(ctx, detail.Report.ID)
	if err != nil {
		t.Fatalf("DeleteByGCReport() error = %v", err)
	}

	if len(result.Candidates) != 1 || result.Candidates[0].Digest != blob.Digest {
		t.Fatalf("result.Candidates = %#v, want exactly [%s]", result.Candidates, blob.Digest)
	}
	if result.Candidates[0].Outcome != ports.GCCandidateOutcomeDeleted {
		t.Fatalf("outcome = %q, want %q", result.Candidates[0].Outcome, ports.GCCandidateOutcomeDeleted)
	}
	if result.Report.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.Report.DeletedCount)
	}
	if result.Report.BytesReclaimed != int64(len(payload)) {
		t.Fatalf("BytesReclaimed = %d, want %d", result.Report.BytesReclaimed, len(payload))
	}
	if result.Report.Status != ports.GCReportStatusDeleted {
		t.Fatalf("Status = %q, want %q", result.Report.Status, ports.GCReportStatusDeleted)
	}

	exists, err := service.blobs.BlobExists(ctx, domain.MustParseDigest(blob.Digest))
	if err != nil {
		t.Fatalf("BlobExists() error = %v", err)
	}
	if exists {
		t.Fatal("blob file still exists on disk after being reported deleted with outcome \"deleted\"")
	}
}

// TestNoUnattendedDeletionOccursWithoutExplicitDeleteRequest proves the
// spec's "Out of Scope for v1" no-unattended-deletion requirement: a
// grace-expired, genuinely unreferenced blob survives any number of
// ComputeGCReport calls as time passes, because ComputeGCReport never calls
// DeleteBlob -- only an explicit DeleteByGCReport request can unlink
// anything. This is a proof-by-construction test (tasks.md 11.1-11.2): it
// requires no production change and is expected to pass immediately,
// exactly like 8.2's traversal-shaped-digest test.
func TestNoUnattendedDeletionOccursWithoutExplicitDeleteRequest(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()

	ctx := context.Background()

	payload := []byte("never-deleted-without-an-explicit-request")
	upload, err := service.BeginUpload(ctx, "library/alpine")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, "library/alpine", upload.ID, digestForTest(payload), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	// Advance well past the grace window so the blob is a genuine,
	// standing candidate -- the strongest case for "would this ever be
	// unlinked by accident?"
	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }

	for i := 0; i < 3; i++ {
		detail, err := service.ComputeGCReport(ctx)
		if err != nil {
			t.Fatalf("ComputeGCReport() call %d error = %v", i, err)
		}
		if detail.Report.CandidateCount != 1 {
			t.Fatalf("call %d: CandidateCount = %d, want 1 (blob must remain a standing candidate)", i, detail.Report.CandidateCount)
		}

		exists, err := service.blobs.BlobExists(ctx, domain.MustParseDigest(blob.Digest))
		if err != nil {
			t.Fatalf("call %d: BlobExists() error = %v", i, err)
		}
		if !exists {
			t.Fatalf("call %d: blob file no longer exists after ComputeGCReport alone -- DeleteByGCReport was never called", i)
		}

		// Time keeps passing between report computations.
		service.now = func() time.Time {
			return time.Now().UTC().Add(gcGraceWindow + time.Hour + time.Duration(i+1)*time.Hour)
		}
	}
}

// zeroReadsMetadataStore wraps the real sqlite store but fails the test if
// GetGCReport is ever called, proving DeleteByGCReport's flag-first
// ordering (design.md Decision F) rather than merely asserting the error
// it returns.
type zeroReadsMetadataStore struct {
	*metadata.Store
	t *testing.T
}

func (z zeroReadsMetadataStore) GetGCReport(context.Context, string) (ports.GCReportDetail, error) {
	z.t.Fatal("GetGCReport must not be called while REGISTRY_GC_DELETE_ENABLED is off (Decision F)")
	return ports.GCReportDetail{}, nil
}

func newTestServicePairSharingStorage(t *testing.T) (*Service, *Service, func()) {
	t.Helper()

	rootDir := t.TempDir()
	blobs, err := fsblob.New(filepath.Join(rootDir, "blobs"))
	if err != nil {
		t.Fatalf("fsblob.New() error = %v", err)
	}

	metadataStore, err := metadata.New(filepath.Join(rootDir, "registry.db"))
	if err != nil {
		t.Fatalf("sqlite.New() error = %v", err)
	}

	serviceA := NewService(blobs, metadataStore, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	serviceB := NewService(blobs, metadataStore, allowAllAccessController{}, ports.NewSingleTenantResolver("tenant-b"), ports.NewInlineJobRunner())

	return serviceA, serviceB, func() {
		serviceA.WaitForBackgroundWork()
		serviceB.WaitForBackgroundWork()
		_ = metadataStore.Close()
	}
}

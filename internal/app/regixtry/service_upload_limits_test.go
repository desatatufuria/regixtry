package regixtry

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

func uploadRowCount(t *testing.T, service *Service) int {
	t.Helper()
	uploads, err := service.metadata.ListUploads(context.Background(), service.tenant(context.Background()), nil)
	if err != nil {
		t.Fatalf("ListUploads() error = %v", err)
	}
	return len(uploads)
}

func TestBeginUploadEnforcesGlobalAndPerRepositoryCaps(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadLimits(3, 2)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := service.BeginUpload(ctx, "team/a"); err != nil {
			t.Fatalf("BeginUpload(team/a) #%d error = %v", i, err)
		}
	}
	if _, err := service.BeginUpload(ctx, "team/a"); !domain.IsCode(err, domain.ErrorCodeTooManyRequests) {
		t.Fatalf("third BeginUpload(team/a) error = %v, want too many requests (per-repository cap)", err)
	}

	// Another repository still has room until the global cap is hit.
	if _, err := service.BeginUpload(ctx, "team/b"); err != nil {
		t.Fatalf("BeginUpload(team/b) error = %v", err)
	}
	if _, err := service.BeginUpload(ctx, "team/c"); !domain.IsCode(err, domain.ErrorCodeTooManyRequests) {
		t.Fatalf("BeginUpload(team/c) error = %v, want too many requests (global cap)", err)
	}
	if got := uploadRowCount(t, service); got != 3 {
		t.Fatalf("open uploads = %d, want 3 (refused uploads leave nothing behind)", got)
	}
}

func TestBeginUploadCapsAreNotExceededUnderConcurrency(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadLimits(5, 5)

	done := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() {
			_, err := service.BeginUpload(context.Background(), "team/a")
			done <- err
		}()
	}
	accepted := 0
	for i := 0; i < 20; i++ {
		if err := <-done; err == nil {
			accepted++
		}
	}
	if accepted != 5 {
		t.Fatalf("accepted %d concurrent uploads, want exactly 5", accepted)
	}
}

func TestCompletedAndCancelledUploadsFreeTheirCapSlot(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadLimits(1, 1)
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	payload := []byte("slot-content")
	if _, err := service.CompleteUpload(ctx, "team/a", upload.ID, digestForTest(payload), strings.NewReader(string(payload))); err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	second, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() after complete error = %v", err)
	}
	if err := service.CancelUpload(ctx, "team/a", second.ID); err != nil {
		t.Fatalf("CancelUpload() error = %v", err)
	}
	if _, err := service.BeginUpload(ctx, "team/a"); err != nil {
		t.Fatalf("BeginUpload() after cancel error = %v", err)
	}
}

func TestCancelUploadRemovesRowAndDataAndIsRepositoryScoped(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	updated, err := service.AppendUpload(ctx, "team/a", upload.ID, strings.NewReader("partial"))
	if err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	state, err := service.metadata.GetUpload(ctx, service.tenant(ctx), upload.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}
	_ = updated

	// An upload of another repository cannot be cancelled through this one.
	if err := service.CancelUpload(ctx, "team/other", upload.ID); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("cross-repository CancelUpload() error = %v, want not found", err)
	}
	if _, err := os.Stat(state.Location); err != nil {
		t.Fatalf("data file must survive a cross-repository cancel: %v", err)
	}

	if err := service.CancelUpload(ctx, "team/a", upload.ID); err != nil {
		t.Fatalf("CancelUpload() error = %v", err)
	}
	if _, err := os.Stat(state.Location); !os.IsNotExist(err) {
		t.Fatalf("data file still present after cancel: %v", err)
	}
	if _, err := service.UploadStatus(ctx, "team/a", upload.ID); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("UploadStatus() after cancel error = %v, want not found", err)
	}
	if err := service.CancelUpload(ctx, "team/a", upload.ID); !domain.IsCode(err, domain.ErrorCodeNotFound) {
		t.Fatalf("second CancelUpload() error = %v, want not found", err)
	}
}

func TestReapAbandonedUploadsHonorsTTLWithFakeClock(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	abandoned, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(ctx, "team/a", abandoned.ID, strings.NewReader("stale")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	state, err := service.metadata.GetUpload(ctx, service.tenant(ctx), abandoned.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}

	// A committed blob must never be touched by the reaper.
	payload := []byte("committed-blob")
	done, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, "team/a", done.ID, digestForTest(payload), strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}

	now := time.Now().UTC()
	service.now = func() time.Time { return now }

	// Inside the TTL: nothing is reaped.
	reaped, err := service.ReapAbandonedUploads(ctx)
	if err != nil || reaped != 0 {
		t.Fatalf("ReapAbandonedUploads() inside TTL = %d, %v, want 0, nil", reaped, err)
	}
	if got := uploadRowCount(t, service); got != 1 {
		t.Fatalf("open uploads inside TTL = %d, want 1", got)
	}

	// Past the TTL: the row and the temp file go; the committed blob stays.
	service.now = func() time.Time { return now.Add(DefaultUploadTTL + time.Minute) }
	reaped, err = service.ReapAbandonedUploads(ctx)
	if err != nil || reaped != 1 {
		t.Fatalf("ReapAbandonedUploads() past TTL = %d, %v, want 1, nil", reaped, err)
	}
	if got := uploadRowCount(t, service); got != 0 {
		t.Fatalf("open uploads after reap = %d, want 0", got)
	}
	if _, err := os.Stat(state.Location); !os.IsNotExist(err) {
		t.Fatalf("temp file still present after reap: %v", err)
	}
	reader, _, err := service.blobs.OpenBlob(ctx, mustDigest(t, blob.Digest))
	if err != nil {
		t.Fatalf("committed blob lost by the reaper: %v", err)
	}
	_ = reader.Close()
}

func TestReapAbandonedUploadsSkipsAnUploadWithAWriteInFlight(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}

	pipeReader, pipeWriter := io.Pipe()
	appendDone := make(chan error, 1)
	go func() {
		_, err := service.AppendUpload(ctx, "team/a", upload.ID, pipeReader)
		appendDone <- err
	}()
	_, _ = pipeWriter.Write([]byte("x"))
	state, err := service.metadata.GetUpload(ctx, service.tenant(ctx), upload.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info, statErr := os.Stat(state.Location); statErr == nil && info.Size() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer never flushed")
		}
		time.Sleep(5 * time.Millisecond)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(10 * DefaultUploadTTL) }
	reaped, err := service.ReapAbandonedUploads(ctx)
	if err != nil || reaped != 0 {
		t.Fatalf("ReapAbandonedUploads() with a live writer = %d, %v, want 0, nil", reaped, err)
	}

	_ = pipeWriter.Close()
	if err := <-appendDone; err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
}

func TestReapAbandonedUploadsDisabledWithNonPositiveTTL(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadTTL(0)
	if _, err := service.BeginUpload(context.Background(), "team/a"); err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	service.now = func() time.Time { return time.Now().UTC().Add(1000 * time.Hour) }
	if reaped, err := service.ReapAbandonedUploads(context.Background()); err != nil || reaped != 0 {
		t.Fatalf("ReapAbandonedUploads() with TTL 0 = %d, %v, want 0, nil", reaped, err)
	}
}

func TestReapAbandonedUploadsDoesNotAffectGC(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	service.now = func() time.Time { return time.Now().UTC().Add(gcGraceWindow + time.Hour) }
	if _, err := service.ComputeGCReport(ctx); err != nil {
		t.Fatalf("ComputeGCReport() error = %v", err)
	}
	if _, err := service.UploadStatus(ctx, "team/a", upload.ID); err != nil {
		t.Fatalf("GC must not touch a live upload: %v", err)
	}
}

func mustDigest(t *testing.T, value string) domain.Digest {
	t.Helper()
	digest, err := domain.ParseDigest(value)
	if err != nil {
		t.Fatalf("ParseDigest(%q) error = %v", value, err)
	}
	return digest
}

func TestReapAbandonedUploadsRemovesOrphanDirsButNeverOnesWithARow(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	live, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	liveState, err := service.metadata.GetUpload(ctx, service.tenant(ctx), live.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}

	// An orphan: the directory exists but SaveUpload never ran (crash).
	orphan, err := service.blobs.BeginUpload(ctx, domain.MustParseRepositoryRef("team/a"))
	if err != nil {
		t.Fatalf("blobs.BeginUpload() error = %v", err)
	}

	now := time.Now().UTC()
	service.now = func() time.Time { return now }
	if reaped, err := service.ReapAbandonedUploads(ctx); err != nil || reaped != 0 {
		t.Fatalf("ReapAbandonedUploads() inside TTL = %d, %v, want 0, nil", reaped, err)
	}
	if _, err := os.Stat(orphan.Location); err != nil {
		t.Fatalf("fresh orphan must survive inside the TTL: %v", err)
	}

	// Make the live upload's row old enough to be reaped too, to show the two
	// paths do not double count, then jump past the TTL.
	service.now = func() time.Time { return now.Add(DefaultUploadTTL + time.Hour) }
	reaped, err := service.ReapAbandonedUploads(ctx)
	if err != nil {
		t.Fatalf("ReapAbandonedUploads() error = %v", err)
	}
	if reaped != 2 {
		t.Fatalf("reaped = %d, want 2 (the idle upload with a row, and the orphan)", reaped)
	}
	if _, err := os.Stat(orphan.Location); !os.IsNotExist(err) {
		t.Fatalf("orphan file still present: %v", err)
	}
	if _, err := os.Stat(liveState.Location); !os.IsNotExist(err) {
		t.Fatalf("idle upload file still present: %v", err)
	}
}

func TestReapAbandonedUploadsKeepsAnOldDirWhoseRowStillExists(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	state, err := service.metadata.GetUpload(ctx, service.tenant(ctx), upload.ID)
	if err != nil {
		t.Fatalf("GetUpload() error = %v", err)
	}
	// Row says "recently updated" while the files are old: the row wins.
	state.UpdatedAt = time.Now().UTC().Add(48 * time.Hour)
	if err := service.metadata.SaveUpload(ctx, service.tenant(ctx), state); err != nil {
		t.Fatalf("SaveUpload() error = %v", err)
	}
	service.now = func() time.Time { return time.Now().UTC().Add(DefaultUploadTTL + time.Hour) }
	if reaped, err := service.ReapAbandonedUploads(ctx); err != nil || reaped != 0 {
		t.Fatalf("ReapAbandonedUploads() = %d, %v, want 0, nil (row exists)", reaped, err)
	}
	if _, err := os.Stat(state.Location); err != nil {
		t.Fatalf("directory with a live row was removed: %v", err)
	}
}

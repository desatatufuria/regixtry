package regixtry

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	domain "regixtry/internal/domain/regixtry"
)

// An identity cannot keep sessions alive past the absolute lifetime by
// appending: the reaper frees them anyway, and append/complete are refused.
func TestUploadAbsoluteLifetimeIsNotExtendedByAppending(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadFairness(0, 72*time.Hour)
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	start := time.Now().UTC()

	// Every 12h the writer appends one byte; the file mtime and the row's
	// updated_at follow the (fake) clock, so the idle TTL never fires.
	for _, hours := range []int{12, 24, 36, 48, 60, 84} {
		now := start.Add(time.Duration(hours) * time.Hour)
		service.now = func() time.Time { return now }
		if hours < 84 {
			if _, err := service.AppendUpload(ctx, "team/a", upload.ID, strings.NewReader("x")); err != nil {
				t.Fatalf("AppendUpload() at +%dh error = %v", hours, err)
			}
		}
		state, err := service.metadata.GetUpload(ctx, service.tenant(ctx), upload.ID)
		if err != nil {
			t.Fatalf("GetUpload() error = %v", err)
		}
		if err := os.Chtimes(state.Location, now, now); err != nil {
			t.Fatalf("Chtimes() error = %v", err)
		}
		state.UpdatedAt = now
		if err := service.metadata.SaveUpload(ctx, service.tenant(ctx), state); err != nil {
			t.Fatalf("SaveUpload() error = %v", err)
		}
		reaped, err := service.ReapAbandonedUploads(ctx)
		if err != nil {
			t.Fatalf("ReapAbandonedUploads() at +%dh error = %v", hours, err)
		}
		if hours < 84 && reaped != 0 {
			t.Fatalf("reaped %d at +%dh, want 0 (inside the absolute lifetime and active)", reaped, hours)
		}
		if hours == 84 && (reaped != 1 || uploadRowCount(t, service) != 0) {
			t.Fatalf("reaped %d (rows %d) at +%dh, want the session reaped despite recent activity", reaped, uploadRowCount(t, service), hours)
		}
	}
}

func TestAppendAndCompleteRefuseASessionPastTheAbsoluteLifetime(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadFairness(0, 72*time.Hour)
	ctx := context.Background()

	upload, err := service.BeginUpload(ctx, "team/a")
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	// Older than the idle TTL but inside the absolute lifetime: still works.
	service.now = func() time.Time { return time.Now().UTC().Add(DefaultUploadTTL + time.Hour) }
	if _, err := service.AppendUpload(ctx, "team/a", upload.ID, strings.NewReader("slow")); err != nil {
		t.Fatalf("AppendUpload() beyond idle TTL but inside lifetime error = %v, want success", err)
	}

	service.now = func() time.Time { return time.Now().UTC().Add(73 * time.Hour) }
	_, appendErr := service.AppendUpload(ctx, "team/a", upload.ID, strings.NewReader("late"))
	if !domain.IsCode(appendErr, domain.ErrorCodeNotFound) {
		t.Fatalf("AppendUpload() past lifetime error = %v, want not found (like an expired upload)", appendErr)
	}
	payload := []byte("slow")
	_, completeErr := service.CompleteUpload(ctx, "team/a", upload.ID, digestForTest(payload), nil)
	if !domain.IsCode(completeErr, domain.ErrorCodeNotFound) {
		t.Fatalf("CompleteUpload() past lifetime error = %v, want not found", completeErr)
	}
	// Cancel still works so the owner can free the slot.
	if err := service.CancelUpload(ctx, "team/a", upload.ID); err != nil {
		t.Fatalf("CancelUpload() past lifetime error = %v, want success", err)
	}
}

package regixtry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func ctxAs(userID string) context.Context {
	return ports.ContextWithPrincipal(context.Background(), domainauth.Principal{
		Subject: "accessor-" + userID + "-" + fmt.Sprint(time.Now().UnixNano()),
		UserID:  userID,
	})
}

// One identity filling its own cap must not stop another identity.
func TestBeginUploadPerIdentityCapLeavesRoomForOthers(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadLimits(10, 10)
	service.SetUploadFairness(4, 0)

	for i := 0; i < 4; i++ {
		if _, err := service.BeginUpload(ctxAs("greedy"), fmt.Sprintf("team/r%d", i)); err != nil {
			t.Fatalf("greedy BeginUpload #%d error = %v", i, err)
		}
	}
	_, err := service.BeginUpload(ctxAs("greedy"), "team/extra")
	if !domain.IsCode(err, domain.ErrorCodeTooManyRequests) {
		t.Fatalf("greedy 5th BeginUpload error = %v, want too many requests (per-identity cap)", err)
	}
	if err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("refusal message = %v, want it to name the per-identity cap", err)
	}
	// A different identity (even another token of the same user id is the
	// same identity, but this is a different user) is unaffected.
	if _, err := service.BeginUpload(ctxAs("honest"), "team/other"); err != nil {
		t.Fatalf("other identity BeginUpload error = %v, want success", err)
	}
	// Cancelling frees the greedy identity's own slot.
	// (global cap is 10: 4 greedy + 1 honest are open)
	if got := uploadRowCount(t, service); got != 5 {
		t.Fatalf("open uploads = %d, want 5 (refused uploads leave nothing behind)", got)
	}
}

// Anonymous mode (no principal) is a single bucket, exactly like before.
func TestBeginUploadAnonymousIsOneBucket(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadFairness(2, 0)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := service.BeginUpload(ctx, fmt.Sprintf("team/r%d", i)); err != nil {
			t.Fatalf("anonymous BeginUpload #%d error = %v", i, err)
		}
	}
	if _, err := service.BeginUpload(ctx, "team/r2"); !domain.IsCode(err, domain.ErrorCodeTooManyRequests) {
		t.Fatalf("third anonymous BeginUpload error = %v, want too many requests", err)
	}
}

// Parallel BeginUpload never exceeds the per-identity or the global cap.
func TestBeginUploadFairnessCapsHoldUnderConcurrency(t *testing.T) {
	t.Parallel()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.SetUploadLimits(12, 12)
	service.SetUploadFairness(5, 0)

	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := map[string]int{}
	for _, id := range []string{"a", "b", "c"} {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := service.BeginUpload(ctxAs(id), fmt.Sprintf("team/%s%d", id, i%7)); err == nil {
					mu.Lock()
					accepted[id]++
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()

	total := 0
	for id, n := range accepted {
		if n > 5 {
			t.Fatalf("identity %s holds %d uploads, cap is 5", id, n)
		}
		total += n
	}
	if total > 12 || uploadRowCount(t, service) != total {
		t.Fatalf("accepted %d, rows %d, want equal and <= 12", total, uploadRowCount(t, service))
	}
	if total != 12 {
		t.Fatalf("accepted %d, want the global cap of 12 reached (3 identities x 5 > 12)", total)
	}
}

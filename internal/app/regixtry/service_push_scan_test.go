package regixtry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// pushDistinctManifests publishes n manifests that share one blob but differ
// by an annotation, so every push yields a distinct digest (defeating the
// per-digest dedup).
func pushDistinctManifests(t *testing.T, service *Service, repository string, n int, prefix string) {
	t.Helper()
	pushDistinctManifestsAs(t, service, repository, n, prefix, "")
}

func pushDistinctManifestsAs(t *testing.T, service *Service, repository string, n int, prefix string, userID string) {
	t.Helper()
	ctx := context.Background()
	if userID != "" {
		ctx = ports.ContextWithPrincipal(ctx, domainauth.Principal{UserID: userID})
	}
	upload, err := service.BeginUpload(ctx, repository)
	if err != nil {
		t.Fatalf("BeginUpload() error = %v", err)
	}
	if _, err := service.AppendUpload(ctx, repository, upload.ID, strings.NewReader("layer-one")); err != nil {
		t.Fatalf("AppendUpload() error = %v", err)
	}
	blob, err := service.CompleteUpload(ctx, repository, upload.ID, digestForTest([]byte("layer-one")), nil)
	if err != nil {
		t.Fatalf("CompleteUpload() error = %v", err)
	}
	for i := 0; i < n; i++ {
		payload := []byte(fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":9},"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"%s","size":9}],"annotations":{"n":"%s-%d"}}`, blob.Digest, blob.Digest, prefix, i))
		if _, err := service.PublishManifest(ctx, repository, fmt.Sprintf("%s-%d", prefix, i), "application/vnd.oci.image.manifest.v1+json", payload); err != nil {
			t.Fatalf("PublishManifest(%d) error = %v", i, err)
		}
	}
}

func newPushScanService(t *testing.T) (*Service, *blockingScanRunner, func()) {
	t.Helper()
	service, cleanup := newTestService(t, allowAllAccessController{})
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	runner := &blockingScanRunner{release: make(chan struct{})}
	service.SetScanRunner(runner)
	return service, runner, func() {
		close(runner.release)
		time.Sleep(50 * time.Millisecond)
		cleanup()
	}
}

func countActivePushScans(t *testing.T, service *Service) (active int, skipped int) {
	t.Helper()
	runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", "", 5000)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	for _, run := range runs {
		switch {
		case run.Status == ports.ScanRunStatusQueued || run.Status == ports.ScanRunStatusRunning:
			active++
		case run.Status == ports.ScanRunStatusSkipped:
			skipped++
		}
	}
	return active, skipped
}

// TestPushScansAreBoundedWhenManyDistinctManifestsArePushed reproduces the
// audit lead: one writer pushing many distinct digests while scans are held
// must not grow queued rows or goroutines without bound.
func TestPushScansAreBoundedWhenManyDistinctManifestsArePushed(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()

	const pushes = 150
	pushDistinctManifests(t, service, "library/alpine", pushes, "flood")
	// Synchronization point: every admitted push has persisted its queued
	// row (and all others a skipped notice) once the slots in use stop
	// changing and no push goroutine is mid-flight.
	service.WaitForBackgroundWork()
	waitUntil(t, "admitted scans to be running", func() bool {
		active, _ := countActivePushScans(t, service)
		return active == pushScanSlotsInUse(service)
	})

	active, skipped := countActivePushScans(t, service)
	t.Logf("active=%d skipped=%d", active, skipped)
	if active > 64 {
		t.Fatalf("active scans = %d for %d pushes, want <= 64 (bounded queue)", active, pushes)
	}
	if skipped == 0 {
		t.Fatalf("skipped records = 0, want a visible 'skipped: ...' scan result for dropped pushes")
	}
}

func TestPushScanLimitsAreConfigurableAndSkippedRowIsOnePerRepository(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()
	service.SetPushScanLimits(5, 2, 0)

	pushDistinctManifests(t, service, "library/alpine", 20, "cfg")
	time.Sleep(500 * time.Millisecond)

	active, skipped := countActivePushScans(t, service)
	if active != 2 || skipped != 1 {
		t.Fatalf("active=%d skipped=%d, want 2 active (per-repository cap) and exactly 1 skipped row", active, skipped)
	}
}

func TestPushScansStillRunAtNormalRates(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 2}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	service.SetScanRunner(&fakeScanRunner{})

	// One push at a time, waiting for each scan: slots must be released.
	for i := 0; i < 20; i++ {
		pushDistinctManifests(t, service, "library/alpine", 1, fmt.Sprintf("slow%d", i))
		deadline := time.Now().Add(5 * time.Second)
		for {
			service.pushScans.mu.Lock()
			total := service.pushScans.total
			service.pushScans.mu.Unlock()
			if total == 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("push %d: slot never released (total=%d)", i, total)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	runs, err := service.metadata.ListScanRuns(context.Background(), "tenant-a", "", 100)
	if err != nil {
		t.Fatalf("ListScanRuns() error = %v", err)
	}
	completed := 0
	for _, run := range runs {
		if run.Status == ports.ScanRunStatusCompleted {
			completed++
		}
		if run.Status == ports.ScanRunStatusSkipped {
			t.Fatalf("unexpected skipped run at normal push rate: %#v", run)
		}
	}
	if completed < 20 {
		t.Fatalf("completed scans = %d, want 20", completed)
	}
}

func TestPushScanSkipIsSilentWhenScanningDisabled(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: false, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	service.SetPushScanLimits(1, 1, 0)
	for i := 0; i < 5; i++ {
		service.pushScans.acquire("tenant-a", "library/alpine", "")
	}
	service.recordPushScanSkipped(context.Background(), "tenant-a", "library/alpine", "latest", "sha256:"+strings.Repeat("a", 64))
	if _, skipped := countActivePushScans(t, service); skipped != 0 {
		t.Fatalf("skipped rows = %d, want 0 when scanning is disabled", skipped)
	}
}

func TestPushScanLimiterConcurrentAcquireNeverExceedsCaps(t *testing.T) {
	t.Parallel()
	l := newPushScanLimiter()
	l.setLimits(10, 4, 100)
	var mu sync.Mutex
	held, maxHeld := 0, 0
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			release, ok := l.acquire("t", fmt.Sprintf("repo/%d", i%5), "p")
			if !ok {
				return
			}
			mu.Lock()
			held++
			if held > maxHeld {
				maxHeld = held
			}
			mu.Unlock()
			time.Sleep(time.Millisecond)
			mu.Lock()
			held--
			mu.Unlock()
			release()
			release() // idempotent
		}(i)
	}
	wg.Wait()
	if maxHeld > 10 {
		t.Fatalf("max concurrently held = %d, want <= 10", maxHeld)
	}
	if l.total != 0 || len(l.perRepo) != 0 {
		t.Fatalf("limiter leaked: total=%d perRepo=%v", l.total, l.perRepo)
	}
}

// seedCompletedCritical stores an older completed run with a critical
// finding for the digest and enables the pull policy.
func seedCompletedCritical(t *testing.T, service *Service, repository string, digest string) {
	t.Helper()
	ctx := context.Background()
	if _, err := service.UpdateScanPolicySettings(ctx, ports.ScanPolicySettings{Enabled: true, SeverityThreshold: ports.ScanPolicyThresholdCritical}); err != nil {
		t.Fatalf("UpdateScanPolicySettings() error = %v", err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	run := ports.ScanRun{ID: "run-critical", Repository: repository, RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: past, UpdatedAt: past, FinishedAt: &past, Critical: 1}
	if err := service.metadata.UpsertScanRun(ctx, "tenant-a", run); err != nil {
		t.Fatalf("UpsertScanRun() error = %v", err)
	}
}

func TestSkippedPushScanNeverUnblocksAKnownCriticalDigest(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()
	digest := "sha256:" + strings.Repeat("d", 64)
	seedCompletedCritical(t, service, "library/alpine", digest)
	if err := service.enforceScanPolicy(context.Background(), "library/alpine", digest); err == nil {
		t.Fatal("precondition: pull of a critical digest must be blocked")
	}

	service.SetPushScanLimits(2, 2, 0)
	for i := 0; i < 2; i++ {
		if _, ok := service.pushScans.acquire("tenant-a", "library/alpine", ""); !ok {
			t.Fatal("could not fill the cap")
		}
	}
	service.startPushScan("tenant-a", "library/alpine", "latest", digest, "")
	service.WaitForBackgroundWork() // drains the skipped-notice recorder

	if err := service.enforceScanPolicy(context.Background(), "library/alpine", digest); err == nil {
		t.Fatal("pull allowed after a dropped re-push: the skipped row hid the critical result")
	}
	summaries, err := service.ListLatestScanRunPerRepository(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Run.Status != ports.ScanRunStatusCompleted || summaries[0].Run.Critical != 1 {
		t.Fatalf("summary = %#v, want the real completed critical run", summaries)
	}
	if _, skipped := countActivePushScans(t, service); skipped != 0 {
		t.Fatalf("skipped rows = %d, want none for a digest that already has a real run", skipped)
	}
}

func TestSkippedPushScanForUnscannedDigestStaysVisibleButNeverWinsLatest(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()
	ctx := context.Background()
	service.SetPushScanLimits(1, 1, 0)
	service.pushScans.acquire("tenant-a", "library/alpine", "")
	digest := "sha256:" + strings.Repeat("e", 64)
	service.startPushScan("tenant-a", "library/alpine", "latest", digest, "")
	service.WaitForBackgroundWork() // drains the skipped-notice recorder

	runs, _ := service.metadata.ListScanRuns(ctx, "tenant-a", "library/alpine", 10)
	if len(runs) != 1 || runs[0].Status != ports.ScanRunStatusSkipped || runs[0].Error != pushScanSkippedError {
		t.Fatalf("runs = %#v, want one visible skipped row", runs)
	}
	if err := service.enforceScanPolicy(ctx, "library/alpine", digest); err != nil {
		t.Fatalf("enforceScanPolicy() = %v, want allow (fail-open for unscanned)", err)
	}
	// A real run created later still wins over the skipped marker, and an
	// older real run wins even though the marker is newer.
	past := time.Now().UTC().Add(-time.Hour)
	real := ports.ScanRun{ID: "real", Repository: "library/alpine", RequestedRef: "latest", Digest: digest, Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: past, UpdatedAt: past, Critical: 2}
	if err := service.metadata.UpsertScanRun(ctx, "tenant-a", real); err != nil {
		t.Fatal(err)
	}
	latest, err := service.metadata.GetLatestScanRunByDigest(ctx, "tenant-a", "library/alpine", digest)
	if err != nil || latest.ID != "real" {
		t.Fatalf("latest = %#v err=%v, want the real run", latest, err)
	}
}

func TestHungScanRunnerFreesItsSlotAfterTheScanTimeout(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.scanRunMargin = 20 * time.Millisecond
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: 50 * time.Millisecond, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	service.SetScanRunner(&fakeScanRunner{release: make(chan struct{})}) // never released; honors ctx

	service.startPushScan("tenant-a", "library/alpine", "latest", "sha256:"+strings.Repeat("1", 64), "p")
	deadline := time.Now().Add(3 * time.Second)
	for {
		service.pushScans.mu.Lock()
		total := service.pushScans.total
		service.pushScans.mu.Unlock()
		if total == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hung runner still holds its push-scan slot after the scan timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
	runs, _ := service.metadata.ListScanRuns(context.Background(), "tenant-a", "library/alpine", 10)
	if len(runs) != 1 || runs[0].Status != ports.ScanRunStatusFailed {
		t.Fatalf("runs = %#v, want one failed (timed out) run", runs)
	}
}

func TestQueuedPushScansTimeOutWaitingForASlot(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	service.scanRunMargin = 20 * time.Millisecond
	if _, err := service.EnsureScanSettings(context.Background(), ports.ScanSettings{Enabled: true, Timeout: 50 * time.Millisecond, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
		t.Fatalf("EnsureScanSettings() error = %v", err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	service.SetScanRunner(&blockingScanRunner{release: make(chan struct{})})
	for i := 0; i < 3; i++ {
		service.startPushScan("tenant-a", "library/alpine", "latest", "sha256:"+strings.Repeat(string(rune('a'+i)), 64), "p")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		service.pushScans.mu.Lock()
		total := service.pushScans.total
		service.pushScans.mu.Unlock()
		if total == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("queued scans never released their slots")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOnePusherWithManyRepositoriesCannotFillTheGlobalCap(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()
	service.SetPushScanLimits(40, 8, 12)

	for r := 0; r < 10; r++ {
		pushDistinctManifestsAs(t, service, fmt.Sprintf("attacker/repo%d", r), 8, "x", "attacker")
	}
	time.Sleep(time.Second)
	active, _ := countActivePushScans(t, service)
	if active > 12 {
		t.Fatalf("attacker holds %d slots across 10 repositories, want <= 12 (per-pusher cap)", active)
	}

	pushDistinctManifestsAs(t, service, "victim/app", 1, "v", "victim")
	time.Sleep(300 * time.Millisecond)
	runs, _ := service.metadata.ListScanRuns(context.Background(), "tenant-a", "victim/app", 10)
	if len(runs) != 1 || runs[0].Status == ports.ScanRunStatusSkipped {
		t.Fatalf("victim runs = %#v, want a real queued/running scan despite the attacker flood", runs)
	}
}

func TestSkippedRowIsTenantScopedAndTracksTheNewestDroppedPush(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()
	ctx := context.Background()
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if err := service.metadata.UpsertFeatureRuntimeState(ctx, tenant, "trivy", ports.FeatureRuntimeState{Status: ports.FeatureRuntimeStatusReady, ActiveBinaryPath: "/x/trivy", CacheDir: "/x/c", UpdatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.metadata.GetScanSettings(ctx, tenant, "trivy"); err != nil {
			if err := service.metadata.UpsertScanSettings(ctx, tenant, "trivy", ports.ScanSettings{Enabled: true, Timeout: time.Minute, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1, UpdatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	d1 := "sha256:" + strings.Repeat("1", 64)
	d2 := "sha256:" + strings.Repeat("2", 64)
	d3 := "sha256:" + strings.Repeat("3", 64)
	service.recordPushScanSkipped(ctx, "tenant-a", "library/alpine", "t1", d1)
	service.recordPushScanSkipped(ctx, "tenant-b", "library/alpine", "tb", d3)
	time.Sleep(5 * time.Millisecond)
	service.recordPushScanSkipped(ctx, "tenant-a", "library/alpine", "t2", d2)

	runsA, _ := service.metadata.ListScanRuns(ctx, "tenant-a", "library/alpine", 10)
	runsB, _ := service.metadata.ListScanRuns(ctx, "tenant-b", "library/alpine", 10)
	if len(runsA) != 1 || runsA[0].Digest != d2 || runsA[0].RequestedRef != "t2" {
		t.Fatalf("tenant-a runs = %#v, want one row holding the newest dropped push", runsA)
	}
	if len(runsB) != 1 || runsB[0].Digest != d3 {
		t.Fatalf("tenant-b runs = %#v, want its own untouched row", runsB)
	}
	if runsA[0].ID == runsB[0].ID {
		t.Fatalf("both tenants share skipped row id %q", runsA[0].ID)
	}

	// The refreshed (newer created_at) skipped row still never outranks a
	// real run of the same digest, even an older one.
	past := time.Now().UTC().Add(-time.Hour)
	if err := service.metadata.UpsertScanRun(ctx, "tenant-a", ports.ScanRun{ID: "real-d2", Repository: "library/alpine", RequestedRef: "t2", Digest: d2, Status: ports.ScanRunStatusCompleted, Trigger: ports.ScanTriggerManual, CreatedAt: past, UpdatedAt: past, Critical: 1}); err != nil {
		t.Fatal(err)
	}
	latest, err := service.metadata.GetLatestScanRunByDigest(ctx, "tenant-a", "library/alpine", d2)
	if err != nil || latest.ID != "real-d2" {
		t.Fatalf("latest = %#v err=%v, want the real run", latest, err)
	}
}

func pushScanSlotsInUse(service *Service) int {
	service.pushScans.mu.Lock()
	defer service.pushScans.mu.Unlock()
	return service.pushScans.total
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	waitUntilWithin(t, 5*time.Second, what, cond)
}

func waitUntilWithin(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// hangingSecretRunner blocks forever; with ignoreCtx it also ignores
// cancellation, so only the caller giving up can free the slot.
type hangingSecretRunner struct {
	ignoreCtx bool
	stop      chan struct{}
}

func (h *hangingSecretRunner) Run(ctx context.Context, _ ports.SecretScanTarget, _ ports.ScanSettings) (ports.SecretScanResult, error) {
	if h.ignoreCtx {
		<-h.stop
		return ports.SecretScanResult{}, nil
	}
	select {
	case <-ctx.Done():
		return ports.SecretScanResult{}, ctx.Err()
	case <-h.stop:
		return ports.SecretScanResult{}, nil
	}
}

func TestHungSecretScanLegNeverHoldsAPushScanSlotForever(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ignoreCtx bool
	}{{"honors cancellation", false}, {"ignores cancellation", true}} {
		t.Run(tc.name, func(t *testing.T) {
			service, cleanup := newTestService(t, allowAllAccessController{})
			defer cleanup()
			service.scanRunMargin = 20 * time.Millisecond
			ctx := context.Background()
			seedRepository(t, service, ctx, "library/alpine")
			digest := seededManifestDigest(t, service, "library/alpine")
			if _, err := service.EnsureScanSettings(ctx, ports.ScanSettings{Enabled: true, Timeout: 50 * time.Millisecond, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 1}); err != nil {
				t.Fatalf("EnsureScanSettings() error = %v", err)
			}
			seedManagedRuntimeState(t, service, "0.57.1")
			seedManagedGitleaksSettings(t, service, true)
			runner := &hangingSecretRunner{ignoreCtx: tc.ignoreCtx, stop: make(chan struct{})}
			defer close(runner.stop)
			service.SetScanRunner(&fakeScanRunner{})
			service.SetSecretScanRunner(runner)

			service.startPushScan("tenant-a", "library/alpine", "latest", digest, "p")
			waitUntil(t, "the hung secret leg to free the push-scan slot", func() bool { return pushScanSlotsInUse(service) == 0 })
		})
	}
}

func TestStuckSecretLegsAreCountedBoundedAndRecoverWhenTheyReturn(t *testing.T) {
	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
	// Generous bounds: each stuck leg is abandoned after 2*(Timeout+margin) =
	// 1s, long enough that a loaded machine still starts the runner first. The
	// legs are started together and the test polls state, never fixed sleeps.
	service.scanRunMargin = 200 * time.Millisecond
	ctx := context.Background()
	// The secret leg dedups per repository@digest, so each push uses its own
	// repository (each seeded with the same manifest bytes).
	var digest string
	for i := 0; i <= maxStuckSecretLegs; i++ {
		seedRepository(t, service, ctx, fmt.Sprintf("library/r%d", i))
		digest = seededManifestDigest(t, service, "library/r0")
	}
	if _, err := service.EnsureScanSettings(ctx, ports.ScanSettings{Enabled: true, Timeout: 300 * time.Millisecond, Interval: time.Hour, RegistryReachableURL: "https://registry.internal", MaxConcurrency: 64}); err != nil {
		t.Fatal(err)
	}
	seedManagedRuntimeState(t, service, "0.57.1")
	seedManagedGitleaksSettings(t, service, true)
	// A stuck leg also holds the secret gate, so allow enough concurrency
	// for every leak to be reached.
	if err := service.metadata.UpsertScanSettings(ctx, "tenant-a", gitleaksFeatureName, ports.ScanSettings{Enabled: true, Interval: time.Hour, Timeout: time.Minute, MaxConcurrency: 64, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	runner := &hangingSecretRunner{ignoreCtx: true, stop: make(chan struct{})}
	service.SetScanRunner(&fakeScanRunner{})
	service.SetSecretScanRunner(runner)
	service.SetPushScanLimits(1000, 1000, 1000)

	// Each scan leaks one stuck leg until the limit is reached.
	for i := 0; i < maxStuckSecretLegs; i++ {
		service.startPushScan("tenant-a", fmt.Sprintf("library/r%d", i), "latest", digest, "p")
	}
	waitUntilWithin(t, 60*time.Second, "every stuck leg to be counted and every slot released", func() bool {
		return service.stuckSecretLegs.Load() == maxStuckSecretLegs && pushScanSlotsInUse(service) == 0
	})
	service.startPushScan("tenant-a", fmt.Sprintf("library/r%d", maxStuckSecretLegs), "latest", digest, "p")
	service.WaitForBackgroundWork()
	if got := service.stuckSecretLegs.Load(); got != maxStuckSecretLegs {
		t.Fatalf("stuck legs = %d, want exactly the bound %d (new pushes must be refused)", got, maxStuckSecretLegs)
	}
	if _, skipped := countActivePushScans(t, service); skipped != 1 {
		t.Fatalf("skipped rows = %d, want the refused push recorded as skipped", skipped)
	}

	close(runner.stop) // the stuck runners finally return
	waitUntilWithin(t, 60*time.Second, "stuck legs to drain", func() bool { return service.stuckSecretLegs.Load() == 0 })
}

// gatedStore blocks the two store calls recordPushScanSkipped makes until
// opened, standing in for serialized sqlite work that stalls.
type gatedStore struct {
	ports.MetadataStore
	mu     sync.Mutex
	closed bool
	open   chan struct{}
}

func (g *gatedStore) wait() {
	g.mu.Lock()
	closed := g.closed
	g.mu.Unlock()
	if closed {
		<-g.open
	}
}

func (g *gatedStore) UpsertScanRun(ctx context.Context, tenant string, run ports.ScanRun) error {
	g.wait()
	return g.MetadataStore.UpsertScanRun(ctx, tenant, run)
}

func (g *gatedStore) GetLatestScanRunByDigest(ctx context.Context, tenant string, repository string, digest string) (ports.ScanRun, error) {
	g.wait()
	return g.MetadataStore.GetLatestScanRunByDigest(ctx, tenant, repository, digest)
}

func (g *gatedStore) block() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

func TestOverCapPushNeverBlocksOnTheStoreAndSkippedRowsAreCoalesced(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	gate := &gatedStore{MetadataStore: service.metadata, open: make(chan struct{})}
	service.metadata = gate
	unblocked := false
	defer func() {
		if !unblocked {
			close(gate.open)
		}
		cleanup()
	}()

	service.SetPushScanLimits(1, 1, 0)
	if _, ok := service.pushScans.acquire("tenant-a", "library/a", ""); !ok { // fill the global cap
		t.Fatal("could not fill the cap")
	}
	gate.block()

	repos := []string{"library/a", "library/b", "library/c"}
	start := time.Now()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, repo := range repos {
			pushDistinctManifests(t, service, repo, 170, "over") // 510 over-cap pushes
		}
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("PublishManifest blocked on the stalled store while over cap")
	}
	t.Logf("510 over-cap pushes took %s", time.Since(start))

	pending, workers := service.skippedPushScans.stats()
	if workers > 1 || pending > len(repos) {
		t.Fatalf("recorder pending=%d workers=%d, want <= %d coalesced entries and a single worker", pending, workers, len(repos))
	}

	unblocked = true
	close(gate.open)
	waitUntil(t, "skipped rows to be recorded once the store unblocks", func() bool {
		_, skipped := countActivePushScans(t, service)
		return skipped == len(repos)
	})
}

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
		select {
		case <-runner.release: // already released by the test
		default:
			close(runner.release)
		}
		service.WaitForBackgroundWork()
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

func pushScanDigest(seed string, i int) string {
	return "sha256:" + fmt.Sprintf("%s%d", seed, i) + strings.Repeat("0", 64-len(seed)-len(fmt.Sprint(i)))
}

func latestPushScanStatus(service *Service, repository string, digest string) string {
	run, err := service.metadata.GetLatestScanRunByDigest(context.Background(), "tenant-a", repository, digest)
	if err != nil {
		return "none"
	}
	return run.Status
}

// TestSaturatingIdentitiesCannotStarveAnotherWritersPushScan reproduces the
// run-2 audit lead: several writer identities, each inside its own per-pusher
// cap, used to fill the whole global pool so a different writer's push was
// never admitted. The last quarter of the pool is now reserved for
// identities that hold no slot yet.
func TestSaturatingIdentitiesCannotStarveAnotherWritersPushScan(t *testing.T) {
	service, _, cleanup := newPushScanService(t)
	defer cleanup()
	service.SetPushScanLimits(8, 8, 4)

	for i := 0; i < 6; i++ {
		service.startPushScan("tenant-a", "a1/app", "t", pushScanDigest("a", i), "writer-a1")
	}
	for i := 0; i < 6; i++ {
		service.startPushScan("tenant-a", "a2/app", "t", pushScanDigest("b", i), "writer-a2")
	}
	victim := pushScanDigest("c", 0)
	service.startPushScan("tenant-a", "b/app", "t", victim, "writer-b")

	waitUntil(t, "the victim's push to be admitted as a real scan", func() bool {
		status := latestPushScanStatus(service, "b/app", victim)
		return status == ports.ScanRunStatusQueued || status == ports.ScanRunStatusRunning
	})
	if got := pushScanSlotsInUse(service); got > 8 {
		t.Fatalf("slots in use = %d, want <= 8 (global cap)", got)
	}
}

func TestPushScanLimiterNeverExceedsCapsUnderParallelIdentitiesAndLeaksNoSlot(t *testing.T) {
	limiter := newPushScanLimiter()
	limiter.setLimits(24, 6, 5)
	var mu sync.Mutex
	var releases []func()
	var wg sync.WaitGroup
	for id := 0; id < 12; id++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if release, ok := limiter.acquire("t", fmt.Sprintf("repo%d/app", i%4), fmt.Sprintf("id%d", id)); ok {
					mu.Lock()
					releases = append(releases, release)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	limiter.mu.Lock()
	total := limiter.total
	for pusher, n := range limiter.perPusher {
		if n > 5 {
			t.Errorf("pusher %q holds %d, want <= 5", pusher, n)
		}
	}
	limiter.mu.Unlock()
	if total > 24 || total != len(releases) {
		t.Fatalf("total = %d with %d held slots, want <= 24 and consistent", total, len(releases))
	}
	for _, release := range releases {
		release()
		release() // idempotent
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.total != 0 || len(limiter.perRepo) != 0 || len(limiter.perPusher) != 0 {
		t.Fatalf("slots leaked: total=%d repo=%d pusher=%d", limiter.total, len(limiter.perRepo), len(limiter.perPusher))
	}
}

// waitForPushScansIdle waits until no slot is held, nothing is deferred and
// the drainer has stopped (releases happen on goroutines WaitForBackgroundWork
// does not cover).
func waitForPushScansIdle(t *testing.T, service *Service) {
	t.Helper()
	waitUntil(t, "push scans to go idle", func() bool {
		l := service.pushScans
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.total == 0 && len(l.deferred) == 0 && !l.drainRunning
	})
	service.WaitForBackgroundWork()
}

// publishedDigests resolves the digests of tags prefix-0..n-1.
func publishedDigests(t *testing.T, service *Service, repository string, prefix string, n int) []string {
	t.Helper()
	ref, err := parseRepository(repository)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, n)
	for i := range out {
		m, err := service.metadata.ResolveManifest(context.Background(), "tenant-a", ref, fmt.Sprintf("%s-%d", prefix, i))
		if err != nil {
			t.Fatalf("ResolveManifest(%s-%d) = %v", prefix, i, err)
		}
		out[i] = m.Digest.String()
	}
	return out
}

// TestRefusedPushScansAreDeferredAndScannedOnceSlotsFree: a push refused at
// admission is not dropped; it is retried within the caps, so it is scanned
// even though the scheduled sweep is disabled (ScheduleEnabled defaults to
// false) and every slot is released afterwards.
func TestRefusedPushScansAreDeferredAndScannedOnceSlotsFree(t *testing.T) {
	service, runner, cleanup := newPushScanService(t)
	service.SetPushScanLimits(2, 8, 2)
	defer cleanup()

	pushDistinctManifestsAs(t, service, "a/app", 5, "d", "writer-a")
	digests := publishedDigests(t, service, "a/app", "d", 5)
	waitUntil(t, "two admitted scans", func() bool { return pushScanSlotsInUse(service) == 2 })

	service.SetScanRunner(&fakeScanRunner{})
	close(runner.release)
	waitUntilWithin(t, 10*time.Second, "every deferred digest to complete", func() bool {
		for _, d := range digests {
			if latestPushScanStatus(service, "a/app", d) != ports.ScanRunStatusCompleted {
				return false
			}
		}
		return true
	})
	waitForPushScansIdle(t, service)
}

// TestDeferredScanOfADeletedManifestIsDroppedWithoutARow: deleting a manifest
// while its scan is deferred must not scan it or leave a result row.
func TestDeferredScanOfADeletedManifestIsDroppedWithoutARow(t *testing.T) {
	service, runner, cleanup := newPushScanService(t)
	service.SetPushScanLimits(1, 8, 1)
	defer cleanup()

	pushDistinctManifestsAs(t, service, "a/app", 2, "s", "writer-a")
	digests := publishedDigests(t, service, "a/app", "s", 2)
	waitUntil(t, "one admitted scan", func() bool { return pushScanSlotsInUse(service) == 1 })
	// s-1 was refused and deferred: delete it, then free the slot.
	ref, _ := parseRepository("a/app")
	digest, err := domain.ParseDigest(digests[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.metadata.DeleteManifestByDigest(context.Background(), "tenant-a", ref, digest); err != nil {
		t.Fatal(err)
	}
	fake := &fakeScanRunner{}
	service.SetScanRunner(fake)
	close(runner.release)
	waitForPushScansIdle(t, service)

	fake.mu.Lock()
	calls := len(fake.calls)
	fake.mu.Unlock()
	if calls != 0 {
		t.Fatalf("scanner called %d times for deferred work, want 0 (manifest deleted)", calls)
	}
	runs, _ := service.metadata.ListScanRuns(context.Background(), "tenant-a", "a/app", 50)
	for _, run := range runs {
		if run.Digest == digests[1] && run.Status != ports.ScanRunStatusSkipped {
			t.Fatalf("deleted digest left a %q row, want none", run.Status)
		}
	}
}

func TestDeferredListFullDropsAreCountedAndLoggedAtMostOncePerMinute(t *testing.T) {
	l := newPushScanLimiter()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if n, due := l.noteDeferDropped(t0); !due || n != 1 {
		t.Fatalf("first drop = (%d,%v), want (1,true)", n, due)
	}
	for i := 0; i < 5; i++ {
		if _, due := l.noteDeferDropped(t0.Add(time.Duration(i+1) * 10 * time.Second)); due && i < 4 {
			t.Fatalf("drop %d logged inside the minute", i)
		}
	}
	// 5 drops at +10s..+50s: none due; the one at +50s is the 5th, still inside.
	if n, due := l.noteDeferDropped(t0.Add(61 * time.Second)); !due || n != 6 {
		t.Fatalf("drop after a minute = (%d,%v), want (6,true) covering all drops since the last line", n, due)
	}
	if l.deferredDropped != 7 {
		t.Fatalf("deferredDropped = %d, want 7", l.deferredDropped)
	}
}

func TestAnonymousPushScansKeepTheBaselinePerIdentityCapWithoutReserve(t *testing.T) {
	l := newPushScanLimiter()
	for i := 0; i < 16; i++ {
		if _, ok := l.acquire("t", fmt.Sprintf("r%d/app", i), ""); !ok {
			t.Fatalf("anonymous acquire %d refused, want admitted up to 16", i)
		}
	}
	if _, ok := l.acquire("t", "r99/app", ""); ok {
		t.Fatal("17th anonymous scan admitted, want the baseline cap of 16")
	}
	// No reserve for the shared anonymous bucket: it may use the whole pool.
	small := newPushScanLimiter()
	small.setLimits(8, 8, 0)
	for i := 0; i < 8; i++ {
		if _, ok := small.acquire("t", fmt.Sprintf("r%d/app", i), ""); !ok {
			t.Fatalf("anonymous acquire %d refused below the global cap", i)
		}
	}
	// A named identity still gets 8 as its cap and the reserve.
	named := newPushScanLimiter()
	named.setLimits(8, 8, 0)
	granted := 0
	for i := 0; i < 8; i++ {
		if _, ok := named.acquire("t", fmt.Sprintf("r%d/app", i), "u"); ok {
			granted++
		}
	}
	if granted != 6 {
		t.Fatalf("named identity granted %d of 8, want 6 (reserve keeps 2)", granted)
	}
}

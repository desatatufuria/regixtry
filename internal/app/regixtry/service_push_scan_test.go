package regixtry

import (
	"context"
	"fmt"
	"runtime"
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
	before := runtime.NumGoroutine()
	pushDistinctManifests(t, service, "library/alpine", pushes, "flood")
	time.Sleep(time.Second)

	active, skipped := countActivePushScans(t, service)
	grown := runtime.NumGoroutine() - before
	t.Logf("active=%d skipped=%d goroutine growth=%d", active, skipped, grown)
	if active > 64 {
		t.Fatalf("active scans = %d for %d pushes, want <= 64 (bounded queue)", active, pushes)
	}
	if grown > 64+20 {
		t.Fatalf("goroutine growth = %d, want bounded", grown)
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

// Not parallel: it shrinks the package-level scanRunMargin.
func TestHungScanRunnerFreesItsSlotAfterTheScanTimeout(t *testing.T) {
	old := scanRunMargin
	scanRunMargin = 20 * time.Millisecond
	defer func() { scanRunMargin = old }()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
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
	old := scanRunMargin
	scanRunMargin = 20 * time.Millisecond
	defer func() { scanRunMargin = old }()

	service, cleanup := newTestService(t, allowAllAccessController{})
	defer cleanup()
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

package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCheckForUpdateCmdMissingCachePerformsRealCheckAndWritesCache guards the
// throttle-tui-update-check feature's baseline: a fresh/missing cache file
// must never change checkForUpdateCmd's existing behavior (it still performs
// a real check, calling QueryService.GetUpdateChannel exactly once), and
// afterward the check's outcome must be persisted to the cache file so the
// NEXT call can throttle. Uses a GetUpdateChannel failure (like
// TestCheckForUpdateCmdResolvesChannelFromServiceBeforeFetchingReleases
// already does) to exercise a real check without ever reaching the real
// GitHub API.
func TestCheckForUpdateCmdMissingCachePerformsRealCheckAndWritesCache(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "update-check-cache.json")
	fixedNow := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	service := &fakeQueryService{updateChannelErr: errors.New("metadata store unavailable")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"), WithUpdateCheckCachePath(cachePath))
	model.now = func() time.Time { return fixedNow }

	cmd := model.checkForUpdateCmd()
	if cmd == nil {
		t.Fatal("checkForUpdateCmd() = nil, want a non-nil Cmd")
	}
	msg, ok := cmd().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 1 {
		t.Fatalf("GetUpdateChannel call count = %d, want exactly 1 -- a missing cache must still perform a real check", service.calls.updateChannel)
	}
	if msg.err == nil {
		t.Fatal("updateCheckCompletedMsg.err = nil, want the real check's error surfaced")
	}

	entry := readCacheFileForTest(t, cachePath)
	if !entry.Failed {
		t.Fatalf("cache entry Failed = %v, want true after a failed real check", entry.Failed)
	}
	if !entry.CheckedAt.Equal(fixedNow) {
		t.Fatalf("cache entry CheckedAt = %v, want %v (model.now())", entry.CheckedAt, fixedNow)
	}
}

// TestCheckForUpdateCmdFreshSuccessCacheSkipsNetworkCall guards the core
// throttling behavior: a cache entry younger than updateCheckCacheTTLSuccess
// must skip the real check entirely (GetUpdateChannel never called) and
// return the cached latestVersion through the same updateCheckCompletedMsg
// shape existing code already consumes.
func TestCheckForUpdateCmdFreshSuccessCacheSkipsNetworkCall(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "update-check-cache.json")
	fixedNow := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	writeCacheFileForTest(t, cachePath, updateCheckCacheEntry{
		CheckedAt:     fixedNow.Add(-1 * time.Hour),
		LatestVersion: "v0.5.0",
	})

	service := &fakeQueryService{updateChannelErr: errors.New("must not be called")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"), WithUpdateCheckCachePath(cachePath))
	model.now = func() time.Time { return fixedNow }

	msg, ok := model.checkForUpdateCmd()().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 0 {
		t.Fatalf("GetUpdateChannel call count = %d, want 0 -- a fresh success cache entry must skip the real check", service.calls.updateChannel)
	}
	if msg.err != nil {
		t.Fatalf("updateCheckCompletedMsg.err = %v, want nil from a cache hit", msg.err)
	}
	if msg.latestVersion != "v0.5.0" {
		t.Fatalf("updateCheckCompletedMsg.latestVersion = %q, want the cached %q", msg.latestVersion, "v0.5.0")
	}
}

// TestCheckForUpdateCmdStaleSuccessCachePerformsFreshCheck guards the other
// side of the TTL: a cache entry older than updateCheckCacheTTLSuccess must
// not be reused -- checkForUpdateCmd performs a real check again.
func TestCheckForUpdateCmdStaleSuccessCachePerformsFreshCheck(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "update-check-cache.json")
	fixedNow := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	writeCacheFileForTest(t, cachePath, updateCheckCacheEntry{
		CheckedAt:     fixedNow.Add(-25 * time.Hour),
		LatestVersion: "v0.4.0",
	})

	service := &fakeQueryService{updateChannelErr: errors.New("metadata store unavailable")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"), WithUpdateCheckCachePath(cachePath))
	model.now = func() time.Time { return fixedNow }

	msg, ok := model.checkForUpdateCmd()().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 1 {
		t.Fatalf("GetUpdateChannel call count = %d, want exactly 1 -- a stale (>24h) success cache entry must trigger a fresh check", service.calls.updateChannel)
	}
	if msg.err == nil {
		t.Fatal("updateCheckCompletedMsg.err = nil, want the fresh check's error surfaced")
	}
}

// TestCheckForUpdateCmdFreshFailureCacheSkipsRetry guards the failure-TTL
// half of the fix for the confirmed incident: a cached failure younger than
// updateCheckCacheTTLFailure must not retry on the very next TUI open.
func TestCheckForUpdateCmdFreshFailureCacheSkipsRetry(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "update-check-cache.json")
	fixedNow := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	writeCacheFileForTest(t, cachePath, updateCheckCacheEntry{
		CheckedAt: fixedNow.Add(-30 * time.Minute),
		Failed:    true,
	})

	service := &fakeQueryService{updateChannelErr: errors.New("must not be called")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"), WithUpdateCheckCachePath(cachePath))
	model.now = func() time.Time { return fixedNow }

	msg, ok := model.checkForUpdateCmd()().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 0 {
		t.Fatalf("GetUpdateChannel call count = %d, want 0 -- a fresh (<1h) cached failure must skip retrying", service.calls.updateChannel)
	}
	if msg.err == nil {
		t.Fatal("updateCheckCompletedMsg.err = nil, want a non-nil err so Update's handler silently skips it exactly like a real failure")
	}
}

// TestCheckForUpdateCmdStaleFailureCacheRetries guards the other side of the
// failure TTL: once updateCheckCacheTTLFailure has elapsed, the check must
// retry rather than staying silent forever.
func TestCheckForUpdateCmdStaleFailureCacheRetries(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "update-check-cache.json")
	fixedNow := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	writeCacheFileForTest(t, cachePath, updateCheckCacheEntry{
		CheckedAt: fixedNow.Add(-2 * time.Hour),
		Failed:    true,
	})

	service := &fakeQueryService{updateChannelErr: errors.New("metadata store unavailable")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"), WithUpdateCheckCachePath(cachePath))
	model.now = func() time.Time { return fixedNow }

	msg, ok := model.checkForUpdateCmd()().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 1 {
		t.Fatalf("GetUpdateChannel call count = %d, want exactly 1 -- a stale (>1h) cached failure must retry", service.calls.updateChannel)
	}
}

// TestCheckForUpdateCmdCorruptCacheFallsBackToRealCheck guards the
// fail-open posture required of local optional state in this codebase
// (mirroring loadSetupManagedTUIConfig/readSetupManagedEnvFile's own
// graceful handling of a missing/malformed file): a corrupt cache file must
// never crash or block the check, only fall back to performing a real one.
func TestCheckForUpdateCmdCorruptCacheFallsBackToRealCheck(t *testing.T) {
	t.Parallel()

	cachePath := filepath.Join(t.TempDir(), "update-check-cache.json")
	if err := os.WriteFile(cachePath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("seed corrupt cache file: %v", err)
	}

	service := &fakeQueryService{updateChannelErr: errors.New("metadata store unavailable")}
	model := NewModel(service, WithCurrentVersion("v0.2.0"), WithUpdateCheckCachePath(cachePath))
	model.now = func() time.Time { return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC) }

	msg, ok := model.checkForUpdateCmd()().(updateCheckCompletedMsg)
	if !ok {
		t.Fatalf("cmd() = %#v, want an updateCheckCompletedMsg", msg)
	}
	if service.calls.updateChannel != 1 {
		t.Fatalf("GetUpdateChannel call count = %d, want exactly 1 -- a corrupt cache file must fall back to a real check", service.calls.updateChannel)
	}
	if msg.err == nil {
		t.Fatal("updateCheckCompletedMsg.err = nil, want the real check's error surfaced")
	}
}

func readCacheFileForTest(t *testing.T, path string) updateCheckCacheEntry {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache file %q: %v", path, err)
	}
	var entry updateCheckCacheEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatalf("unmarshal cache file %q: %v", path, err)
	}
	return entry
}

func writeCacheFileForTest(t *testing.T, path string, entry updateCheckCacheEntry) {
	t.Helper()
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal cache entry: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write cache file %q: %v", path, err)
	}
}

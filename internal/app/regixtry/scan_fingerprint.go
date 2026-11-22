package regixtry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	domain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

// scanSettingsHashVersion changes whenever the set of inputs hashed by
// scanSettingsHash changes, so an upgrade invalidates every stored hash.
const scanSettingsHashVersion = "v1"

// scanFingerprint identifies everything that can change a Trivy result for
// an unchanged image: the scanner version, the vulnerability database (its
// version and updated_at) and the effective scan configuration. Two runs
// with an equal fingerprint produce the same findings, so push and the
// sweep skip the rescan.
type scanFingerprint struct {
	TrivyVersion string
	DBVersion    int
	DBUpdatedAt  *time.Time
	SettingsHash string
	// Unknown is set when a component could not be determined (no DB
	// metadata yet, unreadable ignore file, unknown version). An unknown
	// fingerprint never matches anything: the decision is always "scan".
	Unknown bool
}

func (f scanFingerprint) complete() bool {
	return !f.Unknown && f.TrivyVersion != "" && f.DBUpdatedAt != nil && f.SettingsHash != ""
}

// fingerprintOfRun rebuilds the fingerprint a stored run was scanned with.
func fingerprintOfRun(run ports.ScanRun) scanFingerprint {
	return scanFingerprint{TrivyVersion: strings.TrimSpace(run.TrivyVersion), DBVersion: run.DBVersion, DBUpdatedAt: run.DBUpdatedAt, SettingsHash: run.SettingsHash}
}

func (f scanFingerprint) equal(other scanFingerprint) bool {
	if !f.complete() || !other.complete() {
		return false
	}
	return f.TrivyVersion == other.TrivyVersion && f.DBVersion == other.DBVersion && f.DBUpdatedAt.Equal(*other.DBUpdatedAt) && f.SettingsHash == other.SettingsHash
}

// needsScan is the single "does this digest need a scan?" decision shared
// by push, the scheduled sweep and the manual trigger. latest is the
// digest's latest COMPLETED run (nil when there is none). force (manual
// scans) always scans; otherwise a digest is skipped only when its latest
// completed result is not stale and carries exactly the current fingerprint.
func needsScan(latest *ports.ScanRun, current scanFingerprint, force bool) (bool, string) {
	switch {
	case force:
		return true, "forced"
	case latest == nil:
		return true, "no completed result"
	case latest.Stale:
		return true, "previous rescan failed"
	case current.Unknown || !current.complete():
		return true, "current fingerprint unknown"
	case !fingerprintOfRun(*latest).complete():
		return true, "stored fingerprint incomplete"
	case !fingerprintOfRun(*latest).equal(current):
		return true, "fingerprint changed"
	}
	return false, "fingerprint unchanged"
}

// scanSettingsHash hashes the effective scan configuration that reaches the
// scanner and can change its result: the ignore file and ignore policy paths
// and the sha256 of their content. ok is false when a configured file cannot
// be read (the fingerprint is then unknown and the scan runs, failing at its
// own pre-flight check).
func scanSettingsHash(settings ports.ScanSettings) (string, bool) {
	hash := sha256.New()
	fmt.Fprintf(hash, "scan-settings:%s\n", scanSettingsHashVersion)
	ok := true
	for _, input := range []struct{ label, path string }{{"ignore-file", settings.IgnoreFilePath}, {"ignore-policy", settings.IgnorePolicyPath}} {
		path := strings.TrimSpace(input.path)
		if path == "" {
			fmt.Fprintf(hash, "%s:-\n", input.label)
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			ok = false
			fmt.Fprintf(hash, "%s:%s:unreadable\n", input.label, path)
			continue
		}
		sum := sha256.Sum256(content)
		fmt.Fprintf(hash, "%s:%s:%s\n", input.label, path, hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(hash.Sum(nil)), ok
}

// currentScanFingerprint resolves the fingerprint a scan started now would
// produce, WITHOUT running a scan: the trivy version from the runtime state,
// the DB metadata from the runner (cache metadata) and the settings hash.
func (s *Service) currentScanFingerprint(ctx context.Context, settings ports.ScanSettings) scanFingerprint {
	hash, ok := scanSettingsHash(settings)
	fp := scanFingerprint{TrivyVersion: strings.TrimSpace(settings.TrivyVersion), SettingsHash: hash, Unknown: !ok}
	reader, isReader := s.getScanRunner().(ports.ScanDBMetadataReader)
	if !isReader {
		fp.Unknown = true
		return fp
	}
	meta, err := reader.ReadDBMetadata(ctx, settings)
	if err != nil || meta.UpdatedAt == nil {
		fp.Unknown = true
		return fp
	}
	fp.DBVersion = meta.Version
	fp.DBUpdatedAt = meta.UpdatedAt
	return fp
}

// scanDecision resolves the inputs of needsScan for one digest and, when the
// scan is skipped, records last_checked_at on the existing result (no new
// run). Any store error degrades to "scan": never skip on unknown.
func (s *Service) scanDecision(ctx context.Context, tenant string, repository string, digest string, settings ports.ScanSettings, force bool) bool {
	if force {
		scan, _ := needsScan(nil, scanFingerprint{}, true)
		return scan
	}
	var latest *ports.ScanRun
	run, err := s.metadata.GetLatestCompletedScanRunByDigest(ctx, tenant, repository, digest)
	switch {
	case err == nil:
		latest = &run
	case domain.IsCode(err, domain.ErrorCodeNotFound):
	default:
		return true
	}
	scan, _ := needsScan(latest, s.currentScanFingerprint(ctx, settings), false)
	if !scan && latest != nil {
		if err := s.metadata.TouchScanRunChecked(ctx, tenant, latest.ID, s.now()); err != nil {
			return true
		}
	}
	return scan
}

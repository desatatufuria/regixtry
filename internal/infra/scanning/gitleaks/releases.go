package gitleaks

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"regixtry/internal/infra/release"
)

// minimumGitleaksVersion is the lowest gitleaks release Regixtry will
// resolve, install, or activate.
//
// Verified against gitleaks' actual GitHub release history (not copied from
// design.md's unverified 8.24.0 estimate): archive scanning and its
// --max-archive-depth flag — the flag this package's runner relies on —
// shipped in v8.27.0 (released 2025-06-01, github.com/gitleaks/gitleaks
// PR #1872 "Archive support", merged into that release's changelog).
// v8.24.0 (released 2025-02-20) predates that PR and does not support
// --max-archive-depth.
const minimumGitleaksVersion = "8.27.0"

type releaseAsset struct {
	Version      string
	ArchiveName  string
	ArchiveURL   string
	ChecksumsURL string
}

type releaseClient interface {
	ResolveRelease(ctx context.Context, version string) (releaseAsset, error)
	DownloadReleaseAsset(ctx context.Context, url string) ([]byte, error)
	DownloadChecksums(ctx context.Context, url string) ([]byte, error)
}

type githubReleaseClient struct {
	baseAPI string
	client  *http.Client
}

func newGitHubReleaseClient() releaseClient {
	return githubReleaseClient{baseAPI: "https://api.github.com/repos/gitleaks/gitleaks/releases", client: &http.Client{}}
}

func (c githubReleaseClient) ResolveRelease(ctx context.Context, version string) (releaseAsset, error) {
	trimmedVersion := strings.TrimSpace(version)
	tag := ""
	if trimmedVersion != "" {
		tag = "v" + strings.TrimPrefix(trimmedVersion, "v")
	}
	query := release.AssetQuery{
		BaseAPI: c.baseAPI,
		Tag:     tag,
		MatchArchive: func(name string) bool {
			return strings.HasPrefix(name, "gitleaks_") && strings.HasSuffix(name, "_linux_x64.tar.gz")
		},
		MatchChecksums: func(name string) bool {
			return strings.HasSuffix(name, "checksums.txt")
		},
	}
	asset, err := release.ResolveAsset(ctx, c.client, query)
	if err != nil {
		return releaseAsset{}, fmt.Errorf("resolve gitleaks release: %w", err)
	}
	if err := requireMinimumVersion(asset.Version); err != nil {
		return releaseAsset{}, err
	}
	return releaseAsset{
		Version:      asset.Version,
		ArchiveName:  asset.ArchiveName,
		ArchiveURL:   asset.ArchiveURL,
		ChecksumsURL: asset.ChecksumsURL,
	}, nil
}

func (c githubReleaseClient) DownloadReleaseAsset(ctx context.Context, url string) ([]byte, error) {
	return c.download(ctx, url)
}

func (c githubReleaseClient) DownloadChecksums(ctx context.Context, url string) ([]byte, error) {
	return c.download(ctx, url)
}

func (c githubReleaseClient) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s returned %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// requireMinimumVersion rejects any version below minimumGitleaksVersion.
func requireMinimumVersion(version string) error {
	cmp, err := compareVersions(version, minimumGitleaksVersion)
	if err != nil {
		return err
	}
	if cmp < 0 {
		return fmt.Errorf("gitleaks version %s is below the minimum supported version %s", version, minimumGitleaksVersion)
	}
	return nil
}

// compareVersions compares two dotted numeric version strings
// (major.minor.patch, tolerant of a leading "v" and missing trailing
// segments). It returns -1 if a < b, 0 if equal, 1 if a > b.
func compareVersions(a string, b string) (int, error) {
	aParts, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bParts, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < len(aParts); i++ {
		if aParts[i] != bParts[i] {
			if aParts[i] < bParts[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

func parseVersion(version string) ([3]int, error) {
	var parts [3]int
	trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if trimmed == "" {
		return parts, fmt.Errorf("empty gitleaks version")
	}
	segments := strings.SplitN(trimmed, ".", 3)
	for i, segment := range segments {
		numeric := segment
		for idx, r := range numeric {
			if r < '0' || r > '9' {
				numeric = numeric[:idx]
				break
			}
		}
		if numeric == "" {
			return parts, fmt.Errorf("invalid gitleaks version segment %q in %q", segment, version)
		}
		value, err := strconv.Atoi(numeric)
		if err != nil {
			return parts, fmt.Errorf("invalid gitleaks version segment %q in %q: %w", segment, version, err)
		}
		parts[i] = value
	}
	return parts, nil
}

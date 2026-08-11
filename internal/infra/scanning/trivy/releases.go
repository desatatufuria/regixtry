package trivy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"regixtry/internal/infra/release"
)

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
	return githubReleaseClient{baseAPI: "https://api.github.com/repos/aquasecurity/trivy/releases", client: &http.Client{}}
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
			return strings.HasPrefix(name, "trivy_") && strings.HasSuffix(name, "_Linux-64bit.tar.gz")
		},
		MatchChecksums: func(name string) bool {
			return strings.HasSuffix(name, "checksums.txt")
		},
	}
	asset, err := release.ResolveAsset(ctx, c.client, query)
	if err != nil {
		return releaseAsset{}, fmt.Errorf("resolve trivy release: %w", err)
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

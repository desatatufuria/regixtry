package trivy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var ErrTrivyBinaryNotFound = errors.New("trivy binary not found in archive")

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
	endpoint := strings.TrimRight(c.baseAPI, "/") + "/latest"
	trimmedVersion := strings.TrimSpace(version)
	if trimmedVersion != "" {
		endpoint = strings.TrimRight(c.baseAPI, "/") + "/tags/v" + strings.TrimPrefix(trimmedVersion, "v")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return releaseAsset{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return releaseAsset{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return releaseAsset{}, fmt.Errorf("resolve trivy release: %s returned %d", endpoint, resp.StatusCode)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return releaseAsset{}, err
	}
	asset := releaseAsset{Version: strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")}
	for _, candidate := range payload.Assets {
		name := strings.TrimSpace(candidate.Name)
		switch {
		case strings.Contains(name, "Linux-64bit.tar.gz") && strings.HasPrefix(name, "trivy_"):
			asset.ArchiveName = name
			asset.ArchiveURL = strings.TrimSpace(candidate.BrowserDownloadURL)
		case strings.HasSuffix(name, "checksums.txt"):
			asset.ChecksumsURL = strings.TrimSpace(candidate.BrowserDownloadURL)
		}
	}
	if asset.Version == "" || asset.ArchiveName == "" || asset.ArchiveURL == "" || asset.ChecksumsURL == "" {
		return releaseAsset{}, fmt.Errorf("resolve trivy release: missing Linux 64bit archive or checksums")
	}
	return asset, nil
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

func verifyArchiveChecksum(archivePath string, archiveName string, checksumsBody []byte) error {
	body, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	want := strings.ToLower(hex.EncodeToString(sum[:]))
	for _, line := range strings.Split(string(checksumsBody), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != archiveName {
			continue
		}
		if strings.ToLower(strings.TrimSpace(fields[0])) != want {
			return fmt.Errorf("checksum mismatch for %s", archiveName)
		}
		return nil
	}
	return fmt.Errorf("checksum not found for %s", archiveName)
}

func extractTrivyBinary(ctx context.Context, archivePath string, destinationDir string) (string, error) {
	archiveFile, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer archiveFile.Close()
	gzr, err := gzip.NewReader(archiveFile)
	if err != nil {
		return "", err
	}
	defer gzr.Close()
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return "", err
	}
	tr := tar.NewReader(gzr)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		header, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrTrivyBinaryNotFound
			}
			return "", err
		}
		if filepath.Base(strings.TrimSpace(header.Name)) != "trivy" || header.Typeflag != tar.TypeReg {
			continue
		}
		binaryPath := filepath.Join(destinationDir, "trivy")
		file, err := os.OpenFile(binaryPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(file, tr); err != nil {
			_ = file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return binaryPath, nil
	}
}

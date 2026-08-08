package releases

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
	"runtime"
	"strings"
)

const defaultReleasesAPIURL = "https://api.github.com/repos/desatatufuria/workspace/releases"

type ReleaseAsset struct {
	Tag          string
	Version      string
	ArchiveURL   string
	ChecksumsURL string
	ArchiveName  string
}

type DownloadProgress struct {
	Stage  string
	Detail string
}

type GitHubClient struct {
	baseURL string
	client  *http.Client
}

func NewGitHubClient(baseURL string) *GitHubClient {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		trimmed = defaultReleasesAPIURL
	}
	return &GitHubClient{baseURL: trimmed, client: http.DefaultClient}
}

func NormalizeArch(goarch string) (string, error) {
	switch strings.TrimSpace(goarch) {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("unsupported Linux architecture: %s", goarch)
	}
}

func CurrentLinuxArch() (string, error) {
	return NormalizeArch(runtime.GOARCH)
}

func (c *GitHubClient) Resolve(ctx context.Context, ref string, targetOS string, targetArch string) (ReleaseAsset, error) {
	endpoint := c.baseURL + "/latest"
	trimmedRef := strings.TrimSpace(ref)
	if trimmedRef != "" {
		endpoint = c.baseURL + "/tags/" + trimmedRef
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ReleaseAsset{}, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return ReleaseAsset{}, fmt.Errorf("resolve release metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ReleaseAsset{}, fmt.Errorf("resolve release metadata: unexpected status %d", response.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ReleaseAsset{}, fmt.Errorf("decode release metadata: %w", err)
	}
	if strings.TrimSpace(payload.TagName) == "" {
		return ReleaseAsset{}, errors.New("release metadata did not contain a tag name")
	}

	archiveSuffix := fmt.Sprintf("_%s_%s.tar.gz", strings.TrimSpace(targetOS), strings.TrimSpace(targetArch))
	archiveURL, archiveName, err := selectAsset(payload.Assets, archiveSuffix, "release asset")
	if err != nil {
		return ReleaseAsset{}, err
	}
	checksumsURL, _, err := selectAsset(payload.Assets, "_checksums.txt", "checksum asset")
	if err != nil {
		return ReleaseAsset{}, err
	}

	return ReleaseAsset{
		Tag:          payload.TagName,
		Version:      strings.TrimPrefix(payload.TagName, "v"),
		ArchiveURL:   archiveURL,
		ChecksumsURL: checksumsURL,
		ArchiveName:  archiveName,
	}, nil
}

func selectAsset(assets []struct {
	BrowserDownloadURL string "json:\"browser_download_url\""
}, suffix string, label string) (string, string, error) {
	matches := make([]string, 0, 1)
	for _, asset := range assets {
		candidate := strings.TrimSpace(asset.BrowserDownloadURL)
		if candidate == "" {
			continue
		}
		name := filepath.Base(candidate)
		if strings.HasSuffix(name, suffix) {
			matches = append(matches, candidate)
		}
	}
	if len(matches) == 0 {
		return "", "", fmt.Errorf("%s was not found in the release metadata", label)
	}
	if len(matches) > 1 {
		return "", "", fmt.Errorf("multiple %s files matched the release metadata", label)
	}
	return matches[0], filepath.Base(matches[0]), nil
}

func (c *GitHubClient) DownloadVerifiedBinary(ctx context.Context, asset ReleaseAsset, dir string, progress func(DownloadProgress)) (string, error) {
	archivePath := filepath.Join(dir, asset.ArchiveName)
	checksumsPath := filepath.Join(dir, filepath.Base(asset.ChecksumsURL))
	reportDownloadProgress(progress, "download", fmt.Sprintf("Downloading %s", asset.ArchiveName))
	if err := c.downloadFile(ctx, asset.ArchiveURL, archivePath); err != nil {
		return "", err
	}
	if err := c.downloadFile(ctx, asset.ChecksumsURL, checksumsPath); err != nil {
		return "", err
	}
	reportDownloadProgress(progress, "verify", fmt.Sprintf("Verifying %s", asset.ArchiveName))
	if err := verifyChecksum(archivePath, asset.ArchiveName, checksumsPath); err != nil {
		return "", err
	}
	if err := validateArchive(archivePath); err != nil {
		return "", err
	}
	stagedPath := filepath.Join(dir, "regixtry")
	if err := extractRegixtryBinary(archivePath, stagedPath); err != nil {
		return "", err
	}
	return stagedPath, nil
}

func reportDownloadProgress(progress func(DownloadProgress), stage string, detail string) {
	if progress == nil {
		return
	}
	progress(DownloadProgress{Stage: stage, Detail: detail})
}

func (c *GitHubClient) downloadFile(ctx context.Context, sourceURL string, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s: %w", sourceURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download %s: unexpected status %d", sourceURL, response.StatusCode)
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(file, response.Body); err != nil {
		return err
	}
	return nil
}

func verifyChecksum(archivePath string, archiveName string, checksumsPath string) error {
	body, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("read checksum asset: %w", err)
	}
	checksum := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[1]) == archiveName {
			checksum = strings.TrimSpace(parts[0])
			break
		}
	}
	if checksum == "" {
		return fmt.Errorf("checksum asset does not contain an entry for %s", archiveName)
	}
	if len(checksum) != 64 {
		return fmt.Errorf("checksum entry for %s is malformed", archiveName)
	}
	body, err = os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("read archive for checksum verification: %w", err)
	}
	actual := sha256.Sum256(body)
	if !strings.EqualFold(checksum, hex.EncodeToString(actual[:])) {
		return fmt.Errorf("checksum verification failed for %s", archiveName)
	}
	return nil
}

func validateArchive(archivePath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()
	tarReader := tar.NewReader(gzReader)
	entries := make([]string, 0, 2)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		entries = append(entries, header.Name)
	}
	if len(entries) != 1 || entries[0] != "regixtry" {
		return errors.New("archive must contain exactly one regixtry entry named regixtry")
	}
	return nil
}

func extractRegixtryBinary(archivePath string, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "regixtry" || header.Typeflag != tar.TypeReg {
			continue
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(output, tarReader); err != nil {
			_ = output.Close()
			return err
		}
		if err := output.Close(); err != nil {
			return err
		}
		return os.Chmod(destination, 0o755)
	}
	return errors.New("archive must contain exactly one regixtry entry named regixtry")
}

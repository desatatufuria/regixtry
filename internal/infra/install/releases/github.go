package releases

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"regixtry/internal/infra/release"
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
	Stage      string
	Detail     string
	BytesRead  int64
	TotalBytes int64
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
	archiveSuffix := fmt.Sprintf("_%s_%s.tar.gz", strings.TrimSpace(targetOS), strings.TrimSpace(targetArch))
	query := release.AssetQuery{
		BaseAPI: c.baseURL,
		Tag:     strings.TrimSpace(ref),
		MatchArchive: func(name string) bool {
			return strings.HasSuffix(name, archiveSuffix)
		},
		MatchChecksums: func(name string) bool {
			return strings.HasSuffix(name, "_checksums.txt")
		},
	}

	asset, err := release.ResolveAsset(ctx, c.client, query)
	if err != nil {
		return ReleaseAsset{}, err
	}
	return ReleaseAsset{
		Tag:          asset.Tag,
		Version:      asset.Version,
		ArchiveURL:   asset.ArchiveURL,
		ChecksumsURL: asset.ChecksumsURL,
		ArchiveName:  asset.ArchiveName,
	}, nil
}

func (c *GitHubClient) DownloadVerifiedBinary(ctx context.Context, asset ReleaseAsset, dir string, progress func(DownloadProgress)) (string, error) {
	archivePath := filepath.Join(dir, asset.ArchiveName)
	checksumsPath := filepath.Join(dir, filepath.Base(asset.ChecksumsURL))
	reportDownloadProgress(progress, "download", fmt.Sprintf("Downloading %s", asset.ArchiveName))
	archiveByteProgress := func(bytesRead int64, totalBytes int64) {
		reportDownloadByteProgress(progress, bytesRead, totalBytes)
	}
	if err := c.downloadFile(ctx, asset.ArchiveURL, archivePath, archiveByteProgress); err != nil {
		return "", err
	}
	// The checksums asset is a few hundred bytes; it only needs the discrete
	// stage event already sent above, not granular byte reporting.
	if err := c.downloadFile(ctx, asset.ChecksumsURL, checksumsPath, nil); err != nil {
		return "", err
	}
	reportDownloadProgress(progress, "verify", fmt.Sprintf("Verifying %s", asset.ArchiveName))
	checksumsBody, err := os.ReadFile(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("read checksum asset: %w", err)
	}
	if err := release.VerifyChecksum(archivePath, asset.ArchiveName, checksumsBody); err != nil {
		return "", fmt.Errorf("checksum verification failed for %s: %w", asset.ArchiveName, err)
	}
	if err := validateArchive(archivePath); err != nil {
		return "", err
	}
	stagedPath, err := release.ExtractBinary(ctx, archivePath, dir, "regixtry")
	if err != nil {
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

func reportDownloadByteProgress(progress func(DownloadProgress), bytesRead int64, totalBytes int64) {
	if progress == nil {
		return
	}
	progress(DownloadProgress{Stage: "download", BytesRead: bytesRead, TotalBytes: totalBytes})
}

func (c *GitHubClient) downloadFile(ctx context.Context, sourceURL string, destination string, onByteProgress func(bytesRead int64, totalBytes int64)) error {
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
	var body io.Reader = response.Body
	if onByteProgress != nil {
		// response.ContentLength is -1 when the server did not send a
		// Content-Length header; that sentinel is forwarded as-is (never
		// coerced to a fake value) so the rendering layer can decide how to
		// present an unknown total.
		body = &countingReader{reader: response.Body, total: response.ContentLength, onProgress: onByteProgress}
	}
	if _, err := io.Copy(file, body); err != nil {
		return err
	}
	return nil
}

// countingReader wraps an io.Reader and reports cumulative bytes read after
// every Read() call, forwarding the total byte count observed on the HTTP
// response so callers can render byte-level progress.
type countingReader struct {
	reader     io.Reader
	total      int64
	read       int64
	onProgress func(bytesRead int64, totalBytes int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	if n > 0 {
		c.read += int64(n)
		c.onProgress(c.read, c.total)
	}
	return n, err
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

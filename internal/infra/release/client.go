// Package release provides shared GitHub-release resolution, checksum
// verification, and binary-archive extraction primitives used by every
// Regixtry-managed feature runtime (Trivy, self-update, and future
// features such as Gitleaks). Feature-specific asset naming and binary
// naming are supplied as parameters; orchestration concerns that differ
// per call site (process replacement, systemd backups, self-update's
// stricter single-entry archive validation) stay in their own packages.
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// Asset describes a resolved GitHub release's archive and checksums files.
type Asset struct {
	Tag          string
	Version      string
	ArchiveName  string
	ArchiveURL   string
	ChecksumsURL string
}

// AssetQuery parameterizes a release resolution: which release feed
// (BaseAPI, e.g. "https://api.github.com/repos/<owner>/<repo>/releases"),
// which tag (empty selects "/latest"), and how to recognize a feature's
// own archive and checksums assets among a release's files.
type AssetQuery struct {
	BaseAPI        string
	Tag            string
	MatchArchive   func(assetName string) bool
	MatchChecksums func(assetName string) bool
	// CheckURL, when set, vets the download URL of the selected archive and
	// checksums assets before they are returned.
	CheckURL func(rawURL string) error
}

// ResolveAsset queries a GitHub releases API endpoint and selects the
// archive and checksums assets using the query's matchers. It fails if
// either matcher does not select an asset.
func ResolveAsset(ctx context.Context, httpClient *http.Client, query AssetQuery) (Asset, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	baseAPI := strings.TrimRight(strings.TrimSpace(query.BaseAPI), "/")
	endpoint := baseAPI + "/latest"
	tag := strings.TrimSpace(query.Tag)
	if tag != "" {
		if err := validateTag(tag); err != nil {
			return Asset{}, err
		}
		endpoint = baseAPI + "/tags/" + url.PathEscape(tag)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Asset{}, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Asset{}, fmt.Errorf("resolve release metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Asset{}, fmt.Errorf("resolve release metadata: %s returned %d", endpoint, resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	metadata, err := ReadLimited(resp.Body, MaxMetadataBytes)
	if err != nil {
		return Asset{}, fmt.Errorf("read release metadata: %w", err)
	}
	if err := json.Unmarshal(metadata, &payload); err != nil {
		return Asset{}, fmt.Errorf("decode release metadata: %w", err)
	}
	tagName := strings.TrimSpace(payload.TagName)
	if tagName == "" {
		return Asset{}, fmt.Errorf("release metadata did not contain a tag name")
	}

	if err := validateTag(tagName); err != nil {
		return Asset{}, fmt.Errorf("release metadata tag name: %w", err)
	}

	asset := Asset{Tag: tagName, Version: strings.TrimPrefix(tagName, "v")}
	for _, candidate := range payload.Assets {
		url := strings.TrimSpace(candidate.BrowserDownloadURL)
		if url == "" {
			continue
		}
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			name = filepath.Base(url)
		}
		if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			continue
		}
		if query.MatchArchive != nil && query.MatchArchive(name) {
			asset.ArchiveName = name
			asset.ArchiveURL = url
		}
		if query.MatchChecksums != nil && query.MatchChecksums(name) {
			asset.ChecksumsURL = url
		}
	}
	if query.CheckURL != nil {
		for _, u := range []string{asset.ArchiveURL, asset.ChecksumsURL} {
			if u == "" {
				continue
			}
			if err := query.CheckURL(u); err != nil {
				return Asset{}, err
			}
		}
	}
	if asset.ArchiveName == "" || asset.ArchiveURL == "" || asset.ChecksumsURL == "" {
		return Asset{}, fmt.Errorf("release asset not found: no matching archive or checksums asset in %s", endpoint)
	}
	return asset, nil
}

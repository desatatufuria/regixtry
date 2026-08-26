package release

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Channel selects which released tags a version check considers.
type Channel string

const (
	// ChannelStable only ever considers a tag with no "-rcN" suffix.
	ChannelStable Channel = "stable"
	// ChannelInsider considers every tag, RC or not.
	ChannelInsider Channel = "insider"
)

// ValidChannel reports whether value is exactly one of the two channel
// identifiers -- never coerced, never case-insensitive.
func ValidChannel(value string) bool {
	switch Channel(value) {
	case ChannelStable, ChannelInsider:
		return true
	default:
		return false
	}
}

// releasesListPageSize is generous relative to this repo's actual release
// count; a single page keeps this a plain GET with no pagination logic to
// maintain for what is a best-effort, non-blocking background check, not a
// completeness-critical listing.
const releasesListPageSize = 100

// LatestForChannel lists releases from baseAPI (the plain releases
// collection endpoint, e.g. "https://api.github.com/repos/<owner>/<repo>/
// releases" -- no "/latest" or "/tags/..." suffix) and returns the tag with
// the highest version among the channel's eligible tags, using this
// package's own real version ordering (compareVersions) rather than API
// list order (which is creation-date descending, not version order) or
// lexicographic string order (wrong once an rc number crosses a
// digit-count boundary). A tag that does not parse as this repo's own
// version shape is skipped, not treated as a fatal error -- a foreign or
// malformed tag must never abort the whole scan.
func LatestForChannel(ctx context.Context, httpClient *http.Client, baseAPI string, channel Channel) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	endpoint := fmt.Sprintf("%s?per_page=%d", strings.TrimRight(strings.TrimSpace(baseAPI), "/"), releasesListPageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("release: list releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("release: list releases: %s returned %d", endpoint, resp.StatusCode)
	}

	var payload []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("release: decode releases list: %w", err)
	}

	var (
		bestTag    string
		bestParsed parsedVersion
		found      bool
	)
	for _, entry := range payload {
		tag := strings.TrimSpace(entry.TagName)
		if tag == "" {
			continue
		}
		parsed, err := parseVersion(tag)
		if err != nil {
			continue // a foreign/malformed tag; skip, don't abort the scan
		}
		if channel == ChannelStable && parsed.hasRC {
			continue
		}
		if !found || compareVersions(parsed, bestParsed) > 0 {
			bestTag, bestParsed, found = tag, parsed, true
		}
	}
	if !found {
		return "", fmt.Errorf("release: no eligible release found for channel %q", channel)
	}
	return bestTag, nil
}

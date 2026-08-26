package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"regixtry/internal/ports"
)

const defaultAdminClientTimeout = 15 * time.Second

type AdminClient interface {
	Login(ctx context.Context, username, password string) (AdminSession, error)
	ListFeatures(ctx context.Context, session AdminSession) ([]ports.FeatureSummary, error)
	GetFeaturePage(ctx context.Context, session AdminSession, name string) (ports.FeaturePage, error)
	ListScanRuns(ctx context.Context, session AdminSession, repository string, limit int) ([]ports.ScanRun, error)
	// ListRepositoryScanSummaries backs the Repository Alerts table's
	// crowd-out fix: one row per repository (with its total run count),
	// collapsed server-side before limit -- unlike ListScanRuns, whose
	// limit applies to raw scan runs.
	ListRepositoryScanSummaries(ctx context.Context, session AdminSession, limit int) ([]ports.RepositoryScanSummary, error)
	GetScanRunDetail(ctx context.Context, session AdminSession, runID string) (ports.ScanRunDetail, error)
	GetSecretScanFindings(ctx context.Context, session AdminSession, repository string, digest string) (ports.SecretScanRunDetail, error)
	ExecuteFeatureAction(ctx context.Context, session AdminSession, name string, actionID string) (ports.FeatureActionResult, error)
	GetFeature(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error)
	GetFeatureStatus(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error)
	InstallFeatureRuntime(ctx context.Context, session AdminSession, name string, version string) (ports.FeatureRuntimeState, error)
	UpgradeFeatureRuntime(ctx context.Context, session AdminSession, name string, version string) (ports.FeatureRuntimeState, error)
	RollbackFeatureRuntime(ctx context.Context, session AdminSession, name string) (ports.FeatureRuntimeState, error)
	ConfigureFeature(ctx context.Context, session AdminSession, name string, input ports.FeatureConfigureInput) (ports.FeatureDetails, error)
	GetScanPolicy(ctx context.Context, session AdminSession) (ports.ScanPolicySettings, error)
	UpdateScanPolicy(ctx context.Context, session AdminSession, input ports.ScanPolicySettings) (ports.ScanPolicySettings, error)
	// GetUpdateChannel/SetUpdateChannel back the Operations domain's Update
	// Channel screen (tui-update-check feature). GetUpdateChannel hits
	// GET /update-channel -- a deliberately unauthenticated top-level route,
	// not under /admin/v1 (see router.go's registration comment) -- reused
	// as-is here rather than duplicated as an authenticated read: the
	// session's bearer token is harmlessly attached by the shared request
	// helper, the server simply never requires it for this route.
	// SetUpdateChannel is the only write path, PUT /admin/v1/update-channel,
	// admin-gated like every other admin/v1 route.
	GetUpdateChannel(ctx context.Context, session AdminSession) (string, error)
	SetUpdateChannel(ctx context.Context, session AdminSession, channel string) (string, error)
	GetSigningPolicy(ctx context.Context, session AdminSession) (ports.SigningPolicySettings, error)
	UpdateSigningPolicy(ctx context.Context, session AdminSession, input ports.SigningPolicySettings) (ports.SigningPolicySettings, error)
	// CountSigningKeyUsage is the read-only, best-effort advisory backing
	// trustedKeyList's delete-key confirm: how many of a scope's
	// currently-tagged digests verify against keyPEM. repository == ""
	// means every repository. Never a blocking gate -- see
	// CountManifestsSignedByKey's own doc comment
	// (internal/app/regixtry/service_signing_key_usage.go).
	CountSigningKeyUsage(ctx context.Context, session AdminSession, repository string, keyPEM string) (count int, capped bool, err error)
	EnableFeature(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error)
	DisableFeature(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error)
	ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error)
	CreateUser(ctx context.Context, session AdminSession, input ports.AdminCreateUserInput) (ports.AdminUser, error)
	ResetPassword(ctx context.Context, session AdminSession, input ports.AdminResetPasswordInput) error
	ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error)
	PutUserGrant(ctx context.Context, session AdminSession, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error)
	DeleteUserGrant(ctx context.Context, session AdminSession, userID string, repository string) error
	// ListRepositoryGrants/PutRepositoryGrant/DeleteRepositoryGrant back the
	// repo-admin delegate's own grants view (design.md Decision 7), calling
	// /admin/v1/repositories/{repo}/grants[/{username}] rather than the
	// user-centric /admin/v1/users/{id}/grants routes above.
	ListRepositoryGrants(ctx context.Context, session AdminSession, repository string) ([]ports.AdminRepositoryGrant, error)
	PutRepositoryGrant(ctx context.Context, session AdminSession, input ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error)
	DeleteRepositoryGrant(ctx context.Context, session AdminSession, repository string, username string) error
	ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error)
	CreateUserAdminToken(ctx context.Context, session AdminSession, input ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error)
	RevokeUserAdminToken(ctx context.Context, session AdminSession, userID string, accessor string) error
	EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
	DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
	// ListRobots/CreateRobot back screenAdminRobots/screenAdminCreateRobot
	// (design.md Decision 6): two new routes. Enable/disable/delete and
	// token issue/list/revoke deliberately reuse EnableUser/DisableUser/
	// CreateUserAdminToken etc. above with the robot's user ID -- no new
	// client methods for those.
	ListRobots(ctx context.Context, session AdminSession) ([]ports.AdminRobot, error)
	CreateRobot(ctx context.Context, session AdminSession, input ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error)
	// GetRepositoryOverride returns the stored override for one
	// (repository, feature) pair. A 404 response (no override row) is a
	// valid state, not an error: it returns (zero value, false, nil).
	GetRepositoryOverride(ctx context.Context, session AdminSession, repository string, feature string) (ports.RepositoryOverrideDetails, bool, error)
	// ListRepositoryOverrides returns every stored override row for one
	// feature, used only to annotate the Repository Alerts table.
	ListRepositoryOverrides(ctx context.Context, session AdminSession, feature string) ([]ports.RepositoryOverrideDetails, error)
	SetRepositoryOverride(ctx context.Context, session AdminSession, repository string, feature string, input ports.RepositoryOverrideDetails) (ports.RepositoryOverrideDetails, error)
	ClearRepositoryOverride(ctx context.Context, session AdminSession, repository string, feature string) error
	// DeleteRobot backs the robot-accounts hard-delete route (registry-acl-v1
	// follow-up). Added on PR 4 (backend-only) as part of the API contract;
	// the TUI key/UI wiring lands on PR 5.
	DeleteRobot(ctx context.Context, session AdminSession, userID string) error
}

type HTTPAdminClient struct {
	baseURL string
	client  *stdhttp.Client
	now     func() time.Time
}

func NewHTTPAdminClient(baseURL string, client *stdhttp.Client) (*HTTPAdminClient, error) {
	normalized, err := normalizeAdminAPIBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &stdhttp.Client{Timeout: defaultAdminClientTimeout}
	}

	return &HTTPAdminClient{
		baseURL: normalized,
		client:  client,
		now:     func() time.Time { return time.Now().UTC() },
	}, nil
}

func normalizeAdminAPIBaseURL(baseURL string) (string, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return "", errors.New("admin API base URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse admin API base URL: %w", err)
	}
	if !parsed.IsAbs() || strings.TrimSpace(parsed.Host) == "" {
		return "", errors.New("admin API base URL must be an absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("admin API base URL must use http or https")
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *HTTPAdminClient) Login(ctx context.Context, username, password string) (AdminSession, error) {
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, c.endpoint("/auth/token"), nil)
	if err != nil {
		return AdminSession{}, err
	}
	req.SetBasicAuth(strings.TrimSpace(username), password)

	resp, err := c.client.Do(req)
	if err != nil {
		return AdminSession{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != stdhttp.StatusOK {
		return AdminSession{}, decodeAdminAPIError("login", resp)
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		Token       string `json:"token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return AdminSession{}, err
	}

	bearerToken := strings.TrimSpace(payload.AccessToken)
	if bearerToken == "" {
		bearerToken = strings.TrimSpace(payload.Token)
	}
	if bearerToken == "" {
		return AdminSession{}, errors.New("admin API login response did not include an access token")
	}

	session := AdminSession{
		Username:    strings.TrimSpace(username),
		BearerToken: bearerToken,
	}
	if payload.ExpiresIn > 0 {
		session.ExpiresAt = c.now().Add(time.Duration(payload.ExpiresIn) * time.Second).UTC()
	}

	return session, nil
}

func (c *HTTPAdminClient) ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error) {
	var users []ports.AdminUser
	if err := c.getJSON(ctx, session, "/admin/v1/users", &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *HTTPAdminClient) ListFeatures(ctx context.Context, session AdminSession) ([]ports.FeatureSummary, error) {
	var features []ports.FeatureSummary
	if err := c.getJSON(ctx, session, "/admin/v1/features", &features); err != nil {
		return nil, err
	}
	return features, nil
}

func (c *HTTPAdminClient) GetFeature(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error) {
	return c.getFeatureDetails(ctx, session, "/admin/v1/features/"+url.PathEscape(strings.TrimSpace(name)))
}

func (c *HTTPAdminClient) GetFeaturePage(ctx context.Context, session AdminSession, name string) (ports.FeaturePage, error) {
	var page ports.FeaturePage
	if err := c.getJSON(ctx, session, "/admin/v1/features/"+url.PathEscape(strings.TrimSpace(name)), &page); err != nil {
		return ports.FeaturePage{}, err
	}
	return page, nil
}

func (c *HTTPAdminClient) ListScanRuns(ctx context.Context, session AdminSession, repository string, limit int) ([]ports.ScanRun, error) {
	path := "/admin/v1/scan-runs"
	query := url.Values{}
	if trimmed := strings.TrimSpace(repository); trimmed != "" {
		query.Set("repository", trimmed)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var runs []ports.ScanRun
	if err := c.getJSON(ctx, session, path, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func (c *HTTPAdminClient) ListRepositoryScanSummaries(ctx context.Context, session AdminSession, limit int) ([]ports.RepositoryScanSummary, error) {
	path := "/admin/v1/repository-scan-summaries"
	if limit > 0 {
		path += "?" + (url.Values{"limit": []string{strconv.Itoa(limit)}}).Encode()
	}
	var summaries []ports.RepositoryScanSummary
	if err := c.getJSON(ctx, session, path, &summaries); err != nil {
		return nil, err
	}
	return summaries, nil
}

func (c *HTTPAdminClient) GetScanRunDetail(ctx context.Context, session AdminSession, runID string) (ports.ScanRunDetail, error) {
	var detail ports.ScanRunDetail
	if err := c.getJSON(ctx, session, "/admin/v1/scan-runs/"+url.PathEscape(strings.TrimSpace(runID)), &detail); err != nil {
		return ports.ScanRunDetail{}, err
	}
	return detail, nil
}

// GetSecretScanFindings fetches the redacted secret-scan findings for one
// image (repository@digest), the same attribution key the Trivy scan-run
// detail is keyed by since both legs share the same rescan trigger. A
// caller with no persisted secret scan for that image gets the ordinary
// admin API error (surfaced as a clean empty state, not a crash).
func (c *HTTPAdminClient) GetSecretScanFindings(ctx context.Context, session AdminSession, repository string, digest string) (ports.SecretScanRunDetail, error) {
	query := url.Values{}
	query.Set("repository", strings.TrimSpace(repository))
	query.Set("digest", strings.TrimSpace(digest))
	var detail ports.SecretScanRunDetail
	if err := c.getJSON(ctx, session, "/admin/v1/secret-scan-findings?"+query.Encode(), &detail); err != nil {
		return ports.SecretScanRunDetail{}, err
	}
	return detail, nil
}

func (c *HTTPAdminClient) ExecuteFeatureAction(ctx context.Context, session AdminSession, name string, actionID string) (ports.FeatureActionResult, error) {
	var result ports.FeatureActionResult
	path := "/admin/v1/features/" + url.PathEscape(strings.TrimSpace(name)) + "/actions/" + url.PathEscape(strings.TrimSpace(actionID))
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, nil, &result, stdhttp.StatusOK); err != nil {
		return ports.FeatureActionResult{}, err
	}
	return result, nil
}

func (c *HTTPAdminClient) GetFeatureStatus(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error) {
	return c.getFeatureDetails(ctx, session, "/admin/v1/features/"+url.PathEscape(strings.TrimSpace(name))+"/status")
}

func (c *HTTPAdminClient) InstallFeatureRuntime(ctx context.Context, session AdminSession, name string, version string) (ports.FeatureRuntimeState, error) {
	return c.mutateFeatureRuntime(ctx, session, name, ":install", version)
}

func (c *HTTPAdminClient) UpgradeFeatureRuntime(ctx context.Context, session AdminSession, name string, version string) (ports.FeatureRuntimeState, error) {
	return c.mutateFeatureRuntime(ctx, session, name, ":upgrade", version)
}

func (c *HTTPAdminClient) RollbackFeatureRuntime(ctx context.Context, session AdminSession, name string) (ports.FeatureRuntimeState, error) {
	return c.mutateFeatureRuntime(ctx, session, name, ":rollback", "")
}

func (c *HTTPAdminClient) ConfigureFeature(ctx context.Context, session AdminSession, name string, input ports.FeatureConfigureInput) (ports.FeatureDetails, error) {
	var details ports.FeatureDetails
	body := encodeFeatureConfigureInput(input)
	path := "/admin/v1/features/" + url.PathEscape(strings.TrimSpace(name)) + "/config"
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, path, body, &details, stdhttp.StatusOK); err != nil {
		return ports.FeatureDetails{}, err
	}
	return details, nil
}

func (c *HTTPAdminClient) GetScanPolicy(ctx context.Context, session AdminSession) (ports.ScanPolicySettings, error) {
	var settings ports.ScanPolicySettings
	if err := c.getJSON(ctx, session, "/admin/v1/scan-policy", &settings); err != nil {
		return ports.ScanPolicySettings{}, err
	}
	return settings, nil
}

func (c *HTTPAdminClient) UpdateScanPolicy(ctx context.Context, session AdminSession, input ports.ScanPolicySettings) (ports.ScanPolicySettings, error) {
	var settings ports.ScanPolicySettings
	body := map[string]any{"enabled": input.Enabled, "severity_threshold": input.SeverityThreshold}
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, "/admin/v1/scan-policy", body, &settings, stdhttp.StatusOK); err != nil {
		return ports.ScanPolicySettings{}, err
	}
	return settings, nil
}

func (c *HTTPAdminClient) GetUpdateChannel(ctx context.Context, session AdminSession) (string, error) {
	var payload struct {
		Channel string `json:"channel"`
	}
	if err := c.getJSON(ctx, session, "/update-channel", &payload); err != nil {
		return "", err
	}
	return payload.Channel, nil
}

func (c *HTTPAdminClient) SetUpdateChannel(ctx context.Context, session AdminSession, channel string) (string, error) {
	var payload struct {
		Channel string `json:"channel"`
	}
	body := map[string]any{"channel": channel}
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, "/admin/v1/update-channel", body, &payload, stdhttp.StatusOK); err != nil {
		return "", err
	}
	return payload.Channel, nil
}

func (c *HTTPAdminClient) GetSigningPolicy(ctx context.Context, session AdminSession) (ports.SigningPolicySettings, error) {
	var settings ports.SigningPolicySettings
	if err := c.getJSON(ctx, session, "/admin/v1/signing-policy", &settings); err != nil {
		return ports.SigningPolicySettings{}, err
	}
	return settings, nil
}

func (c *HTTPAdminClient) UpdateSigningPolicy(ctx context.Context, session AdminSession, input ports.SigningPolicySettings) (ports.SigningPolicySettings, error) {
	var settings ports.SigningPolicySettings
	body := map[string]any{"enabled": input.Enabled, "trusted_public_keys": input.TrustedPublicKeys}
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, "/admin/v1/signing-policy", body, &settings, stdhttp.StatusOK); err != nil {
		return ports.SigningPolicySettings{}, err
	}
	return settings, nil
}

func (c *HTTPAdminClient) CountSigningKeyUsage(ctx context.Context, session AdminSession, repository string, keyPEM string) (int, bool, error) {
	path := "/admin/v1/signing-policy/key-usage"
	query := url.Values{}
	query.Set("key", keyPEM)
	if trimmed := strings.TrimSpace(repository); trimmed != "" {
		query.Set("repository", trimmed)
	}
	path += "?" + query.Encode()

	var decoded struct {
		Count  int  `json:"count"`
		Capped bool `json:"capped"`
	}
	if err := c.getJSON(ctx, session, path, &decoded); err != nil {
		return 0, false, err
	}
	return decoded.Count, decoded.Capped, nil
}

func (c *HTTPAdminClient) EnableFeature(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error) {
	return c.mutateFeature(ctx, session, name, ":enable")
}

func (c *HTTPAdminClient) DisableFeature(ctx context.Context, session AdminSession, name string) (ports.FeatureDetails, error) {
	return c.mutateFeature(ctx, session, name, ":disable")
}

func (c *HTTPAdminClient) CreateUser(ctx context.Context, session AdminSession, input ports.AdminCreateUserInput) (ports.AdminUser, error) {
	var user ports.AdminUser
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, "/admin/v1/users", input, &user, stdhttp.StatusCreated); err != nil {
		return ports.AdminUser{}, err
	}
	return user, nil
}

func (c *HTTPAdminClient) ResetPassword(ctx context.Context, session AdminSession, input ports.AdminResetPasswordInput) error {
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(input.UserID)) + ":reset-password"
	return c.requestNoContent(ctx, stdhttp.MethodPost, session, path, input, stdhttp.StatusNoContent)
}

func (c *HTTPAdminClient) ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error) {
	var grants []ports.AdminRepoGrant
	if err := c.getJSON(ctx, session, "/admin/v1/users/"+url.PathEscape(strings.TrimSpace(userID))+"/grants", &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

func (c *HTTPAdminClient) PutUserGrant(ctx context.Context, session AdminSession, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error) {
	var grant ports.AdminRepoGrant
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(input.UserID)) + "/grants/" + strings.TrimSpace(input.Repository)
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, path, input, &grant, stdhttp.StatusOK); err != nil {
		return ports.AdminRepoGrant{}, err
	}
	return grant, nil
}

func (c *HTTPAdminClient) DeleteUserGrant(ctx context.Context, session AdminSession, userID string, repository string) error {
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(userID)) + "/grants/" + strings.TrimSpace(repository)
	return c.requestNoContent(ctx, stdhttp.MethodDelete, session, path, nil, stdhttp.StatusNoContent)
}

// repositoryGrantsPath/repositoryGrantPath build the delegate-facing
// repository-grant resource paths (design.md Decision 3 route table).
// repository is deliberately NOT PathEscape-d -- it can literally contain
// "/" (mirroring PutUserGrant/DeleteUserGrant's own unescaped-repository
// precedent above); username can never contain "/" so it is escaped.
func repositoryGrantsPath(repository string) string {
	return "/admin/v1/repositories/" + strings.TrimSpace(repository) + "/grants"
}

func repositoryGrantPath(repository string, username string) string {
	return repositoryGrantsPath(repository) + "/" + url.PathEscape(strings.TrimSpace(username))
}

func (c *HTTPAdminClient) ListRepositoryGrants(ctx context.Context, session AdminSession, repository string) ([]ports.AdminRepositoryGrant, error) {
	var grants []ports.AdminRepositoryGrant
	if err := c.getJSON(ctx, session, repositoryGrantsPath(repository), &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

func (c *HTTPAdminClient) PutRepositoryGrant(ctx context.Context, session AdminSession, input ports.AdminPutRepositoryGrantInput) (ports.AdminRepositoryGrant, error) {
	var grant ports.AdminRepositoryGrant
	path := repositoryGrantPath(input.Repository, input.Username)
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, path, input, &grant, stdhttp.StatusOK); err != nil {
		return ports.AdminRepositoryGrant{}, err
	}
	return grant, nil
}

func (c *HTTPAdminClient) DeleteRepositoryGrant(ctx context.Context, session AdminSession, repository string, username string) error {
	path := repositoryGrantPath(repository, username)
	return c.requestNoContent(ctx, stdhttp.MethodDelete, session, path, nil, stdhttp.StatusNoContent)
}

func (c *HTTPAdminClient) ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error) {
	var tokens []ports.AdminToken
	if err := c.getJSON(ctx, session, "/admin/v1/users/"+url.PathEscape(strings.TrimSpace(userID))+"/admin-tokens", &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func (c *HTTPAdminClient) CreateUserAdminToken(ctx context.Context, session AdminSession, input ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error) {
	var created ports.AdminCreatedToken
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(input.UserID)) + "/admin-tokens"
	body := struct {
		Name       string `json:"name"`
		TTLSeconds int64  `json:"ttl_seconds,omitempty"`
	}{
		Name: strings.TrimSpace(input.Name),
	}
	if input.TTL > 0 {
		body.TTLSeconds = int64(input.TTL / time.Second)
	}
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, body, &created, stdhttp.StatusCreated); err != nil {
		return ports.AdminCreatedToken{}, err
	}
	return created, nil
}

func (c *HTTPAdminClient) RevokeUserAdminToken(ctx context.Context, session AdminSession, userID string, accessor string) error {
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(userID)) + "/admin-tokens/" + url.PathEscape(strings.TrimSpace(accessor))
	return c.requestNoContent(ctx, stdhttp.MethodDelete, session, path, nil, stdhttp.StatusNoContent)
}

// repositoryOverridePath builds the admin resource path for one
// (feature, repository) pair. repository is deliberately NOT
// url.PathEscape-d (design.md Decision 8 wire shape): escaping its slashes
// would defeat the server-side split (adminNestedResource splits on the
// last "/repository-overrides/" occurrence, expecting a literal slash-
// bearing repository as the final segment), mirroring PutUserGrant/
// DeleteUserGrant's own unescaped repository precedent.
func repositoryOverridePath(feature string, repository string) string {
	return "/admin/v1/features/" + url.PathEscape(strings.TrimSpace(feature)) + "/repository-overrides/" + strings.TrimSpace(repository)
}

// GetRepositoryOverride fetches one repository's stored override. A 404
// (design.md Decision 7's row-presence boundary) is a valid "no override"
// state, not an error, so the modal can distinguish it from a real failure.
func (c *HTTPAdminClient) GetRepositoryOverride(ctx context.Context, session AdminSession, repository string, feature string) (ports.RepositoryOverrideDetails, bool, error) {
	resp, err := c.request(ctx, stdhttp.MethodGet, session, repositoryOverridePath(feature, repository), nil)
	if err != nil {
		return ports.RepositoryOverrideDetails{}, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == stdhttp.StatusNotFound {
		return ports.RepositoryOverrideDetails{}, false, nil
	}
	if resp.StatusCode != stdhttp.StatusOK {
		return ports.RepositoryOverrideDetails{}, false, decodeAdminAPIError("read admin resource", resp)
	}
	var details ports.RepositoryOverrideDetails
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return ports.RepositoryOverrideDetails{}, false, err
	}
	return details, true, nil
}

// ListRepositoryOverrides fetches every stored override row for one feature
// (design.md Decision 7's "List (TUI annotation)" row), used only to
// annotate the Repository Alerts table.
func (c *HTTPAdminClient) ListRepositoryOverrides(ctx context.Context, session AdminSession, feature string) ([]ports.RepositoryOverrideDetails, error) {
	var overrides []ports.RepositoryOverrideDetails
	path := "/admin/v1/features/" + url.PathEscape(strings.TrimSpace(feature)) + "/repository-overrides"
	if err := c.getJSON(ctx, session, path, &overrides); err != nil {
		return nil, err
	}
	return overrides, nil
}

// SetRepositoryOverride PUTs a full replacement of one repository's
// override. The request body only ever carries the fields the target
// feature's codec accepts (design.md Decision 3's DisallowUnknownFields) --
// gitleaks gets config_path, signing gets trusted_public_keys, every other
// feature (currently only trivy) gets ignore_file_path/ignore_policy_path
// (design.md Decision 11 piece 3).
func (c *HTTPAdminClient) SetRepositoryOverride(ctx context.Context, session AdminSession, repository string, feature string, input ports.RepositoryOverrideDetails) (ports.RepositoryOverrideDetails, error) {
	body := map[string]any{"enabled": input.Enabled}
	switch feature {
	case gitleaksFeatureName:
		body["config_path"] = input.ConfigPath
	case signingFeatureName:
		body["trusted_public_keys"] = input.TrustedPublicKeys
		body["unsigned_self_read"] = input.UnsignedSelfRead
	default:
		body["ignore_file_path"] = input.IgnoreFilePath
		body["ignore_policy_path"] = input.IgnorePolicyPath
	}
	var details ports.RepositoryOverrideDetails
	if err := c.requestJSON(ctx, stdhttp.MethodPut, session, repositoryOverridePath(feature, repository), body, &details, stdhttp.StatusOK); err != nil {
		return ports.RepositoryOverrideDetails{}, err
	}
	return details, nil
}

// ClearRepositoryOverride deletes one repository's override, exactly like
// DeleteUserGrant (design.md Decision 8).
func (c *HTTPAdminClient) ClearRepositoryOverride(ctx context.Context, session AdminSession, repository string, feature string) error {
	return c.requestNoContent(ctx, stdhttp.MethodDelete, session, repositoryOverridePath(feature, repository), nil, stdhttp.StatusNoContent)
}

func (c *HTTPAdminClient) DeleteRobot(ctx context.Context, session AdminSession, userID string) error {
	path := "/admin/v1/robots/" + url.PathEscape(strings.TrimSpace(userID))
	return c.requestNoContent(ctx, stdhttp.MethodDelete, session, path, nil, stdhttp.StatusNoContent)
}

func (c *HTTPAdminClient) EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error) {
	return c.mutateUser(ctx, session, userID, ":enable")
}

func (c *HTTPAdminClient) DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error) {
	return c.mutateUser(ctx, session, userID, ":disable")
}

// ListRobots fetches every robot account (design.md Decision 6's route
// table).
func (c *HTTPAdminClient) ListRobots(ctx context.Context, session AdminSession) ([]ports.AdminRobot, error) {
	var robots []ports.AdminRobot
	if err := c.getJSON(ctx, session, "/admin/v1/robots", &robots); err != nil {
		return nil, err
	}
	return robots, nil
}

// CreateRobot creates a robot account, its single repository grant, and its
// issued admin-credential token in one call (design.md Decision 6), mirroring
// CreateUserAdminToken's ttl_seconds wire convention: omitted (zero) means
// the service's default TTL.
func (c *HTTPAdminClient) CreateRobot(ctx context.Context, session AdminSession, input ports.AdminCreateRobotInput) (ports.AdminCreatedRobot, error) {
	var created ports.AdminCreatedRobot
	body := struct {
		Name       string `json:"name"`
		Repository string `json:"repository"`
		Role       string `json:"role"`
		TTLSeconds int64  `json:"ttl_seconds,omitempty"`
	}{
		Name:       strings.TrimSpace(input.Name),
		Repository: strings.TrimSpace(input.Repository),
		Role:       string(input.Role),
	}
	if input.TTL > 0 {
		body.TTLSeconds = int64(input.TTL / time.Second)
	}
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, "/admin/v1/robots", body, &created, stdhttp.StatusCreated); err != nil {
		return ports.AdminCreatedRobot{}, err
	}
	return created, nil
}

func (c *HTTPAdminClient) mutateUser(ctx context.Context, session AdminSession, userID string, action string) (ports.AdminUser, error) {
	var user ports.AdminUser
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(userID)) + action
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, nil, &user, stdhttp.StatusOK); err != nil {
		return ports.AdminUser{}, err
	}
	return user, nil
}

func (c *HTTPAdminClient) mutateFeature(ctx context.Context, session AdminSession, name string, action string) (ports.FeatureDetails, error) {
	var details ports.FeatureDetails
	path := "/admin/v1/features/" + url.PathEscape(strings.TrimSpace(name)) + action
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, nil, &details, stdhttp.StatusOK); err != nil {
		return ports.FeatureDetails{}, err
	}
	return details, nil
}

func (c *HTTPAdminClient) mutateFeatureRuntime(ctx context.Context, session AdminSession, name string, action string, version string) (ports.FeatureRuntimeState, error) {
	var state ports.FeatureRuntimeState
	path := "/admin/v1/features/" + url.PathEscape(strings.TrimSpace(name)) + action
	body := map[string]string{}
	var payload any
	if strings.TrimSpace(version) != "" {
		body["version"] = strings.TrimSpace(version)
		payload = body
	}
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, payload, &state, stdhttp.StatusOK); err != nil {
		return ports.FeatureRuntimeState{}, err
	}
	return state, nil
}

func (c *HTTPAdminClient) getJSON(ctx context.Context, session AdminSession, path string, target any) error {
	return c.requestJSON(ctx, stdhttp.MethodGet, session, path, nil, target, stdhttp.StatusOK)
}

func (c *HTTPAdminClient) requestJSON(ctx context.Context, method string, session AdminSession, path string, body any, target any, successStatus int) error {
	resp, err := c.request(ctx, method, session, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != successStatus {
		return decodeAdminAPIError(requestActionLabel(method), resp)
	}
	if target == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return err
	}
	return nil
}

func (c *HTTPAdminClient) requestNoContent(ctx context.Context, method string, session AdminSession, path string, body any, successStatus int) error {
	resp, err := c.request(ctx, method, session, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != successStatus {
		return decodeAdminAPIError(requestActionLabel(method), resp)
	}
	return nil
}

func (c *HTTPAdminClient) request(ctx context.Context, method string, session AdminSession, path string, body any) (*stdhttp.Response, error) {
	if session.IsExpired(c.now()) {
		return nil, NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	}

	reader, err := jsonBodyReader(body)
	if err != nil {
		return nil, err
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, c.endpoint(path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+session.BearerToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func jsonBodyReader(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(payload), nil
}

func requestActionLabel(method string) string {
	switch method {
	case stdhttp.MethodGet:
		return "read admin resource"
	default:
		return "mutate admin resource"
	}
}

func (c *HTTPAdminClient) endpoint(path string) string {
	return c.baseURL + path
}

func (c *HTTPAdminClient) getFeatureDetails(ctx context.Context, session AdminSession, path string) (ports.FeatureDetails, error) {
	var details ports.FeatureDetails
	if err := c.getJSON(ctx, session, path, &details); err != nil {
		return ports.FeatureDetails{}, err
	}
	return details, nil
}

func encodeFeatureConfigureInput(input ports.FeatureConfigureInput) map[string]any {
	body := map[string]any{}
	if input.Enabled != nil {
		body["enabled"] = *input.Enabled
	}
	if input.ScheduleEnabled != nil {
		body["schedule_enabled"] = *input.ScheduleEnabled
	}
	if input.Interval != nil {
		body["interval"] = input.Interval.String()
	}
	if input.Timeout != nil {
		body["timeout"] = input.Timeout.String()
	}
	if input.ServiceURL != nil {
		body["service_url"] = *input.ServiceURL
	}
	if input.RegistryReachableURL != nil {
		body["registry_reachable_url"] = *input.RegistryReachableURL
	}
	if input.AuthToken != nil {
		body["auth_token"] = *input.AuthToken
	}
	if input.TLSCACertPath != nil {
		body["tls_ca_cert_path"] = *input.TLSCACertPath
	}
	if input.TLSInsecureSkipVerify != nil {
		body["tls_insecure_skip_verify"] = *input.TLSInsecureSkipVerify
	}
	if input.MaxConcurrency != nil {
		body["max_concurrency"] = *input.MaxConcurrency
	}
	return body
}

func decodeAdminAPIError(action string, resp *stdhttp.Response) error {
	if resp == nil {
		return errors.New("admin API request failed")
	}
	if resp.StatusCode == stdhttp.StatusUnauthorized && strings.Contains(resp.Header.Get("WWW-Authenticate"), `error="invalid_token"`) {
		return NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	}

	message := strings.TrimSpace(readAdminAPIErrorMessage(resp.Body))
	if message == "" {
		message = resp.Status
	}

	return fmt.Errorf("%s: %s", action, message)
}

func readAdminAPIErrorMessage(body io.Reader) string {
	if body == nil {
		return ""
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return ""
	}
	return payload.Error
}

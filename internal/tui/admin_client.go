package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"

	"registry/internal/ports"
)

type AdminClient interface {
	Login(ctx context.Context, username, password string) (AdminSession, error)
	ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error)
	ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error)
	ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error)
	EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
	DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error)
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
		client = stdhttp.DefaultClient
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

func (c *HTTPAdminClient) ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error) {
	var grants []ports.AdminRepoGrant
	if err := c.getJSON(ctx, session, "/admin/v1/users/"+url.PathEscape(strings.TrimSpace(userID))+"/grants", &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

func (c *HTTPAdminClient) ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error) {
	var tokens []ports.AdminToken
	if err := c.getJSON(ctx, session, "/admin/v1/users/"+url.PathEscape(strings.TrimSpace(userID))+"/admin-tokens", &tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

func (c *HTTPAdminClient) EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error) {
	return c.mutateUser(ctx, session, userID, ":enable")
}

func (c *HTTPAdminClient) DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error) {
	return c.mutateUser(ctx, session, userID, ":disable")
}

func (c *HTTPAdminClient) mutateUser(ctx context.Context, session AdminSession, userID string, action string) (ports.AdminUser, error) {
	var user ports.AdminUser
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(userID)) + action
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, &user); err != nil {
		return ports.AdminUser{}, err
	}
	return user, nil
}

func (c *HTTPAdminClient) getJSON(ctx context.Context, session AdminSession, path string, target any) error {
	return c.requestJSON(ctx, stdhttp.MethodGet, session, path, target)
}

func (c *HTTPAdminClient) requestJSON(ctx context.Context, method string, session AdminSession, path string, target any) error {
	if session.IsExpired(c.now()) {
		return NewAdminSessionExpiredError(AdminSessionExpiredReasonExpired)
	}

	req, err := stdhttp.NewRequestWithContext(ctx, method, c.endpoint(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+session.BearerToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != stdhttp.StatusOK {
		return decodeAdminAPIError(requestActionLabel(method), resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return err
	}
	return nil
}

func requestActionLabel(method string) string {
	switch method {
	case stdhttp.MethodPost:
		return "mutate admin resource"
	default:
		return "read admin resource"
	}
}

func (c *HTTPAdminClient) endpoint(path string) string {
	return c.baseURL + path
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

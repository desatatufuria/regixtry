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
	"strings"
	"time"

	"regixtry/internal/ports"
)

const defaultAdminClientTimeout = 15 * time.Second

type AdminClient interface {
	Login(ctx context.Context, username, password string) (AdminSession, error)
	ListUsers(ctx context.Context, session AdminSession) ([]ports.AdminUser, error)
	CreateUser(ctx context.Context, session AdminSession, input ports.AdminCreateUserInput) (ports.AdminUser, error)
	ResetPassword(ctx context.Context, session AdminSession, input ports.AdminResetPasswordInput) error
	ListUserGrants(ctx context.Context, session AdminSession, userID string) ([]ports.AdminRepoGrant, error)
	PutUserGrant(ctx context.Context, session AdminSession, input ports.AdminPutRepoGrantInput) (ports.AdminRepoGrant, error)
	DeleteUserGrant(ctx context.Context, session AdminSession, userID string, repository string) error
	ListUserAdminTokens(ctx context.Context, session AdminSession, userID string) ([]ports.AdminToken, error)
	CreateUserAdminToken(ctx context.Context, session AdminSession, input ports.AdminCreateTokenInput) (ports.AdminCreatedToken, error)
	RevokeUserAdminToken(ctx context.Context, session AdminSession, userID string, accessor string) error
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

func (c *HTTPAdminClient) EnableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error) {
	return c.mutateUser(ctx, session, userID, ":enable")
}

func (c *HTTPAdminClient) DisableUser(ctx context.Context, session AdminSession, userID string) (ports.AdminUser, error) {
	return c.mutateUser(ctx, session, userID, ":disable")
}

func (c *HTTPAdminClient) mutateUser(ctx context.Context, session AdminSession, userID string, action string) (ports.AdminUser, error) {
	var user ports.AdminUser
	path := "/admin/v1/users/" + url.PathEscape(strings.TrimSpace(userID)) + action
	if err := c.requestJSON(ctx, stdhttp.MethodPost, session, path, nil, &user, stdhttp.StatusOK); err != nil {
		return ports.AdminUser{}, err
	}
	return user, nil
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

package regixtryhttp

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	stdhttp "net/http"
	domainauth "regixtry/internal/domain/auth"
	domainregistry "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

func (r *Router) handleAdmin(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if r.admin == nil || r.auth == nil {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}

	principal, ok := r.requireAdminPrincipal(w, req)
	if !ok {
		return
	}

	subpath := strings.Trim(strings.TrimPrefix(req.URL.Path, "/admin/v1"), "/")
	if subpath == "" {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}

	switch {
	case subpath == "scan-settings":
		r.handleAdminScanSettings(w, req)
	case subpath == "scan-runs":
		r.handleAdminScanRuns(w, req)
	case subpath == "users":
		r.handleAdminUsersCollection(w, req, *principal)
	case strings.HasPrefix(subpath, "users/"):
		r.handleAdminUserResource(w, req, *principal, strings.TrimPrefix(subpath, "users/"))
	default:
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
	}
}

func (r *Router) handleAdminScanSettings(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	switch req.Method {
	case stdhttp.MethodGet:
		settings, err := r.service.GetScanSettings(req.Context())
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, scanSettingsResponse(settings))
	case stdhttp.MethodPut:
		settings, err := decodeScanSettings(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		updated, err := r.service.UpdateScanSettings(req.Context(), settings)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, scanSettingsResponse(updated))
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPut}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminScanRuns(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	switch req.Method {
	case stdhttp.MethodGet:
		repository := strings.TrimSpace(req.URL.Query().Get("repository"))
		limit := 20
		if raw := strings.TrimSpace(req.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeAdminError(w, domainauth.NewValidationError("limit must be a positive integer"), ports.Challenge{})
				return
			}
			limit = parsed
		}
		runs, err := r.service.ListScanRuns(req.Context(), repository, limit)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, runs)
	case stdhttp.MethodPost:
		var payload struct {
			Repository string `json:"repository"`
			Reference  string `json:"reference"`
		}
		if err := decodeAdminJSON(req, &payload); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		run, err := r.service.QueueManualScan(req.Context(), payload.Repository, payload.Reference)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusAccepted, run)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func decodeScanSettings(req *stdhttp.Request) (ports.ScanSettings, error) {
	var payload struct {
		Enabled         bool   `json:"enabled"`
		ScheduleEnabled bool   `json:"schedule_enabled"`
		Interval        string `json:"interval"`
		Timeout         string `json:"timeout"`
		CacheDir        string `json:"cache_dir"`
		BinaryPath      string `json:"binary_path"`
		MaxConcurrency  int    `json:"max_concurrency"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.ScanSettings{}, err
	}
	interval, err := time.ParseDuration(strings.TrimSpace(payload.Interval))
	if err != nil {
		return ports.ScanSettings{}, domainauth.NewValidationError("interval must be a valid duration")
	}
	timeout, err := time.ParseDuration(strings.TrimSpace(payload.Timeout))
	if err != nil {
		return ports.ScanSettings{}, domainauth.NewValidationError("timeout must be a valid duration")
	}
	return ports.ScanSettings{Enabled: payload.Enabled, ScheduleEnabled: payload.ScheduleEnabled, Interval: interval, Timeout: timeout, CacheDir: payload.CacheDir, BinaryPath: payload.BinaryPath, MaxConcurrency: payload.MaxConcurrency}, nil
}

func scanSettingsResponse(settings ports.ScanSettings) map[string]any {
	return map[string]any{
		"enabled":          settings.Enabled,
		"schedule_enabled": settings.ScheduleEnabled,
		"interval":         settings.Interval.String(),
		"timeout":          settings.Timeout.String(),
		"cache_dir":        settings.CacheDir,
		"binary_path":      settings.BinaryPath,
		"max_concurrency":  settings.MaxConcurrency,
		"updated_at":       settings.UpdatedAt,
	}
}

func (r *Router) handleAdminUsersCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	switch req.Method {
	case stdhttp.MethodGet:
		users, err := r.admin.ListAdminUsers(req.Context(), principal)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusOK, users)
	case stdhttp.MethodPost:
		var input ports.AdminCreateUserInput
		if err := decodeAdminJSON(req, &input); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		user, err := r.admin.CreateAdminUser(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		writeJSON(w, stdhttp.StatusCreated, user)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminUserResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, resource string) {
	switch {
	case strings.HasSuffix(resource, ":enable"):
		userID, ok := adminResourceID(resource, ":enable")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserEnablement(w, req, principal, userID, true)
	case strings.HasSuffix(resource, ":disable"):
		userID, ok := adminResourceID(resource, ":disable")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserEnablement(w, req, principal, userID, false)
	case strings.HasSuffix(resource, ":reset-password"):
		userID, ok := adminResourceID(resource, ":reset-password")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserPasswordReset(w, req, principal, userID)
	case strings.HasSuffix(resource, "/grants"):
		userID, ok := adminNestedUserID(resource, "/grants")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserGrantsCollection(w, req, principal, userID)
	case strings.Contains(resource, "/grants/"):
		userID, repository, ok := adminNestedResource(resource, "/grants/")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserGrantResource(w, req, principal, userID, repository)
	case strings.HasSuffix(resource, "/admin-tokens"):
		userID, ok := adminNestedUserID(resource, "/admin-tokens")
		if !ok {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserTokensCollection(w, req, principal, userID)
	case strings.Contains(resource, "/admin-tokens/"):
		userID, accessor, ok := adminNestedResource(resource, "/admin-tokens/")
		if !ok || strings.Contains(accessor, "/") {
			writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
			return
		}
		r.handleAdminUserTokenResource(w, req, principal, userID, accessor)
	default:
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
	}
}

func (r *Router) handleAdminUserGrantsCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	grants, err := r.admin.ListAdminUserRepoGrants(req.Context(), principal, userID)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, grants)
}

func (r *Router) handleAdminUserGrantResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string, repository string) {
	switch req.Method {
	case stdhttp.MethodPut:
		var input ports.AdminPutRepoGrantInput
		if err := decodeAdminJSON(req, &input); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		input.UserID = userID
		input.Repository = repository

		grant, err := r.admin.PutAdminUserRepoGrant(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusOK, grant)
	case stdhttp.MethodDelete:
		if err := r.admin.DeleteAdminUserRepoGrant(req.Context(), principal, userID, repository); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		w.WriteHeader(stdhttp.StatusNoContent)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminUserTokensCollection(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	switch req.Method {
	case stdhttp.MethodGet:
		tokens, err := r.admin.ListAdminUserTokens(req.Context(), principal, userID)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusOK, tokens)
	case stdhttp.MethodPost:
		input, err := decodeAdminCreateTokenInput(req)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
		input.UserID = userID

		created, err := r.admin.CreateAdminUserToken(req.Context(), principal, input)
		if err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}

		writeJSON(w, stdhttp.StatusCreated, created)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleAdminUserTokenResource(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string, accessor string) {
	if req.Method != stdhttp.MethodDelete {
		w.Header().Set("Allow", stdhttp.MethodDelete)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	if err := r.admin.RevokeAdminUserToken(req.Context(), principal, userID, accessor); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

func (r *Router) handleAdminUserEnablement(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string, enabled bool) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	var (
		user ports.AdminUser
		err  error
	)
	if enabled {
		user, err = r.admin.EnableAdminUser(req.Context(), principal, userID)
	} else {
		user, err = r.admin.DisableAdminUser(req.Context(), principal, userID)
	}
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	writeJSON(w, stdhttp.StatusOK, user)
}

func (r *Router) handleAdminUserPasswordReset(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, userID string) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	var input ports.AdminResetPasswordInput
	if err := decodeAdminJSON(req, &input); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	input.UserID = userID

	if err := r.admin.ResetAdminUserPassword(req.Context(), principal, input); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}

	w.WriteHeader(stdhttp.StatusNoContent)
}

func adminResourceID(resource string, suffix string) (string, bool) {
	userID := strings.TrimSpace(strings.TrimSuffix(resource, suffix))
	if userID == "" || strings.Contains(userID, "/") {
		return "", false
	}
	return userID, true
}

func adminNestedUserID(resource string, suffix string) (string, bool) {
	return adminResourceID(resource, suffix)
}

func adminNestedResource(resource string, marker string) (string, string, bool) {
	parts := strings.SplitN(resource, marker, 2)
	if len(parts) != 2 {
		return "", "", false
	}

	userID := strings.TrimSpace(parts[0])
	nested := strings.TrimSpace(parts[1])
	if userID == "" || nested == "" || strings.Contains(userID, "/") {
		return "", "", false
	}

	return userID, nested, true
}

func decodeAdminCreateTokenInput(req *stdhttp.Request) (ports.AdminCreateTokenInput, error) {
	var payload struct {
		Name       string `json:"name"`
		TTLSeconds *int64 `json:"ttl_seconds"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		return ports.AdminCreateTokenInput{}, err
	}
	if payload.TTLSeconds != nil && *payload.TTLSeconds < 0 {
		return ports.AdminCreateTokenInput{}, domainauth.NewValidationError("ttl_seconds must be zero or greater")
	}

	input := ports.AdminCreateTokenInput{Name: payload.Name}
	if payload.TTLSeconds != nil {
		input.TTL = time.Duration(*payload.TTLSeconds) * time.Second
	}

	return input, nil
}

func decodeAdminJSON(req *stdhttp.Request, dst any) error {
	if req.Body == nil {
		return domainauth.NewValidationError("request body is required")
	}
	defer req.Body.Close()

	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return domainauth.NewValidationError("request body is required")
		}
		var unmarshalTypeError *json.UnmarshalTypeError
		if errors.As(err, &unmarshalTypeError) {
			field := strings.TrimSpace(unmarshalTypeError.Field)
			if field == "" {
				field = strings.TrimSpace(unmarshalTypeError.Struct)
			}
			if field != "" {
				return domainauth.NewValidationError(field + " has an invalid type")
			}
		}
		var numErr *strconv.NumError
		if errors.As(err, &numErr) {
			return domainauth.NewValidationError("request body contains an invalid number")
		}
		return domainauth.NewValidationError("request body must be valid JSON")
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return domainauth.NewValidationError("request body must contain a single JSON object")
	}

	return nil
}

func (r *Router) requireAdminPrincipal(w stdhttp.ResponseWriter, req *stdhttp.Request) (*domainauth.Principal, bool) {
	bearerChallenge := r.adminChallenge(req)
	principal, err := r.authenticate(req)
	if err != nil {
		writeAdminError(w, err, bearerChallenge)
		return nil, false
	}
	if principal == nil {
		writeAdminError(w, domainauth.NewInvalidCredentialsError(), bearerChallenge)
		return nil, false
	}
	if !principal.IsAdmin {
		writeAdminError(w, domainauth.NewForbiddenError("admin access is required"), ports.Challenge{})
		return nil, false
	}

	return principal, true
}

func (r *Router) adminChallenge(req *stdhttp.Request) ports.Challenge {
	challenge := r.service.Challenge(ports.Action{})
	challenge.Scope = ""
	if req != nil && strings.TrimSpace(req.Header.Get("Authorization")) != "" {
		challenge.Error = "invalid_token"
	}
	return challenge
}

func writeAdminError(w stdhttp.ResponseWriter, err error, challenge ports.Challenge) {
	status := stdhttp.StatusInternalServerError

	message := err.Error()
	if registryErr, ok := err.(*domainregistry.Error); ok {
		message = registryErr.Message
		switch registryErr.Code {
		case domainregistry.ErrorCodeInvalidRepository, domainregistry.ErrorCodeValidation:
			status = stdhttp.StatusUnprocessableEntity
		case domainregistry.ErrorCodeNotFound:
			status = stdhttp.StatusNotFound
		case domainregistry.ErrorCodeConflict:
			status = stdhttp.StatusConflict
		default:
			status = stdhttp.StatusInternalServerError
		}
	} else if authErr, ok := err.(*domainauth.Error); ok {
		message = authErr.Message
		switch authErr.Code {
		case domainauth.ErrorCodeInvalidCredentials, domainauth.ErrorCodeExpiredToken, domainauth.ErrorCodeRevokedToken, domainauth.ErrorCodeDisabledUser:
			status = stdhttp.StatusUnauthorized
			w.Header().Set("WWW-Authenticate", challengeHeader(challenge))
		case domainauth.ErrorCodeForbidden:
			status = stdhttp.StatusForbidden
		case domainauth.ErrorCodeValidation:
			status = stdhttp.StatusUnprocessableEntity
		case domainauth.ErrorCodeNotFound:
			status = stdhttp.StatusNotFound
		case domainauth.ErrorCodeConflict:
			status = stdhttp.StatusConflict
		default:
			status = stdhttp.StatusInternalServerError
		}
	}

	writeJSON(w, status, map[string]any{"error": message})
}

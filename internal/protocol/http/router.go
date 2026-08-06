package registryhttp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	appregistry "registry/internal/app/registry"
	domainauth "registry/internal/domain/auth"
	domain "registry/internal/domain/registry"
	"registry/internal/ports"
)

type Router struct {
	service *appregistry.Service
	auth    ports.AuthService
	admin   ports.AdminHTTPService
	logger  *log.Logger
	mux     *stdhttp.ServeMux
}

type RouterOption func(*Router)

func WithLogger(logger *log.Logger) RouterOption {
	return func(router *Router) {
		if logger != nil {
			router.logger = logger
		}
	}
}

func NewRouter(service *appregistry.Service, authService ports.AuthService, options ...RouterOption) *Router {
	router := &Router{service: service, auth: authService, logger: log.Default(), mux: stdhttp.NewServeMux()}
	if adminService, ok := authService.(ports.AdminHTTPService); ok {
		router.admin = adminService
	}
	for _, option := range options {
		if option != nil {
			option(router)
		}
	}
	if authService != nil {
		router.mux.HandleFunc("/auth/token", router.handleToken)
	}
	if router.admin != nil {
		router.mux.HandleFunc("/admin/v1", router.handleAdmin)
		router.mux.HandleFunc("/admin/v1/", router.handleAdmin)
		router.mux.HandleFunc("/admin/v1/users", router.handleAdmin)
		router.mux.HandleFunc("/admin/v1/users/", router.handleAdmin)
	}
	router.mux.HandleFunc("/v2/", router.handleV2)
	router.mux.HandleFunc("/v2", router.handleV2)
	return router
}

func (r *Router) ServeHTTP(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	start := time.Now()
	recorder := &statusCapturingResponseWriter{ResponseWriter: w}
	r.mux.ServeHTTP(recorder, req)
	r.logRequest(req, recorder.statusCode(), time.Since(start), recorder.Header().Get("WWW-Authenticate"))
}

func (r *Router) logRequest(req *stdhttp.Request, statusCode int, duration time.Duration, authChallenge string) {
	if r.logger == nil {
		return
	}

	message := fmt.Sprintf("registry request method=%s path=%s status=%d duration=%s", req.Method, requestPath(req), statusCode, duration.Round(time.Microsecond))
	if authChallenge != "" {
		message += fmt.Sprintf(" auth_challenge=%q", authChallenge)
	}

	r.logger.Print(message)
}

func requestPath(req *stdhttp.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	if req.URL.RawQuery == "" {
		return req.URL.Path
	}
	return req.URL.Path + "?" + req.URL.RawQuery
}

type statusCapturingResponseWriter struct {
	stdhttp.ResponseWriter
	status int
}

func (w *statusCapturingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturingResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = stdhttp.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func (w *statusCapturingResponseWriter) statusCode() int {
	if w.status == 0 {
		return stdhttp.StatusOK
	}
	return w.status
}

func (r *Router) handleV2(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.URL.Path == "/v2" || req.URL.Path == "/v2/" {
		if r.auth != nil {
			principal, err := r.authenticate(req)
			if err != nil {
				writeError(w, req, err, r.challengeForError(ports.Action{}, err), "UNAUTHORIZED")
				return
			}
			if principal == nil {
				writeError(w, req, domain.NewUnauthorizedError("authentication required"), r.service.Challenge(ports.Action{}), "UNAUTHORIZED")
				return
			}
		}

		w.WriteHeader(stdhttp.StatusOK)
		return
	}

	path := strings.TrimPrefix(req.URL.Path, "/v2/")
	if path == "_catalog" {
		r.handleCatalog(w, req)
		return
	}

	repository, suffix, ok := splitRepositoryPath(path)
	if !ok {
		writeError(w, req, domain.NewNotFoundError("route", req.URL.Path), r.service.Challenge(ports.Action{}), "NAME_UNKNOWN")
		return
	}

	switch {
	case suffix == "blobs/uploads" || suffix == "blobs/uploads/":
		r.handleUploadStart(w, req, repository)
	case strings.HasPrefix(suffix, "blobs/uploads/"):
		routeID := strings.TrimPrefix(suffix, "blobs/uploads/")
		r.handleUploadState(w, req, repository, routeID)
	case strings.HasPrefix(suffix, "blobs/"):
		r.handleBlobRead(w, req, repository, strings.TrimPrefix(suffix, "blobs/"))
	case strings.HasPrefix(suffix, "manifests/"):
		r.handleManifest(w, req, repository, strings.TrimPrefix(suffix, "manifests/"))
	case suffix == "tags/list":
		r.handleTags(w, req, repository)
	default:
		writeError(w, req, domain.NewNotFoundError("route", req.URL.Path), r.service.Challenge(ports.Action{}), "NAME_UNKNOWN")
	}
}

func (r *Router) handleCatalog(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	action := ports.Action{Verb: ports.ActionCatalog}
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	limit, after, err := parsePagination(req)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(action), "NAME_INVALID")
		return
	}

	req, ok := r.withPrincipal(w, req, action)
	if !ok {
		return
	}

	result, err := r.service.Catalog(req.Context(), limit, after)
	if err != nil {
		writeError(w, req, err, r.challengeForError(action, err), "NAME_UNKNOWN")
		return
	}

	writeJSON(w, stdhttp.StatusOK, result)
}

func (r *Router) handleUploadStart(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string) {
	action := ports.Action{Verb: ports.ActionPush, Repository: repository}
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	req, ok := r.withPrincipal(w, req, action)
	if !ok {
		return
	}

	state, err := r.service.BeginUpload(req.Context(), repository)
	if err != nil {
		writeError(w, req, err, r.challengeForError(action, err), "BLOB_UPLOAD_UNKNOWN")
		return
	}

	setUploadHeaders(w.Header(), repository, state.ID, state.Size)
	w.WriteHeader(stdhttp.StatusAccepted)
}

func (r *Router) handleUploadState(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string, uploadID string) {
	action := ports.Action{Verb: ports.ActionPush, Repository: repository}
	req, ok := r.withPrincipal(w, req, action)
	if !ok {
		return
	}

	switch req.Method {
	case stdhttp.MethodGet, stdhttp.MethodHead:
		state, err := r.service.UploadStatus(req.Context(), repository, uploadID)
		if err != nil {
			writeError(w, req, err, r.challengeForError(action, err), "BLOB_UPLOAD_UNKNOWN")
			return
		}

		setUploadHeaders(w.Header(), repository, state.ID, state.Size)
		w.WriteHeader(stdhttp.StatusNoContent)
	case stdhttp.MethodPatch:
		defer req.Body.Close()
		state, err := r.service.AppendUpload(req.Context(), repository, uploadID, req.Body)
		if err != nil {
			writeError(w, req, err, r.challengeForError(action, err), "BLOB_UPLOAD_UNKNOWN")
			return
		}

		setUploadHeaders(w.Header(), repository, state.ID, state.Size)
		w.WriteHeader(stdhttp.StatusAccepted)
	case stdhttp.MethodPut:
		digest := req.URL.Query().Get("digest")
		var body io.Reader
		if req.Body != nil {
			defer req.Body.Close()
			body = req.Body
		}

		blob, err := r.service.CompleteUpload(req.Context(), repository, uploadID, digest, body)
		if err != nil {
			writeError(w, req, err, r.challengeForError(action, err), "BLOB_UPLOAD_INVALID")
			return
		}

		w.Header().Set("Location", blobLocation(repository, blob.Digest))
		w.Header().Set("Docker-Content-Digest", blob.Digest)
		w.WriteHeader(stdhttp.StatusCreated)
	case stdhttp.MethodDelete:
		writeError(w, req, domain.NewValidationError("upload cancellation is not implemented in this slice"), r.service.Challenge(action), "UNSUPPORTED")
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodPatch, stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleBlobRead(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string, digest string) {
	action := ports.Action{Verb: ports.ActionPull, Repository: repository}
	if req.Method != stdhttp.MethodGet && req.Method != stdhttp.MethodHead {
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodHead}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	req, ok := r.withPrincipal(w, req, action)
	if !ok {
		return
	}

	reader, descriptor, err := r.service.OpenBlob(req.Context(), repository, digest)
	if err != nil {
		writeError(w, req, err, r.challengeForError(action, err), "BLOB_UNKNOWN")
		return
	}
	defer reader.Close()

	writeBlobHeaders(w.Header(), descriptor.MediaType, descriptor.Digest, descriptor.Size)
	if req.Method == stdhttp.MethodHead {
		w.WriteHeader(stdhttp.StatusOK)
		return
	}

	w.WriteHeader(stdhttp.StatusOK)
	_, _ = io.Copy(w, reader)
}

func (r *Router) handleManifest(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string, reference string) {
	action := ports.Action{Repository: repository}
	if req.Method == stdhttp.MethodPut {
		action.Verb = ports.ActionPush
	} else {
		action.Verb = ports.ActionPull
	}

	req, ok := r.withPrincipal(w, req, action)
	if !ok {
		return
	}

	switch req.Method {
	case stdhttp.MethodPut:
		defer req.Body.Close()
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, req, err, r.service.Challenge(action), "MANIFEST_INVALID")
			return
		}

		manifest, err := r.service.PublishManifest(req.Context(), repository, reference, req.Header.Get("Content-Type"), payload)
		if err != nil {
			writeError(w, req, err, r.challengeForError(action, err), "MANIFEST_INVALID")
			return
		}

		w.Header().Set("Location", manifestLocation(repository, reference))
		w.Header().Set("Docker-Content-Digest", manifest.Digest)
		w.WriteHeader(stdhttp.StatusCreated)
	case stdhttp.MethodGet, stdhttp.MethodHead:
		manifest, err := r.service.OpenManifest(req.Context(), repository, reference)
		if err != nil {
			writeError(w, req, err, r.challengeForError(action, err), "MANIFEST_UNKNOWN")
			return
		}

		writeBlobHeaders(w.Header(), manifest.MediaType, manifest.Digest.String(), manifest.Size)
		if req.Method == stdhttp.MethodHead {
			w.WriteHeader(stdhttp.StatusOK)
			return
		}

		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write(manifest.Payload)
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodPut, stdhttp.MethodGet, stdhttp.MethodHead}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleTags(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string) {
	action := ports.Action{Verb: ports.ActionInspect, Repository: repository}
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	limit, after, err := parsePagination(req)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(action), "NAME_INVALID")
		return
	}

	req, ok := r.withPrincipal(w, req, action)
	if !ok {
		return
	}

	tags, err := r.service.Tags(req.Context(), repository, limit, after)
	if err != nil {
		writeError(w, req, err, r.challengeForError(action, err), "NAME_UNKNOWN")
		return
	}

	writeJSON(w, stdhttp.StatusOK, tags)
}

func (r *Router) handleToken(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	challenge := ports.Challenge{Scheme: "Basic", Realm: registryRealm(r.service.Challenge(ports.Action{}))}
	if req.Method != stdhttp.MethodGet && req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodPost}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	username, secret, ok := req.BasicAuth()
	if !ok || strings.TrimSpace(username) == "" || strings.TrimSpace(secret) == "" {
		writeError(w, req, domainauth.NewInvalidCredentialsError(), challenge, "UNAUTHORIZED")
		return
	}

	requestedScopes, err := requestedScopes(req)
	if err != nil {
		writeError(w, req, err, challenge, "UNAUTHORIZED")
		return
	}

	result, err := r.auth.LoginWithPassword(req.Context(), username, secret, requestedScopes)
	if err != nil && domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) {
		result, err = r.auth.LoginWithPreissuedToken(req.Context(), username, secret, requestedScopes)
	}
	if err != nil {
		writeError(w, req, err, challenge, "UNAUTHORIZED")
		return
	}

	issuedAt := result.ExpiresAt.Add(-domainauth.AccessTokenTTL).UTC().Format(time.RFC3339)
	response := map[string]any{
		"token":        result.BearerToken,
		"access_token": result.BearerToken,
		"expires_in":   int(domainauth.AccessTokenTTL.Seconds()),
		"issued_at":    issuedAt,
	}
	if service := strings.TrimSpace(req.URL.Query().Get("service")); service != "" {
		response["service"] = service
	}
	if result.Scope != "" {
		response["scope"] = result.Scope
	}

	writeJSON(w, stdhttp.StatusOK, response)
}

func (r *Router) withPrincipal(w stdhttp.ResponseWriter, req *stdhttp.Request, action ports.Action) (*stdhttp.Request, bool) {
	if r.auth == nil {
		return req, true
	}

	principal, err := r.authenticate(req)
	if err != nil {
		writeError(w, req, err, r.challengeForError(action, err), "UNAUTHORIZED")
		return nil, false
	}
	if principal == nil {
		return req, true
	}

	return req.WithContext(ports.ContextWithPrincipal(req.Context(), *principal)), true
}

func (r *Router) authenticate(req *stdhttp.Request) (*domainauth.Principal, error) {
	authorization := strings.TrimSpace(req.Header.Get("Authorization"))
	if authorization == "" {
		return nil, nil
	}

	parts := strings.SplitN(authorization, " ", 2)
	if len(parts) != 2 {
		return nil, domainauth.NewInvalidCredentialsError()
	}

	if !strings.EqualFold(strings.TrimSpace(parts[0]), "Bearer") {
		return nil, nil
	}

	bearerToken := strings.TrimSpace(parts[1])
	if bearerToken == "" {
		return nil, domainauth.NewInvalidCredentialsError()
	}

	principal, err := r.auth.VerifyAccessToken(req.Context(), bearerToken)
	if err != nil {
		return nil, err
	}

	return &principal, nil
}

func (r *Router) challengeForError(action ports.Action, err error) ports.Challenge {
	challenge := r.service.Challenge(action)
	if !isInvalidTokenError(err) {
		return challenge
	}

	challenge.Error = "invalid_token"
	return challenge
}

func isInvalidTokenError(err error) bool {
	return domainauth.IsCode(err, domainauth.ErrorCodeInvalidCredentials) ||
		domainauth.IsCode(err, domainauth.ErrorCodeExpiredToken) ||
		domainauth.IsCode(err, domainauth.ErrorCodeRevokedToken) ||
		domainauth.IsCode(err, domainauth.ErrorCodeDisabledUser)
}

func registryRealm(challenge ports.Challenge) string {
	if challenge.Realm != "" {
		return challenge.Realm
	}

	return "registry"
}

func requestedScopes(req *stdhttp.Request) ([]domainauth.Scope, error) {
	if req == nil {
		return nil, nil
	}

	if err := req.ParseForm(); err != nil {
		return nil, domainauth.NewValidationError("scope request could not be parsed")
	}

	return domainauth.ParseScopes(req.Form["scope"])
}

func parsePagination(req *stdhttp.Request) (int, string, error) {
	limit := 0
	if raw := strings.TrimSpace(req.URL.Query().Get("n")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return 0, "", domain.NewValidationError("pagination parameter n must be a positive integer")
		}
		limit = value
	}

	return limit, strings.TrimSpace(req.URL.Query().Get("last")), nil
}

func splitRepositoryPath(path string) (string, string, bool) {
	markers := []string{"/blobs/uploads/", "/blobs/uploads", "/blobs/", "/manifests/", "/tags/list"}
	for _, marker := range markers {
		if index := strings.Index(path, marker); index > 0 {
			return path[:index], strings.TrimPrefix(path[index+1:], "/"), true
		}
		if strings.HasSuffix(path, strings.TrimPrefix(marker, "/")) {
			repository := strings.TrimSuffix(path, strings.TrimPrefix(marker, "/"))
			repository = strings.TrimSuffix(repository, "/")
			if repository != "" {
				return repository, strings.TrimPrefix(marker, "/"), true
			}
		}
	}

	return "", "", false
}

func setUploadHeaders(header stdhttp.Header, repository string, uploadID string, size int64) {
	header.Set("Location", uploadLocation(repository, uploadID))
	header.Set("Docker-Upload-UUID", uploadID)
	header.Set("Range", uploadRange(size))
	header.Set("Content-Length", "0")
	if size > 0 {
		header.Set("OCI-Chunk-Min-Length", strconv.FormatInt(size, 10))
	}
}

func writeBlobHeaders(header stdhttp.Header, mediaType string, digest string, size int64) {
	if mediaType != "" {
		header.Set("Content-Type", mediaType)
	}
	header.Set("Docker-Content-Digest", digest)
	header.Set("Content-Length", strconv.FormatInt(size, 10))
}

func writeError(w stdhttp.ResponseWriter, req *stdhttp.Request, err error, challenge ports.Challenge, defaultCode string) {
	status := stdhttp.StatusInternalServerError
	code := defaultCode
	message := err.Error()

	if registryErr, ok := err.(*domain.Error); ok {
		message = registryErr.Message
		switch registryErr.Code {
		case domain.ErrorCodeUnauthorized:
			status = stdhttp.StatusUnauthorized
			code = "UNAUTHORIZED"
			w.Header().Set("WWW-Authenticate", challengeHeader(challenge))
		case domain.ErrorCodeNotFound:
			status = stdhttp.StatusNotFound
		case domain.ErrorCodeInvalidDigest, domain.ErrorCodeDigestMismatch, domain.ErrorCodeInvalidManifest, domain.ErrorCodeValidation:
			status = stdhttp.StatusBadRequest
			if code == "" {
				code = "DIGEST_INVALID"
			}
		case domain.ErrorCodeConflict:
			status = stdhttp.StatusConflict
			if code == "" {
				code = "DENIED"
			}
		default:
			status = stdhttp.StatusInternalServerError
		}
	} else if authErr, ok := err.(*domainauth.Error); ok {
		message = authErr.Message
		switch authErr.Code {
		case domainauth.ErrorCodeInvalidCredentials, domainauth.ErrorCodeExpiredToken, domainauth.ErrorCodeRevokedToken, domainauth.ErrorCodeDisabledUser, domainauth.ErrorCodeForbidden:
			status = stdhttp.StatusUnauthorized
			code = "UNAUTHORIZED"
			w.Header().Set("WWW-Authenticate", challengeHeader(challenge))
		case domainauth.ErrorCodeValidation:
			status = stdhttp.StatusBadRequest
		case domainauth.ErrorCodeNotFound:
			status = stdhttp.StatusNotFound
		case domainauth.ErrorCodeConflict:
			status = stdhttp.StatusConflict
		default:
			status = stdhttp.StatusInternalServerError
		}
	}

	writeJSON(w, status, map[string]any{
		"errors": []map[string]string{{
			"code":    code,
			"message": message,
		}},
	})
}

func writeJSON(w stdhttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func challengeHeader(challenge ports.Challenge) string {
	if challenge.Scheme == "" {
		challenge.Scheme = "Bearer"
	}

	parts := []string{}
	if challenge.Realm != "" {
		parts = append(parts, fmt.Sprintf(`realm=%q`, challenge.Realm))
	}
	if challenge.Service != "" {
		parts = append(parts, fmt.Sprintf(`service=%q`, challenge.Service))
	}
	if challenge.Scope != "" {
		parts = append(parts, fmt.Sprintf(`scope=%q`, challenge.Scope))
	}
	if challenge.Error != "" {
		parts = append(parts, fmt.Sprintf(`error=%q`, challenge.Error))
	}

	if len(parts) == 0 {
		return challenge.Scheme
	}

	return challenge.Scheme + " " + strings.Join(parts, ",")
}

func uploadLocation(repository string, uploadID string) string {
	return "/v2/" + repository + "/blobs/uploads/" + uploadID
}

func blobLocation(repository string, digest string) string {
	return "/v2/" + repository + "/blobs/" + digest
}

func manifestLocation(repository string, reference string) string {
	return "/v2/" + repository + "/manifests/" + reference
}

func uploadRange(size int64) string {
	if size <= 0 {
		return "0-0"
	}
	return fmt.Sprintf("0-%d", size-1)
}

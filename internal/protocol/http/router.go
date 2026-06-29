package registryhttp

import (
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"strconv"
	"strings"

	appregistry "registry/internal/app/registry"
	domain "registry/internal/domain/registry"
	"registry/internal/ports"
)

type Router struct {
	service *appregistry.Service
	mux     *stdhttp.ServeMux
}

func NewRouter(service *appregistry.Service) *Router {
	router := &Router{service: service, mux: stdhttp.NewServeMux()}
	router.mux.HandleFunc("/v2/", router.handleV2)
	router.mux.HandleFunc("/v2", router.handleV2)
	return router
}

func (r *Router) ServeHTTP(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) handleV2(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.URL.Path == "/v2" || req.URL.Path == "/v2/" {
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
		writeError(w, req, domain.NewNotFoundError("route", req.URL.Path), r.service.Challenge(), "NAME_UNKNOWN")
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
		writeError(w, req, domain.NewNotFoundError("route", req.URL.Path), r.service.Challenge(), "NAME_UNKNOWN")
	}
}

func (r *Router) handleCatalog(w stdhttp.ResponseWriter, req *stdhttp.Request) {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	limit, after, err := parsePagination(req)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(), "NAME_INVALID")
		return
	}

	result, err := r.service.Catalog(req.Context(), limit, after)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(), "NAME_UNKNOWN")
		return
	}

	writeJSON(w, stdhttp.StatusOK, result)
}

func (r *Router) handleUploadStart(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	state, err := r.service.BeginUpload(req.Context(), repository)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(), "BLOB_UPLOAD_UNKNOWN")
		return
	}

	setUploadHeaders(w.Header(), repository, state.ID, state.Size)
	w.WriteHeader(stdhttp.StatusAccepted)
}

func (r *Router) handleUploadState(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string, uploadID string) {
	switch req.Method {
	case stdhttp.MethodGet, stdhttp.MethodHead:
		state, err := r.service.UploadStatus(req.Context(), repository, uploadID)
		if err != nil {
			writeError(w, req, err, r.service.Challenge(), "BLOB_UPLOAD_UNKNOWN")
			return
		}

		setUploadHeaders(w.Header(), repository, state.ID, state.Size)
		w.WriteHeader(stdhttp.StatusNoContent)
	case stdhttp.MethodPatch:
		defer req.Body.Close()
		state, err := r.service.AppendUpload(req.Context(), repository, uploadID, req.Body)
		if err != nil {
			writeError(w, req, err, r.service.Challenge(), "BLOB_UPLOAD_UNKNOWN")
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
			writeError(w, req, err, r.service.Challenge(), "BLOB_UPLOAD_INVALID")
			return
		}

		w.Header().Set("Location", blobLocation(repository, blob.Digest))
		w.Header().Set("Docker-Content-Digest", blob.Digest)
		w.WriteHeader(stdhttp.StatusCreated)
	case stdhttp.MethodDelete:
		writeError(w, req, domain.NewValidationError("upload cancellation is not implemented in this slice"), r.service.Challenge(), "UNSUPPORTED")
	default:
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodPatch, stdhttp.MethodPut, stdhttp.MethodDelete}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
	}
}

func (r *Router) handleBlobRead(w stdhttp.ResponseWriter, req *stdhttp.Request, repository string, digest string) {
	if req.Method != stdhttp.MethodGet && req.Method != stdhttp.MethodHead {
		w.Header().Set("Allow", strings.Join([]string{stdhttp.MethodGet, stdhttp.MethodHead}, ", "))
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	reader, descriptor, err := r.service.OpenBlob(req.Context(), repository, digest)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(), "BLOB_UNKNOWN")
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
	switch req.Method {
	case stdhttp.MethodPut:
		defer req.Body.Close()
		payload, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, req, err, r.service.Challenge(), "MANIFEST_INVALID")
			return
		}

		manifest, err := r.service.PublishManifest(req.Context(), repository, reference, req.Header.Get("Content-Type"), payload)
		if err != nil {
			writeError(w, req, err, r.service.Challenge(), "MANIFEST_INVALID")
			return
		}

		w.Header().Set("Location", manifestLocation(repository, reference))
		w.Header().Set("Docker-Content-Digest", manifest.Digest)
		w.WriteHeader(stdhttp.StatusCreated)
	case stdhttp.MethodGet, stdhttp.MethodHead:
		manifest, err := r.service.OpenManifest(req.Context(), repository, reference)
		if err != nil {
			writeError(w, req, err, r.service.Challenge(), "MANIFEST_UNKNOWN")
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
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}

	limit, after, err := parsePagination(req)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(), "NAME_INVALID")
		return
	}

	tags, err := r.service.Tags(req.Context(), repository, limit, after)
	if err != nil {
		writeError(w, req, err, r.service.Challenge(), "NAME_UNKNOWN")
		return
	}

	writeJSON(w, stdhttp.StatusOK, tags)
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

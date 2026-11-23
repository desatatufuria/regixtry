package regixtryhttp

import (
	stdhttp "net/http"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// handleAdminSecurity serves /admin/v1/security/...: the login failures the
// server recorded and the live backoff state. It is reached only after
// requireAdminPrincipal, and every service method checks global admin again.
func (r *Router) handleAdminSecurity(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal, resource string) {
	if r.security == nil {
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
		return
	}
	switch resource {
	case "login-failures":
		r.handleAdminLoginFailures(w, req, principal)
	case "login-summary":
		r.handleAdminLoginSummary(w, req, principal)
	case "login-backoff":
		r.handleAdminLoginBackoff(w, req, principal)
	case "login-backoff/reset":
		r.handleAdminLoginBackoffReset(w, req, principal)
	default:
		writeAdminError(w, domainauth.NewNotFoundError("route", req.URL.Path), ports.Challenge{})
	}
}

func requireGet(w stdhttp.ResponseWriter, req *stdhttp.Request) bool {
	if req.Method != stdhttp.MethodGet {
		w.Header().Set("Allow", stdhttp.MethodGet)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return false
	}
	return true
}

// handleAdminLoginFailures serves GET /admin/v1/security/login-failures:
// aggregated failures newest window first. Query: from, to (RFC 3339),
// account, ip, reason, limit (1..200, default 50), cursor (the opaque next of
// the previous page).
func (r *Router) handleAdminLoginFailures(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	if !requireGet(w, req) {
		return
	}
	query := req.URL.Query()
	filter := ports.LoginFailureFilter{Account: query.Get("account"), IP: query.Get("ip"), Reason: query.Get("reason")}
	for name, target := range map[string]*time.Time{"from": &filter.From, "to": &filter.To} {
		if !query.Has(name) {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, query.Get(name))
		if err != nil {
			writeAdminError(w, domainauth.NewValidationError(name+" must be an RFC 3339 time"), ports.Challenge{})
			return
		}
		*target = parsed.UTC()
	}
	limit := 0
	if query.Has("limit") {
		var err error
		if limit, err = parseAdminLimit(query.Get("limit")); err != nil {
			writeAdminError(w, err, ports.Challenge{})
			return
		}
	}
	page, err := r.security.ListAdminLoginFailures(req.Context(), principal, filter, limit, query.Get("cursor"))
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, page)
}

// handleAdminLoginSummary serves GET /admin/v1/security/login-summary.
func (r *Router) handleAdminLoginSummary(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	if !requireGet(w, req) {
		return
	}
	summary, err := r.security.AdminLoginSummary(req.Context(), principal)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, summary)
}

// handleAdminLoginBackoff serves GET /admin/v1/security/login-backoff. Whether
// trusted proxies are configured is a boolean: the CIDRs stay in the startup
// configuration.
func (r *Router) handleAdminLoginBackoff(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	if !requireGet(w, req) {
		return
	}
	state, err := r.security.AdminLoginBackoff(req.Context(), principal)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	state.TrustedProxiesConfigured = len(r.trustedProxies) > 0
	writeJSON(w, stdhttp.StatusOK, state)
}

// handleAdminLoginBackoffReset serves POST /admin/v1/security/login-backoff/reset
// with {"account":"alice"}: it clears the backoff counter of that one account
// (a login name, or the "hash:xxxxxxxx" label of an unknown username) and
// answers {"account":"alice","reset":true}. An account with no backoff state is
// a 404. Audited as the access event login.backoff.reset.
func (r *Router) handleAdminLoginBackoffReset(w stdhttp.ResponseWriter, req *stdhttp.Request, principal domainauth.Principal) {
	if req.Method != stdhttp.MethodPost {
		w.Header().Set("Allow", stdhttp.MethodPost)
		w.WriteHeader(stdhttp.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Account string `json:"account"`
	}
	if err := decodeAdminJSON(req, &payload); err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	label, err := r.security.ResetAdminLoginBackoff(req.Context(), principal, payload.Account)
	if err != nil {
		writeAdminError(w, err, ports.Challenge{})
		return
	}
	writeJSON(w, stdhttp.StatusOK, map[string]any{"account": label, "reset": true})
}

package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"regixtry/internal/ports"
)

func TestRouterAdminTTLSecondsOutOfRangeIsRejectedWithoutCreating(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	owner, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "overflow-owner", Password: "password123", Enabled: true, IsAdmin: true,
	})
	if err != nil {
		t.Fatalf("CreateAdminUser() error = %v", err)
	}

	post := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	counts := func() (tokens, robots int) {
		toks, err := authService.ListAdminTokens(context.Background(), adminActor, owner.ID)
		if err != nil {
			t.Fatalf("ListAdminTokens() error = %v", err)
		}
		bots, err := authService.ListRobots(context.Background(), adminActor)
		if err != nil {
			t.Fatalf("ListRobots() error = %v", err)
		}
		return len(toks), len(bots)
	}

	for i, ttl := range []string{"63072001", "9223372037", "9223372036854775807", "4611686018427387904"} {
		beforeTokens, beforeRobots := counts()
		suffix := strconv.Itoa(i)

		rec := post("/admin/v1/users/"+owner.ID+"/admin-tokens", `{"name":"big-`+suffix+`","ttl_seconds":`+ttl+`}`)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "2 years") {
			t.Errorf("user token ttl_seconds=%s: status = %d body = %s, want 422 naming 2 years", ttl, rec.Code, rec.Body.String())
		}
		rec = post("/admin/v1/robots", `{"name":"big-`+suffix+`","repository":"team/app","role":"repo-writer","ttl_seconds":`+ttl+`}`)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "2 years") {
			t.Errorf("robot ttl_seconds=%s: status = %d body = %s, want 422 naming 2 years", ttl, rec.Code, rec.Body.String())
		}

		afterTokens, afterRobots := counts()
		if afterTokens != beforeTokens || afterRobots != beforeRobots {
			t.Errorf("ttl_seconds=%s created something: tokens %d->%d robots %d->%d", ttl, beforeTokens, afterTokens, beforeRobots, afterRobots)
		}
	}

	// Negative values keep their pre-existing handler rejection.
	for path, body := range map[string]string{
		"/admin/v1/users/" + owner.ID + "/admin-tokens": `{"name":"neg","ttl_seconds":-1}`,
		"/admin/v1/robots": `{"name":"neg","repository":"team/app","role":"repo-writer","ttl_seconds":-1}`,
	} {
		rec := post(path, body)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "ttl_seconds must be zero or greater") {
			t.Errorf("%s negative: status = %d body = %s, want 422 zero-or-greater", path, rec.Code, rec.Body.String())
		}
	}
}

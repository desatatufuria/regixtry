package regixtryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

const (
	ttlOverMaxMessage = "admin token ttl must be 2 years or shorter"
	ttlGrammarMessage = "ttl must be a whole number followed by h, d or y, for example 12h, 30d or 2y"
	ttlBothMessage    = "send either ttl or ttl_seconds, not both"
)

func TestRouterAdminReadableTTL(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	owner, err := authService.CreateAdminUser(context.Background(), adminActor, ports.AdminCreateUserInput{
		Username: "readable-ttl-owner", Password: "fixture-pass-9x", Enabled: true, IsAdmin: true,
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

	tokenPath := "/admin/v1/users/" + owner.ID + "/admin-tokens"
	robotPath := "/admin/v1/robots"
	bodyFor := func(path, name, ttlFields string) string {
		if path == robotPath {
			return `{"name":"` + name + `","repository":"team/app","role":"repo-writer"` + ttlFields + `}`
		}
		return `{"name":"` + name + `"` + ttlFields + `}`
	}
	// expiry returns the token's lifetime as seen by the client: expires_at
	// minus created_at for tokens, and expires_at minus the request window for
	// robots (the robot projection carries no token creation time).
	expiryWithin := func(t *testing.T, path string, rec *httptest.ResponseRecorder, before, after time.Time, want time.Duration) {
		t.Helper()
		var resp struct {
			ExpiresAt time.Time `json:"expires_at"`
			Token     struct {
				CreatedAt time.Time `json:"created_at"`
			} `json:"token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
		}
		if path == tokenPath {
			if got := resp.ExpiresAt.Sub(resp.Token.CreatedAt); got != want {
				t.Errorf("expires_at - created_at = %s, want exactly %s", got, want)
			}
			return
		}
		lo, hi := before.Add(want).Add(-time.Second), after.Add(want).Add(time.Second)
		if resp.ExpiresAt.Before(lo) || resp.ExpiresAt.After(hi) {
			t.Errorf("expires_at = %s, want within [%s, %s]", resp.ExpiresAt, lo, hi)
		}
	}

	day := 24 * time.Hour
	accepted := []struct {
		name  string
		field string
		want  time.Duration
	}{
		{"12h", `,"ttl":"12h"`, 12 * time.Hour},
		{"30d", `,"ttl":"30d"`, 30 * day},
		{"1y", `,"ttl":"1y"`, 365 * day},
		{"2y", `,"ttl":"2y"`, 730 * day},
		{"730d", `,"ttl":"730d"`, 730 * day},
		{"17520h", `,"ttl":"17520h"`, 17520 * time.Hour},
		{"ttl_seconds alone", `,"ttl_seconds":3600`, time.Hour},
		{"ttl null with ttl_seconds", `,"ttl":null,"ttl_seconds":7200`, 2 * time.Hour},
		{"ttl_seconds null with ttl", `,"ttl":"2d","ttl_seconds":null`, 2 * day},
		{"neither", ``, 30 * day},
	}
	for _, path := range []string{tokenPath, robotPath} {
		for i, tt := range accepted {
			before := time.Now()
			name := "ok-" + strconv.Itoa(i)
			rec := post(path, bodyFor(path, name, tt.field))
			after := time.Now()
			if rec.Code != http.StatusCreated {
				t.Errorf("%s %s: status = %d body = %s, want 201", path, tt.name, rec.Code, rec.Body.String())
				continue
			}
			expiryWithin(t, path, rec, before, after, tt.want)
		}
	}

	rejected := []struct {
		name    string
		field   string
		message string
	}{
		{"731d", `,"ttl":"731d"`, ttlOverMaxMessage},
		{"17521h", `,"ttl":"17521h"`, ttlOverMaxMessage},
		{"3y", `,"ttl":"3y"`, ttlOverMaxMessage},
		{"huge days", `,"ttl":"99999999999999999999d"`, ttlOverMaxMessage},
		{"max int64 hours", `,"ttl":"9223372036854775807h"`, ttlOverMaxMessage},
		{"max int64 days", `,"ttl":"9223372036854775807d"`, ttlOverMaxMessage},
		{"max int64 years", `,"ttl":"9223372036854775807y"`, ttlOverMaxMessage},
		{"empty", `,"ttl":""`, ttlGrammarMessage},
		{"no unit", `,"ttl":"30"`, ttlGrammarMessage},
		{"unit only", `,"ttl":"d"`, ttlGrammarMessage},
		{"seconds unit", `,"ttl":"30s"`, ttlGrammarMessage},
		{"minutes unit", `,"ttl":"30m"`, ttlGrammarMessage},
		{"decimal", `,"ttl":"1.5d"`, ttlGrammarMessage},
		{"negative", `,"ttl":"-1d"`, ttlGrammarMessage},
		{"plus sign", `,"ttl":"+1d"`, ttlGrammarMessage},
		{"zero", `,"ttl":"0d"`, ttlGrammarMessage},
		{"leading zero", `,"ttl":"012h"`, ttlGrammarMessage},
		{"leading space", `,"ttl":" 30d"`, ttlGrammarMessage},
		{"trailing space", `,"ttl":"30d "`, ttlGrammarMessage},
		{"inner space", `,"ttl":"30 d"`, ttlGrammarMessage},
		{"uppercase", `,"ttl":"30D"`, ttlGrammarMessage},
		{"compound", `,"ttl":"1d2h"`, ttlGrammarMessage},
		{"non-ascii digit", `,"ttl":"٣d"`, ttlGrammarMessage},
		{"json number", `,"ttl":30`, ttlGrammarMessage},
		{"json bool", `,"ttl":true`, ttlGrammarMessage},
		{"both fields", `,"ttl":"30d","ttl_seconds":3600`, ttlBothMessage},
		{"both fields with zero seconds", `,"ttl":"30d","ttl_seconds":0`, ttlBothMessage},
	}
	for _, path := range []string{tokenPath, robotPath} {
		for i, tt := range rejected {
			beforeTokens, beforeRobots := counts()
			rec := post(path, bodyFor(path, "bad-"+strconv.Itoa(i), tt.field))
			if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), tt.message) {
				t.Errorf("%s %s: status = %d body = %s, want 422 with %q", path, tt.name, rec.Code, rec.Body.String(), tt.message)
			}
			afterTokens, afterRobots := counts()
			if afterTokens != beforeTokens || afterRobots != beforeRobots {
				t.Errorf("%s %s created something: tokens %d->%d robots %d->%d", path, tt.name, beforeTokens, afterTokens, beforeRobots, afterRobots)
			}
		}
	}
}

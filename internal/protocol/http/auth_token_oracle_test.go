package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func TestAuthTokenEndpointDoesNotRevealUsernameState(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	for _, name := range []string{"active", "gone"} {
		if _, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: name, Password: "password123", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	gone, err := authService.ListAdminUsers(ctx, adminActor)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range gone {
		if u.Username == "gone" {
			if _, err := authService.DisableAdminUser(ctx, adminActor, u.ID); err != nil {
				t.Fatal(err)
			}
		}
	}

	attempt := func(user, pass string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/token?scope=repository:team/app:pull", nil)
		req.SetBasicAuth(user, pass)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	want := attempt("active", "wrong-password")
	if want.Code != http.StatusUnauthorized {
		t.Fatalf("baseline status = %d", want.Code)
	}
	cases := []struct{ name, user, pass string }{
		{"unknown user", "nobody", "wrong-password"},
		{"disabled user wrong password", "gone", "wrong-password"},
		{"disabled user correct password", "gone", "password123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := attempt(tc.user, tc.pass)
			if got.Code != want.Code || got.Body.String() != want.Body.String() {
				t.Fatalf("response differs from wrong-password for an active user:\n got %d %q\nwant %d %q", got.Code, got.Body.String(), want.Code, want.Body.String())
			}
			if got.Header().Get("WWW-Authenticate") != want.Header().Get("WWW-Authenticate") {
				t.Fatalf("challenge differs: %q vs %q", got.Header().Get("WWW-Authenticate"), want.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestAuthTokenEndpointCredentialTokensKeepWorkingAndDisabledOnesAreUniform(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, _, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	u, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ci", Password: "password123", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: u.ID, Name: "k", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	get := func(user, secret string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/auth/token", nil)
		req.SetBasicAuth(user, secret)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := get("ci", cred.Secret); rec.Code != http.StatusOK {
		t.Fatalf("credential token exchange status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := authService.DisableAdminUser(ctx, adminActor, u.ID); err != nil {
		t.Fatal(err)
	}
	rec := get("ci", cred.Secret)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("disabled user with a valid credential token: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

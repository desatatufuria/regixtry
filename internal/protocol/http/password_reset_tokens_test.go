package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func TestAdminPasswordResetClosesLoginSessionsButKeepsCredentialTokens(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	victim, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "victim", Password: "password123", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	bystander, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "bystander", Password: "password123", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: victim.ID, Name: "ci", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	bystanderCredential, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: bystander.ID, Name: "ci", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	access, err := authService.LoginWithPassword(ctx, "victim", "password123", nil)
	if err != nil {
		t.Fatal(err)
	}
	scoped, _ := domainauth.ParseScopes([]string{"repository:team/app:pull"})
	accessScoped, err := authService.LoginWithPassword(ctx, "victim", "password123", scoped)
	if err != nil {
		t.Fatal(err)
	}
	bystanderAccess, err := authService.LoginWithPassword(ctx, "bystander", "password123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rec := adminGet(handler, "/admin/v1/users", access.BearerToken); rec.Code != http.StatusOK {
		t.Fatalf("precondition: victim access token status = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+victim.ID+":reset-password", strings.NewReader(`{"new_password":"password456"}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("reset status = %d body=%s", rec.Code, rec.Body.String())
	}

	for name, bearer := range map[string]string{"access token": access.BearerToken, "scoped access token": accessScoped.BearerToken} {
		if got := adminGet(handler, "/admin/v1/users", bearer).Code; got != http.StatusUnauthorized {
			t.Errorf("%s after reset: status = %d, want 401", name, got)
		}
	}
	// The credential token (PAT-like) survives and mints a fresh session.
	fresh, err := authService.LoginWithPreissuedToken(ctx, "victim", credential.Secret, nil)
	if err != nil {
		t.Fatalf("admin credential token must still exchange after the reset: %v", err)
	}
	if got := adminGet(handler, "/admin/v1/users", fresh.BearerToken).Code; got != http.StatusOK {
		t.Errorf("fresh access token from the credential: status = %d, want 200", got)
	}
	if _, err := authService.LoginWithPassword(ctx, "victim", "password456", nil); err != nil {
		t.Errorf("new password must work: %v", err)
	}

	// Other users are untouched.
	if got := adminGet(handler, "/admin/v1/users", bystanderAccess.BearerToken).Code; got != http.StatusOK {
		t.Errorf("bystander access token status = %d, want 200", got)
	}
	if _, err := authService.LoginWithPreissuedToken(ctx, "bystander", bystanderCredential.Secret, nil); err != nil {
		t.Errorf("bystander credential must still work: %v", err)
	}

	// The audit event is still recorded.
	events, err := authService.ListAdminAccessEvents(ctx, adminActor, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if string(e.Action) == string(domainauth.AccessActionUserPasswordReset) {
			found = true
		}
	}
	if !found {
		t.Error("password reset audit event missing")
	}
}

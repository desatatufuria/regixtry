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

func resetPassword(handler http.Handler, adminToken, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/admin/v1/users/"+userID+":reset-password", strings.NewReader(`{"new_password":"password456"}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func countResetEvents(t *testing.T, svc ports.AdminHTTPService, actor domainauth.Principal) int {
	t.Helper()
	events, err := svc.ListAdminAccessEvents(context.Background(), actor, 200)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range events {
		if string(e.Action) == string(domainauth.AccessActionUserPasswordReset) {
			n++
		}
	}
	return n
}

func TestAdminPasswordResetRejectsRobotsAndKeepsSentinel(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	robot, err := authService.CreateAdminRobot(ctx, adminActor, ports.AdminCreateRobotInput{Name: "ci-bot", Repository: "team/app", Role: domainauth.RepoRoleWriter})
	if err != nil {
		t.Fatalf("CreateAdminRobot() error = %v", err)
	}
	before := countResetEvents(t, authService, adminActor)

	rec := resetPassword(handler, adminToken, robot.Robot.ID)
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("robot reset status = %d, want a 4xx; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "robot") {
		t.Fatalf("body = %s, want a message naming the robot", rec.Body.String())
	}

	if got := countResetEvents(t, authService, adminActor); got != before {
		t.Fatalf("password_reset events = %d, want %d (a rejected reset records nothing)", got, before)
	}
}

func TestAdminPasswordResetOfAHumanKeepsTheirCredentialTokensAndClosesAccessTokens(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	human, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "human", Password: "password123", Enabled: true, IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := authService.CreateAdminUserToken(ctx, adminActor, ports.AdminCreateTokenInput{UserID: human.ID, Name: "ci", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	access, err := authService.LoginWithPassword(ctx, "human", "password123", nil)
	if err != nil {
		t.Fatal(err)
	}
	before := countResetEvents(t, authService, adminActor)

	if rec := resetPassword(handler, adminToken, human.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("human reset status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Login sessions close; PAT-like credential tokens survive.
	if got := adminGet(handler, "/admin/v1/users", access.BearerToken).Code; got != http.StatusUnauthorized {
		t.Errorf("existing access token after reset: status = %d, want 401", got)
	}
	if _, err := authService.LoginWithPreissuedToken(ctx, "human", cred.Secret, nil); err != nil {
		t.Errorf("admin credential token after reset: %v", err)
	}
	if _, err := authService.LoginWithPassword(ctx, "human", "password123", nil); err == nil {
		t.Error("old password must stop working")
	}
	if _, err := authService.LoginWithPassword(ctx, "human", "password456", nil); err != nil {
		t.Errorf("new password must work: %v", err)
	}
	if got := countResetEvents(t, authService, adminActor); got != before+1 {
		t.Errorf("password_reset events = %d, want %d", got, before+1)
	}
}

func TestAdminResettingYourOwnPasswordClosesYourOwnAccessToken(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()

	if rec := resetPassword(handler, adminToken, adminActor.UserID); rec.Code != http.StatusNoContent {
		t.Fatalf("self reset status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := adminGet(handler, "/admin/v1/users", adminToken).Code; got != http.StatusUnauthorized {
		t.Fatalf("own access token after own reset: status = %d, want 401", got)
	}
	if _, err := authService.LoginWithPassword(context.Background(), "admin", "password456", nil); err != nil {
		t.Fatalf("logging in with the new password: %v", err)
	}
}

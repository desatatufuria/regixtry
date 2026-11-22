package regixtryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

func adminGet(handler http.Handler, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAdminAPIRejectsScopeLimitedAccessTokens(t *testing.T) {
	t.Parallel()

	handler, authService, adminActor, adminToken, cleanup := newTestRouterWithRealAuth(t)
	defer cleanup()
	ctx := context.Background()

	scoped, err := domainauth.ParseScopes([]string{"repository:team/app:pull"})
	if err != nil {
		t.Fatal(err)
	}
	adminScoped, err := authService.LoginWithPassword(ctx, "admin", "password123", scoped)
	if err != nil {
		t.Fatal(err)
	}

	// A repo-admin whose requested scope intersects to nothing must not end up
	// with an unrestricted token either.
	owner, err := authService.CreateAdminUser(ctx, adminActor, ports.AdminCreateUserInput{Username: "ra", Password: "password123", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.PutAdminRepositoryGrant(ctx, adminActor, ports.AdminPutRepositoryGrantInput{Username: owner.Username, Repository: "team/app", Role: domainauth.RepoRoleAdmin}); err != nil {
		t.Fatal(err)
	}
	other, _ := domainauth.ParseScopes([]string{"repository:other/none:pull"})
	raEmpty, err := authService.LoginWithPassword(ctx, "ra", "password123", other)
	if err != nil {
		t.Fatal(err)
	}
	raFull, err := authService.LoginWithPassword(ctx, "ra", "password123", nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path, bearer string
		want               int
	}{
		{"unscoped admin token keeps working", "/admin/v1/users", adminToken, http.StatusOK},
		{"scope-limited admin token is refused on admin routes", "/admin/v1/users", adminScoped.BearerToken, http.StatusForbidden},
		{"scope-limited admin token is refused on repository routes", "/admin/v1/repositories/team/app/grants", adminScoped.BearerToken, http.StatusForbidden},
		{"repo-admin unscoped token keeps working on its repository", "/admin/v1/repositories/team/app/grants", raFull.BearerToken, http.StatusOK},
		{"repo-admin token whose scope intersected to nothing is refused", "/admin/v1/repositories/team/app/grants", raEmpty.BearerToken, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := adminGet(handler, tc.path, tc.bearer)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

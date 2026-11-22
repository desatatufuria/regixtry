package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	appauth "regixtry/internal/app/auth"
	appregixtry "regixtry/internal/app/regixtry"
	domainauth "regixtry/internal/domain/auth"
	authpostgres "regixtry/internal/infra/auth/postgres"
	metadata "regixtry/internal/infra/metadata/sqlite"
	"regixtry/internal/infra/storage/fsblob"
	"regixtry/internal/ports"
	regixtryhttp "regixtry/internal/protocol/http"
)

// TestDeletingARobotOverTheRealWire drives x then y through the real HTTP
// router and SQLite stores: the robot, its grant and its tokens are gone, the
// other robot is untouched, a human cannot be deleted, a non-admin cannot
// delete, and the delete leaves a robot.deleted event.
func TestDeletingARobotOverTheRealWire(t *testing.T) {
	dir := t.TempDir()
	blob, err := fsblob.New(filepath.Join(dir, "c"))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := metadata.New(filepath.Join(dir, "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer meta.Close()
	authStore, err := authpostgres.NewWithDriver("sqlite", filepath.Join(dir, "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer authStore.Close()
	access := ports.NewPrincipalAccessController(ports.Challenge{Realm: "http://127.0.0.1:5000/auth/token", Service: "regixtry"})
	authService := appauth.NewService(authStore)
	service := appregixtry.NewService(blob, meta, access, ports.NewSingleTenantResolver("tenant-a"), ports.NewInlineJobRunner())
	router := regixtryhttp.NewRouter(service, authService)
	ctx := context.Background()

	boot, err := authService.BootstrapAdmin(ctx, ports.BootstrapAdminInput{Username: "admin", Password: "fixture-pass-9x"})
	if err != nil {
		t.Fatal(err)
	}
	login, err := authService.LoginWithPassword(ctx, "admin", "fixture-pass-9x", nil)
	if err != nil {
		t.Fatal(err)
	}
	actor := domainauth.Principal{UserID: boot.User.ID, Username: "admin", IsAdmin: true}
	var robotIDs []string
	for _, name := range []string{"alpha", "beta"} {
		created, err := authService.CreateAdminRobot(ctx, actor, ports.AdminCreateRobotInput{Name: name, Repository: "team/api", Role: domainauth.RepoRoleWriter, TTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := authService.CreateAdminUserToken(ctx, actor, ports.AdminCreateTokenInput{UserID: created.Robot.ID, Name: "extra", TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
		robotIDs = append(robotIDs, created.Robot.ID)
	}
	human, err := authService.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: "bob", Password: "fixture-pass-9x", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.CreateAdminUser(ctx, actor, ports.AdminCreateUserInput{Username: "carol", Password: "fixture-pass-9x", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	defer server.Close()
	client, err := NewHTTPAdminClient(server.URL, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	session := AdminSession{Username: "admin", BearerToken: login.BearerToken, ExpiresAt: time.Now().Add(time.Hour)}

	m := newAdminReadyModel(t, client)
	m = resizeModel(t, m, 150, 24)
	m.adminSession, m.adminAuth = session, adminAuthStateAuthenticated
	updated, cmd := m.Update(navigateMsg{To: screenAdminRobots})
	m = repoAccessRun(t, updated.(Model), cmd)
	selected, ok := m.adminScreens[slotRobots].(robotsScreen).selectedRobot()
	if !ok {
		t.Fatal("no robot selected")
	}
	m = repoAccessPress(t, m, testKeyRunes("x"))
	mustContain(t, accessModelText(m), "Delete robot "+selected.Username)
	m = repoAccessPress(t, m, testKeyRunes("y"))
	mustContain(t, accessModelText(m), "Robot "+selected.Username+" deleted.")

	if _, err := authStore.GetUserByID(ctx, selected.ID); err == nil {
		t.Fatal("the robot still exists")
	}
	grants, _ := authStore.ListRepoGrants(ctx, selected.ID)
	tokens, _ := authStore.ListTokensByUser(ctx, selected.ID, domainauth.TokenKindAdminCredential)
	if len(grants) != 0 || len(tokens) != 0 {
		t.Fatalf("left behind: %d grants, %d tokens", len(grants), len(tokens))
	}
	other := robotIDs[0]
	if selected.ID == other {
		other = robotIDs[1]
	}
	grants, _ = authStore.ListRepoGrants(ctx, other)
	tokens, _ = authStore.ListTokensByUser(ctx, other, domainauth.TokenKindAdminCredential)
	if len(grants) != 1 || len(tokens) != 2 {
		t.Fatalf("the other robot lost data: %d grants, %d tokens", len(grants), len(tokens))
	}

	events, err := authStore.ListAccessEvents(ctx, 50)
	if err != nil {
		t.Fatalf("list access events: %v", err)
	}
	deleted := 0
	for _, event := range events {
		if string(event.Action) == "robot.deleted" {
			deleted++
		}
	}
	if deleted != 1 {
		t.Fatalf("robot.deleted events = %d, want 1", deleted)
	}

	if err := client.DeleteRobot(ctx, session, human.ID); err == nil {
		t.Fatal("a human user was accepted as a robot")
	}
	if _, err := authStore.GetUserByID(ctx, human.ID); err != nil {
		t.Fatal("a human user was deleted")
	}
	carol, err := authService.LoginWithPassword(ctx, "carol", "fixture-pass-9x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteRobot(ctx, AdminSession{Username: "carol", BearerToken: carol.BearerToken}, other); err == nil {
		t.Fatal("a non-admin deleted a robot")
	}
	if _, err := authStore.GetUserByID(ctx, other); err != nil {
		t.Fatal("the robot was deleted by a non-admin")
	}
}

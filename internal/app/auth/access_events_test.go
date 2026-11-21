package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
	"regixtry/internal/ports"
)

type auditFixture struct {
	store   *memoryAuthStore
	service *Service
	now     time.Time
	admin   domainauth.Principal
	alice   domainauth.User
	repo    regixtrydomain.RepositoryRef
}

func newAuditFixture(t *testing.T) auditFixture {
	t.Helper()

	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	store := newMemoryAuthStore()
	service := NewService(store)
	service.now = func() time.Time { return now }
	alice := domainauth.User{ID: "u-alice", Username: "alice", PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}
	_ = store.UpsertUser(context.Background(), alice)

	return auditFixture{
		store:   store,
		service: service,
		now:     now,
		admin:   domainauth.Principal{UserID: "u-admin", Username: "admin", IsAdmin: true},
		alice:   alice,
		repo:    regixtrydomain.MustParseRepositoryRef("team/app"),
	}
}

func (f auditFixture) only(t *testing.T) domainauth.AccessEvent {
	t.Helper()
	if len(f.store.events) != 1 {
		t.Fatalf("events = %#v, want exactly one", f.store.events)
	}
	return f.store.events[0]
}

func (f auditFixture) none(t *testing.T) {
	t.Helper()
	if len(f.store.events) != 0 {
		t.Fatalf("events = %#v, want none", f.store.events)
	}
}

// missingGrantStore reports every grant delete as not found, like the real
// store does for a grant that does not exist.
type missingGrantStore struct{ *memoryAuthStore }

func (missingGrantStore) DeleteRepoGrant(_ context.Context, _ string, repository regixtrydomain.RepositoryRef) error {
	return domainauth.NewNotFoundError("grant", repository.String())
}

func TestPutRepoGrantRecordsNewThenChangedThenSkipsNoOp(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleReader); err != nil {
		t.Fatal(err)
	}
	created := f.only(t)
	want := domainauth.AccessEvent{
		ID: created.ID, OccurredAt: f.now, Action: domainauth.AccessActionGrantPut,
		ActorUserID: "u-admin", ActorUsername: "admin",
		SubjectUserID: "u-alice", SubjectUsername: "alice",
		Repository: "team/app", Role: "repo-reader", Detail: "",
	}
	if created != want || created.ID == "" {
		t.Fatalf("new grant event = %#v, want %#v", created, want)
	}

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if len(f.store.events) != 2 {
		t.Fatalf("events = %d, want 2", len(f.store.events))
	}
	changed := f.store.events[1]
	if changed.Role != "repo-writer" || changed.Detail != "Reader to Writer" || changed.Action != domainauth.AccessActionGrantPut {
		t.Fatalf("changed grant event = %#v", changed)
	}
	if changed.ID == created.ID {
		t.Fatal("every event needs its own id")
	}

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if len(f.store.events) != 2 {
		t.Fatalf("re-putting the same role must record nothing, events = %d", len(f.store.events))
	}
}

func TestFailedGrantMutationsRecordNothing(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()
	plain := domainauth.Principal{UserID: "u-bob", Username: "bob"}

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", "owner"); err == nil {
		t.Error("invalid role must fail")
	}
	if _, err := f.service.PutRepoGrant(ctx, f.admin, "nobody", "team/app", domainauth.RepoRoleReader); err == nil {
		t.Error("unknown user must fail")
	}
	if _, err := f.service.PutRepoGrant(ctx, plain, f.alice.ID, "team/app", domainauth.RepoRoleReader); err == nil {
		t.Error("forbidden actor must fail")
	}
	missing := NewService(missingGrantStore{f.store})
	missing.now = f.service.now
	if err := missing.DeleteRepoGrant(ctx, f.admin, f.alice.ID, "team/app"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Errorf("deleting a missing grant error = %v, want not found", err)
	}
	if err := missing.DeleteRepositoryGrant(ctx, f.admin, "team/app", "alice"); !domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		t.Errorf("delegate-path delete of a missing grant error = %v, want not found", err)
	}
	if err := f.service.DeleteRepoGrant(ctx, plain, f.alice.ID, "team/app"); err == nil {
		t.Error("forbidden delete must fail")
	}
	if _, err := f.service.PutRepositoryGrant(ctx, plain, "team/app", "alice", domainauth.RepoRoleReader); err == nil {
		t.Error("non-delegate must fail")
	}
	f.none(t)
}

func TestDeleteRepoGrantRecordsRevocation(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteRepoGrant(ctx, f.admin, f.alice.ID, "team/app"); err != nil {
		t.Fatal(err)
	}
	if len(f.store.events) != 2 {
		t.Fatalf("events = %d, want 2", len(f.store.events))
	}
	got := f.store.events[1]
	if got.Action != domainauth.AccessActionGrantDelete || got.SubjectUsername != "alice" || got.SubjectUserID != "u-alice" ||
		got.Repository != "team/app" || got.ActorUsername != "admin" || !got.OccurredAt.Equal(f.now) {
		t.Fatalf("revocation event = %#v", got)
	}
}

func TestDelegateGrantPathsRecordTheDelegateAsActor(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()
	delegate := domainauth.Principal{
		UserID: "u-dele", Username: "dele",
		Grants: []domainauth.RepoGrant{{UserID: "u-dele", Repository: f.repo, Role: domainauth.RepoRoleAdmin}},
	}

	if _, err := f.service.PutRepositoryGrant(ctx, delegate, "team/app", "Alice", domainauth.RepoRoleReader); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PutRepositoryGrant(ctx, delegate, "team/app", "alice", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if err := f.service.DeleteRepositoryGrant(ctx, delegate, "team/app", "alice"); err != nil {
		t.Fatal(err)
	}
	if len(f.store.events) != 3 {
		t.Fatalf("events = %#v, want 3", f.store.events)
	}
	for _, event := range f.store.events {
		if event.ActorUserID != "u-dele" || event.ActorUsername != "dele" || event.SubjectUsername != "alice" || event.Repository != "team/app" {
			t.Fatalf("delegate event = %#v", event)
		}
	}
	if e := f.store.events[0]; e.Action != domainauth.AccessActionGrantPut || e.Detail != "" || e.Role != "repo-reader" {
		t.Fatalf("new delegate grant = %#v", e)
	}
	if e := f.store.events[1]; e.Detail != "Reader to Writer" {
		t.Fatalf("changed delegate grant = %#v", e)
	}
	if e := f.store.events[2]; e.Action != domainauth.AccessActionGrantDelete {
		t.Fatalf("delegate revocation = %#v", e)
	}
}

func TestDelegateRefusedMutationsRecordNothing(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()
	delegate := domainauth.Principal{
		UserID: "u-dele", Username: "dele",
		Grants: []domainauth.RepoGrant{{UserID: "u-dele", Repository: f.repo, Role: domainauth.RepoRoleAdmin}},
	}
	if err := f.store.PutRepoGrant(ctx, domainauth.RepoGrant{UserID: f.alice.ID, Repository: f.repo, Role: domainauth.RepoRoleAdmin}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.PutRepositoryGrant(ctx, delegate, "team/app", "alice", domainauth.RepoRoleAdmin); err == nil {
		t.Error("delegate granting repo-admin must fail")
	}
	if _, err := f.service.PutRepositoryGrant(ctx, delegate, "team/app", "alice", domainauth.RepoRoleReader); err == nil {
		t.Error("delegate changing a peer admin must fail")
	}
	if err := f.service.DeleteRepositoryGrant(ctx, delegate, "team/app", "alice"); err == nil {
		t.Error("delegate revoking a peer admin must fail")
	}
	f.none(t)
}

func TestCreateAdminTokenRecordsIssuanceWithReadableLifetime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ttl  time.Duration
		want string
	}{
		{"default is 30 days", 0, "30 days"},
		{"hours", 12 * time.Hour, "12 hours"},
		{"one hour", time.Hour, "1 hour"},
		{"one day", 24 * time.Hour, "1 day"},
		{"days", 90 * 24 * time.Hour, "90 days"},
		{"two years", domainauth.MaxAdminTokenTTL, "2 years"},
		{"not a whole day stays in hours", 36 * time.Hour, "36 hours"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAuditFixture(t)

			created, err := f.service.CreateAdminToken(context.Background(), f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, Name: " deploy ", TTL: tt.ttl})
			if err != nil {
				t.Fatal(err)
			}
			got := f.only(t)
			if got.Action != domainauth.AccessActionTokenIssued || got.Detail != tt.want || got.TokenName != "deploy" ||
				got.SubjectUsername != "alice" || got.SubjectUserID != "u-alice" || got.ActorUsername != "admin" || !got.OccurredAt.Equal(f.now) {
				t.Fatalf("event = %#v, want detail %q", got, tt.want)
			}
			for _, secretish := range []string{created.Secret, created.Accessor, created.Token.SecretHash, created.Token.ID} {
				for _, value := range []string{got.ID, got.Detail, got.TokenName, got.Role, got.Repository, got.SubjectUserID} {
					if secretish != "" && value == secretish {
						t.Fatalf("event leaks %q: %#v", secretish, got)
					}
				}
			}
		})
	}
}

func TestFailedTokenIssuanceRecordsNothing(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	if _, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, TTL: domainauth.MaxAdminTokenTTL + time.Second}); err == nil {
		t.Error("over-max ttl must fail")
	}
	if _, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: "nobody"}); err == nil {
		t.Error("unknown user must fail")
	}
	if _, err := f.service.CreateAdminToken(ctx, domainauth.Principal{UserID: "u-bob"}, ports.CreateAdminTokenInput{UserID: f.alice.ID}); err == nil {
		t.Error("forbidden actor must fail")
	}
	disabled := f.alice
	disabled.Enabled = false
	_ = f.store.UpsertUser(ctx, disabled)
	if _, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID}); err == nil {
		t.Error("disabled user must fail")
	}
	f.none(t)
}

func TestCreateAdminUserTokenRecordsExactlyOneEvent(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)

	if _, err := f.service.CreateAdminUserToken(context.Background(), f.admin, ports.AdminCreateTokenInput{UserID: f.alice.ID, Name: "ci", TTL: 24 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if got := f.only(t); got.Detail != "1 day" || got.Action != domainauth.AccessActionTokenIssued {
		t.Fatalf("event = %#v", got)
	}
}

func TestRevokeAdminUserTokenRecordsRevocation(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	created, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	f.store.events = nil

	other := domainauth.User{ID: "u-other", Username: "other", Enabled: true}
	_ = f.store.UpsertUser(ctx, other)
	if err := f.service.RevokeAdminUserToken(ctx, f.admin, other.ID, created.Accessor); err == nil {
		t.Fatal("revoking through the wrong user must fail")
	}
	if err := f.service.RevokeAdminUserToken(ctx, f.admin, f.alice.ID, "missing"); err == nil {
		t.Fatal("unknown accessor must fail")
	}
	if err := f.service.RevokeAdminUserToken(ctx, domainauth.Principal{UserID: "u-bob"}, f.alice.ID, created.Accessor); err == nil {
		t.Fatal("forbidden actor must fail")
	}
	f.none(t)

	if err := f.service.RevokeAdminUserToken(ctx, f.admin, f.alice.ID, created.Accessor); err != nil {
		t.Fatal(err)
	}
	got := f.only(t)
	if got.Action != domainauth.AccessActionTokenRevoked || got.SubjectUsername != "alice" || got.SubjectUserID != "u-alice" ||
		got.TokenName != "ci" || got.ActorUsername != "admin" || got.Detail != "" || !got.OccurredAt.Equal(f.now) {
		t.Fatalf("event = %#v", got)
	}
	if got.TokenName == created.Accessor || got.Detail == created.Accessor {
		t.Fatal("the accessor must never reach the history")
	}
}

func TestRevokeAdminTokenByAccessorRecordsRevocation(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	created, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	f.store.events = nil

	if err := f.service.RevokeAdminToken(ctx, f.admin, "missing"); err == nil {
		t.Fatal("unknown accessor must fail")
	}
	f.none(t)

	if err := f.service.RevokeAdminToken(ctx, f.admin, " "+created.Accessor+" "); err != nil {
		t.Fatal(err)
	}
	got := f.only(t)
	if got.Action != domainauth.AccessActionTokenRevoked || got.SubjectUsername != "alice" || got.TokenName != "ci" {
		t.Fatalf("event = %#v", got)
	}
}

func TestCreateRobotRecordsExactlyOneEvent(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	created, err := f.service.CreateRobot(ctx, f.admin, ports.CreateRobotInput{Name: "CI-Bot", Repository: "team/app", Role: domainauth.RepoRoleWriter, TTL: 7 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	got := f.only(t)
	want := domainauth.AccessEvent{
		ID: got.ID, OccurredAt: f.now, Action: domainauth.AccessActionRobotCreated,
		ActorUserID: "u-admin", ActorUsername: "admin",
		SubjectUserID: created.User.ID, SubjectUsername: "ci-bot",
		Repository: "team/app", Role: "repo-writer", TokenName: "CI-Bot", Detail: "7 days",
	}
	if got != want {
		t.Fatalf("event = %#v, want %#v", got, want)
	}
}

func TestCreateRobotDefaultLifetimeAndFailuresRecordNothing(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	if _, err := f.service.CreateRobot(ctx, f.admin, ports.CreateRobotInput{Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleReader}); err != nil {
		t.Fatal(err)
	}
	if got := f.only(t); got.Detail != "30 days" {
		t.Fatalf("default lifetime detail = %q", got.Detail)
	}
	f.store.events = nil

	bad := []ports.CreateRobotInput{
		{Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleReader},                                                // duplicate name
		{Name: "x", Repository: "team/app", Role: "owner"},                                                                   // invalid role
		{Name: "y", Repository: "team/app", Role: domainauth.RepoRoleReader, TTL: domainauth.MaxAdminTokenTTL + time.Second}, // over max
		{Name: " ", Repository: "team/app", Role: domainauth.RepoRoleReader},                                                 // no name
	}
	for _, input := range bad {
		if _, err := f.service.CreateRobot(ctx, f.admin, input); err == nil {
			t.Errorf("CreateRobot(%#v) must fail", input)
		}
	}
	if _, err := f.service.CreateRobot(ctx, domainauth.Principal{UserID: "u-bob"}, ports.CreateRobotInput{Name: "z", Repository: "team/app", Role: domainauth.RepoRoleReader}); err == nil {
		t.Error("forbidden actor must fail")
	}
	f.none(t)
}

func TestRobotRolledBackAfterTokenFailureRecordsNothing(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	failing := &failingTokenStore{memoryAuthStore: f.store, err: errors.New("token store down")}
	service := NewService(failing)
	service.now = func() time.Time { return f.now }

	if _, err := service.CreateRobot(context.Background(), f.admin, ports.CreateRobotInput{Name: "ci", Repository: "team/app", Role: domainauth.RepoRoleReader}); err == nil {
		t.Fatal("CreateRobot must fail")
	}
	f.none(t)
}

func TestFailingAuditWriteNeverFailsTheMutation(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()
	f.store.appendEventErr = errors.New("audit table unavailable")

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleReader); err != nil {
		t.Fatalf("PutRepoGrant error = %v, want success", err)
	}
	if grants, _ := f.store.ListRepoGrants(ctx, f.alice.ID); len(grants) != 1 {
		t.Fatalf("grant must be applied, got %#v", grants)
	}
	created, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, Name: "ci"})
	if err != nil || created.Secret == "" {
		t.Fatalf("CreateAdminToken = %#v, %v; want success", created, err)
	}
	if err := f.service.RevokeAdminUserToken(ctx, f.admin, f.alice.ID, created.Accessor); err != nil {
		t.Fatalf("RevokeAdminUserToken error = %v", err)
	}
	if _, err := f.service.CreateRobot(ctx, f.admin, ports.CreateRobotInput{Name: "bot", Repository: "team/app", Role: domainauth.RepoRoleReader}); err != nil {
		t.Fatalf("CreateRobot error = %v", err)
	}
	if err := f.service.DeleteRepoGrant(ctx, f.admin, f.alice.ID, "team/app"); err != nil {
		t.Fatalf("DeleteRepoGrant error = %v", err)
	}
	if robots, _ := f.store.ListRobots(ctx); len(robots) != 1 {
		t.Fatalf("robot must stay created, got %d", len(robots))
	}
}

func TestListAdminAccessEvents(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)
	ctx := context.Background()

	if _, err := f.service.ListAdminAccessEvents(ctx, domainauth.Principal{UserID: "u-bob"}, 10); !domainauth.IsCode(err, domainauth.ErrorCodeForbidden) {
		t.Fatalf("non-admin error = %v, want forbidden", err)
	}
	empty, err := f.service.ListAdminAccessEvents(ctx, f.admin, 10)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, %v; want a non-nil empty slice", empty, err)
	}

	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleReader); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PutRepoGrant(ctx, f.admin, f.alice.ID, "team/app", domainauth.RepoRoleWriter); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateAdminToken(ctx, f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, Name: "ci", TTL: 12 * time.Hour}); err != nil {
		t.Fatal(err)
	}

	got, err := f.service.ListAdminAccessEvents(ctx, f.admin, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("got = %#v, %v; want 2 rows", got, err)
	}
	first := got[0]
	if first.Action != domainauth.AccessActionTokenIssued || first.Actor != "admin" || first.Subject != "alice" ||
		first.TokenName != "ci" || first.Detail != "12 hours" || !first.At.Equal(f.now) || first.ID == "" {
		t.Fatalf("first = %#v", first)
	}
	if second := got[1]; second.Action != domainauth.AccessActionGrantPut || second.Detail != "Reader to Writer" ||
		second.Repository != "team/app" || second.Role != "repo-writer" {
		t.Fatalf("second = %#v", second)
	}
	if strings.Contains(strings.ToLower(first.Actor+first.Subject), "u-") {
		t.Fatal("user ids must not leak into the projection")
	}
}

func TestRecordedEventsCapGiantFreeText(t *testing.T) {
	t.Parallel()
	f := newAuditFixture(t)

	giant := strings.Repeat("日", 1<<20)
	if _, err := f.service.CreateAdminToken(context.Background(), f.admin, ports.CreateAdminTokenInput{UserID: f.alice.ID, Name: giant}); err != nil {
		t.Fatalf("token creation rules must not change: %v", err)
	}
	got := f.only(t)
	if n := len([]rune(got.TokenName)); n != domainauth.AccessEventTextLimit || !strings.HasSuffix(got.TokenName, "…") {
		t.Fatalf("stored token name = %d runes", n)
	}
}

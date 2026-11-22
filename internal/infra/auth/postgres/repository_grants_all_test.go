package postgres

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
	regixtrydomain "regixtry/internal/domain/regixtry"
)

type grantSeed struct {
	id, username string
	robot        bool
	repo         string
	role         domainauth.RepoRole
}

func seedGrants(t *testing.T, store *Store, seeds []grantSeed) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for i, seed := range seeds {
		if !seen[seed.id] {
			seen[seed.id] = true
			user := domainauth.User{ID: seed.id, Username: seed.username, PasswordHash: "hash-" + seed.id, IsRobot: seed.robot, Enabled: seed.id != "u-disabled", CreatedAt: now, UpdatedAt: now}
			if err := store.UpsertUser(ctx, user); err != nil {
				t.Fatalf("UpsertUser(%s) error = %v", seed.id, err)
			}
		}
		grant := domainauth.RepoGrant{UserID: seed.id, Repository: regixtrydomain.MustParseRepositoryRef(seed.repo), Role: seed.role, CreatedAt: now.Add(time.Duration(i) * time.Minute), UpdatedAt: now.Add(time.Duration(i) * time.Hour)}
		if err := store.PutRepoGrant(ctx, grant); err != nil {
			t.Fatalf("PutRepoGrant() error = %v", err)
		}
	}
}

func rowKeys(rows []domainauth.RepositoryGrantRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Repository+"|"+row.Username+"|"+row.UserID)
	}
	return out
}

// walk reads every page of the given size and returns the reassembled rows.
func walk(t *testing.T, store *Store, size int) []domainauth.RepositoryGrantRow {
	t.Helper()
	var all []domainauth.RepositoryGrantRow
	var cursor *domainauth.GrantCursor
	for pages := 0; pages < 1000; pages++ {
		page, err := store.ListRepoGrantsPage(context.Background(), cursor, size)
		if err != nil {
			t.Fatalf("ListRepoGrantsPage() error = %v", err)
		}
		if len(page) == 0 {
			return all
		}
		if len(page) > size {
			t.Fatalf("page has %d rows, limit %d", len(page), size)
		}
		all = append(all, page...)
		next := domainauth.CursorFor(page[len(page)-1])
		cursor = &next
	}
	t.Fatal("pagination did not terminate")
	return nil
}

var allGrantSeeds = []grantSeed{
	{"u-ana", "ana", false, "team/web", domainauth.RepoRoleWriter},
	{"u-bob", "bob", false, "team/web", domainauth.RepoRoleReader},
	{"r-1", "ci-bot", true, "team/web", domainauth.RepoRoleReader},
	{"u-ana", "ana", false, "alpha/api", domainauth.RepoRoleAdmin},
	{"u-disabled", "dave", false, "never/pushed", domainauth.RepoRoleReader},
	{"u-upper", "zed", false, "team/web", domainauth.RepoRoleReader},
	{"u-under", "ana.x", false, "team/web", domainauth.RepoRoleReader},
	{"u-aaa", "ana2", false, "team/web", domainauth.RepoRoleReader},
}

func TestListRepoGrantsPageOrdersAcrossRepositoriesAndPrincipals(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	seedGrants(t, store, allGrantSeeds)

	got, err := store.ListRepoGrantsPage(context.Background(), nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"alpha/api|ana|u-ana",
		"never/pushed|dave|u-disabled",
		"team/web|ana|u-ana",
		"team/web|ana.x|u-under",
		"team/web|ana2|u-aaa",
		"team/web|bob|u-bob",
		"team/web|ci-bot|r-1",
		"team/web|zed|u-upper",
	}
	if !reflect.DeepEqual(rowKeys(got), want) {
		t.Fatalf("order =\n%v\nwant\n%v", rowKeys(got), want)
	}

	robot := got[6]
	if !robot.IsRobot || robot.Role != domainauth.RepoRoleReader || robot.UserID != "r-1" || robot.CreatedAt.IsZero() || robot.UpdatedAt.IsZero() || !robot.UpdatedAt.After(robot.CreatedAt) {
		t.Fatalf("robot row = %#v", robot)
	}
	if got[0].IsRobot {
		t.Fatalf("user row flagged robot: %#v", got[0])
	}
}

func TestListRepoGrantsPageReassemblesForEveryPageSize(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	seedGrants(t, store, allGrantSeeds)

	full, err := store.ListRepoGrantsPage(context.Background(), nil, 1000)
	if err != nil || len(full) != len(allGrantSeeds) {
		t.Fatalf("full = %d rows, %v; want %d", len(full), err, len(allGrantSeeds))
	}
	for _, size := range []int{1, 2, 3, 4, len(allGrantSeeds), len(allGrantSeeds) + 5} {
		t.Run(fmt.Sprintf("size %d", size), func(t *testing.T) {
			if got := walk(t, store, size); !reflect.DeepEqual(rowKeys(got), rowKeys(full)) {
				t.Fatalf("reassembled =\n%v\nwant\n%v", rowKeys(got), rowKeys(full))
			}
		})
	}
}

func TestListRepoGrantsPageCursorSurvivesConcurrentChanges(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	seedGrants(t, store, allGrantSeeds)
	ctx := context.Background()

	first, err := store.ListRepoGrantsPage(ctx, nil, 3)
	if err != nil || len(first) != 3 {
		t.Fatalf("first = %d, %v", len(first), err)
	}
	cursor := domainauth.CursorFor(first[2])

	// The row the cursor points at disappears and a new one lands before it.
	if err := store.DeleteRepoGrant(ctx, first[2].UserID, regixtrydomain.MustParseRepositoryRef(first[2].Repository)); err != nil {
		t.Fatal(err)
	}
	seedGrants(t, store, []grantSeed{{"u-new", "aaron", false, "alpha/api", domainauth.RepoRoleReader}})

	rest, err := store.ListRepoGrantsPage(ctx, &cursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"team/web|ana.x|u-under",
		"team/web|ana2|u-aaa",
		"team/web|bob|u-bob",
		"team/web|ci-bot|r-1",
		"team/web|zed|u-upper",
	}
	// first[2] was team/web|ana|u-ana (now deleted); everything after it stays.
	if !reflect.DeepEqual(rowKeys(rest), want) {
		t.Fatalf("rest =\n%v\nwant\n%v", rowKeys(rest), want)
	}
}

func TestListRepoGrantsPageEdges(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()

	empty, err := store.ListRepoGrantsPage(ctx, nil, 10)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty = %#v, %v; want a non-nil empty slice", empty, err)
	}

	seedGrants(t, store, allGrantSeeds[:2])
	if got, err := store.ListRepoGrantsPage(ctx, nil, 0); err != nil || len(got) != 0 {
		t.Fatalf("limit 0 = %#v, %v", got, err)
	}
	if got, err := store.ListRepoGrantsPage(ctx, nil, 50); err != nil || len(got) != 2 {
		t.Fatalf("limit above total = %d rows, %v", len(got), err)
	}
	end := domainauth.GrantCursor{Repository: "team/web", Username: "bob", UserID: "u-bob"}
	if got, err := store.ListRepoGrantsPage(ctx, &end, 10); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("cursor at the end = %#v, %v; want a non-nil empty slice", got, err)
	}
	beyond := domainauth.GrantCursor{Repository: "zzz/none", Username: "x", UserID: "x"}
	if got, err := store.ListRepoGrantsPage(ctx, &beyond, 10); err != nil || len(got) != 0 {
		t.Fatalf("cursor beyond all = %#v, %v", got, err)
	}
}

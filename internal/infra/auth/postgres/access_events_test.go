package postgres

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	domainauth "regixtry/internal/domain/auth"
)

func accessEvent(id string, at time.Time) domainauth.AccessEvent {
	return domainauth.AccessEvent{
		ID:            id,
		OccurredAt:    at,
		Action:        domainauth.AccessActionGrantPut,
		ActorUserID:   "actor-id",
		ActorUsername: "actor",
	}
}

func eventIDs(events []domainauth.AccessEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.ID)
	}
	return out
}

func TestAppendAndListAccessEventsNewestFirst(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	// Inserted out of order. Inside one second the stored text sorts the whole
	// second ("...:00Z") after its fractions ("...:00.5Z"), so a plain text
	// ordering would be wrong here.
	for _, event := range []domainauth.AccessEvent{
		accessEvent("a-whole", base),
		accessEvent("b-quarter", base.Add(250*time.Millisecond)),
		accessEvent("c-next", base.Add(time.Second)),
		accessEvent("d-half", base.Add(500*time.Millisecond)),
		accessEvent("e-nano", base.Add(1)),
	} {
		if err := store.AppendAccessEvent(ctx, event); err != nil {
			t.Fatalf("AppendAccessEvent(%s) error = %v", event.ID, err)
		}
	}

	got, err := store.ListAccessEvents(ctx, 10)
	if err != nil {
		t.Fatalf("ListAccessEvents() error = %v", err)
	}
	want := []string{"c-next", "d-half", "b-quarter", "e-nano", "a-whole"}
	if !reflect.DeepEqual(eventIDs(got), want) {
		t.Fatalf("order = %v, want %v", eventIDs(got), want)
	}
}

func TestListAccessEventsTiesBreakByIDDescending(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 10, 0, 0, 5, time.UTC)

	for _, id := range []string{"id-1", "id-3", "id-2"} {
		if err := store.AppendAccessEvent(ctx, accessEvent(id, at)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ListAccessEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"id-3", "id-2", "id-1"}; !reflect.DeepEqual(eventIDs(got), want) {
		t.Fatalf("order = %v, want %v", eventIDs(got), want)
	}
}

func TestListAccessEventsLimitIsExactInsideASecond(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	for _, event := range []domainauth.AccessEvent{
		accessEvent("old", base.Add(-time.Second)),
		accessEvent("whole", base),
		accessEvent("tenth", base.Add(100*time.Millisecond)),
		accessEvent("ninety", base.Add(900*time.Millisecond)),
	} {
		if err := store.AppendAccessEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		limit int
		want  []string
	}{
		{1, []string{"ninety"}},
		{2, []string{"ninety", "tenth"}},
		{3, []string{"ninety", "tenth", "whole"}},
		{4, []string{"ninety", "tenth", "whole", "old"}},
		{50, []string{"ninety", "tenth", "whole", "old"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("limit %d", tt.limit), func(t *testing.T) {
			got, err := store.ListAccessEvents(ctx, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(eventIDs(got), tt.want) {
				t.Fatalf("ids = %v, want %v", eventIDs(got), tt.want)
			}
		})
	}
}

func TestListAccessEventsEmptyAndNonPositiveLimit(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()

	got, err := store.ListAccessEvents(ctx, 10)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list = %#v, %v; want a non-nil empty slice", got, err)
	}

	if err := store.AppendAccessEvent(ctx, accessEvent("x", time.Now())); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1} {
		got, err := store.ListAccessEvents(ctx, limit)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("limit %d = %#v, %v; want a non-nil empty slice", limit, got, err)
		}
	}
}

func TestAccessEventRoundTripKeepsEveryFieldAndNullables(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	at := time.Date(2026, 3, 1, 10, 0, 0, 123456789, time.UTC)

	full := domainauth.AccessEvent{
		ID: "full", OccurredAt: at, Action: domainauth.AccessActionGrantPut,
		ActorUserID: "u-admin", ActorUsername: "admin",
		SubjectUserID: "u-ana", SubjectUsername: "ana",
		Repository: "team/app", Role: "repo-writer", TokenName: "ci", Detail: "Reader to Writer",
	}
	sparse := domainauth.AccessEvent{
		ID: "sparse", OccurredAt: at.Add(-time.Hour), Action: domainauth.AccessActionTokenRevoked,
		ActorUserID: "u-admin", ActorUsername: "admin",
	}
	for _, event := range []domainauth.AccessEvent{full, sparse} {
		if err := store.AppendAccessEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	got, err := store.ListAccessEvents(ctx, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("list = %#v, %v", got, err)
	}
	if !reflect.DeepEqual(got[0], full) {
		t.Fatalf("full = %#v, want %#v", got[0], full)
	}
	if !reflect.DeepEqual(got[1], sparse) {
		t.Fatalf("sparse = %#v, want %#v", got[1], sparse)
	}

	var nulls int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM auth_access_events
		WHERE id = 'sparse' AND subject_user_id IS NULL AND subject_username IS NULL AND repository IS NULL
		  AND role IS NULL AND token_name IS NULL AND detail IS NULL
	`).Scan(&nulls); err != nil || nulls != 1 {
		t.Fatalf("optional columns of a sparse event must be NULL, got count=%d err=%v", nulls, err)
	}
}

func TestAppendAccessEventRejectsInvalidAndDuplicate(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()

	bad := accessEvent("bad", time.Now())
	bad.Action = "nope"
	if err := store.AppendAccessEvent(ctx, bad); err == nil {
		t.Fatal("unknown action must be rejected")
	}

	good := accessEvent("dup", time.Now())
	if err := store.AppendAccessEvent(ctx, good); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAccessEvent(ctx, good); err == nil {
		t.Fatal("a duplicate id must be rejected")
	}
}

func TestAccessEventsTableHasNoSecretColumnsAndNoForeignKeys(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()

	rows, err := store.db.QueryContext(ctx, `SELECT name FROM pragma_table_info('auth_access_events') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	want := []string{"id", "occurred_at", "action", "actor_user_id", "actor_username", "subject_user_id", "subject_username", "repository", "role", "token_name", "detail"}
	if !reflect.DeepEqual(columns, want) {
		t.Fatalf("columns = %v, want %v", columns, want)
	}

	var foreignKeys int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pragma_foreign_key_list('auth_access_events')`).Scan(&foreignKeys); err != nil || foreignKeys != 0 {
		t.Fatalf("foreign keys = %d, err = %v; history must survive deleted users", foreignKeys, err)
	}
}

func TestAccessEventsSurviveDeletedUser(t *testing.T) {
	t.Parallel()

	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()

	user := domainauth.User{ID: "gone", Username: "gone", PasswordHash: "h", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := store.UpsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	event := accessEvent("keep", now)
	event.SubjectUserID, event.SubjectUsername = "gone", "gone"
	if err := store.AppendAccessEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteUser(ctx, "gone"); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListAccessEvents(ctx, 10)
	if err != nil || len(got) != 1 || got[0].SubjectUsername != "gone" {
		t.Fatalf("events after delete = %#v, %v", got, err)
	}
}

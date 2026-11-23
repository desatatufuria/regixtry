package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

var lfBase = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func lfRecord(window time.Time, account, ip string, count int64) ports.LoginFailureRecord {
	return ports.LoginFailureRecord{
		WindowStart: window, Endpoint: "/auth/token", Reason: "wrong password", Account: account, IP: ip,
		Count: count, FirstSeen: window.Add(10 * time.Second), LastSeen: window.Add(20 * time.Second),
	}
}

func lfAll(t *testing.T, store *Store) []ports.LoginFailureRecord {
	t.Helper()
	rows, err := store.ListLoginFailures(context.Background(), ports.LoginFailureFilter{}, nil, 1000)
	if err != nil {
		t.Fatalf("ListLoginFailures() error = %v", err)
	}
	return rows
}

func TestLoginFailuresMigrationIsAdditiveGuardedAndIdempotent(t *testing.T) {
	t.Parallel()
	var statements []string
	for _, statement := range migrationStatements() {
		if strings.Contains(statement, "auth_login_failures") {
			statements = append(statements, statement)
		}
	}
	if len(statements) != 1 || !strings.Contains(statements[0], "CREATE TABLE IF NOT EXISTS auth_login_failures") {
		t.Fatalf("statements = %q, want one guarded CREATE TABLE", statements)
	}
	for _, banned := range []string{"DROP ", "ALTER ", "REFERENCES", "FOREIGN KEY"} {
		if strings.Contains(strings.ToUpper(statements[0]), banned) {
			t.Fatalf("migration contains %q", banned)
		}
	}
	store := newSQLiteBackedStore(t)
	defer store.Close()
	for i := 0; i < 2; i++ {
		if err := bootstrapSchema(context.Background(), store.db); err != nil {
			t.Fatalf("bootstrapSchema() rerun %d error = %v", i, err)
		}
	}
}

func TestLoginFailuresMigrationUpgradesAnOldSchemaWithoutTouchingIt(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, statement := range migrationStatements() {
		if strings.Contains(statement, "auth_login_failures") {
			continue
		}
		if _, err := db.ExecContext(ctx, statement); err != nil && !isTolerableDuplicateColumnError(err) {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO auth_access_events (id, occurred_at, action, actor_user_id, actor_username) VALUES ('e1', '2026-03-01T12:00:00Z', 'grant.put', 'u', 'admin')`); err != nil {
		t.Fatal(err)
	}
	if err := bootstrapSchema(ctx, db); err != nil {
		t.Fatalf("upgrade error = %v", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_access_events`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("access events rows = %d err = %v, want the old row untouched", n, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_login_failures`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("new table rows = %d err = %v", n, err)
	}
}

func TestAddLoginFailuresUpsertsByAddingCountsAndWideningSeen(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()

	first := lfRecord(lfBase, "alice", "203.0.113.7", 3)
	second := lfRecord(lfBase, "alice", "203.0.113.7", 4)
	second.FirstSeen = lfBase.Add(5 * time.Second)
	second.LastSeen = lfBase.Add(2 * time.Minute)
	other := lfRecord(lfBase, "bob", "203.0.113.7", 1)
	if err := store.AddLoginFailures(ctx, []ports.LoginFailureRecord{first, other}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddLoginFailures(ctx, []ports.LoginFailureRecord{second}); err != nil {
		t.Fatal(err)
	}
	rows := lfAll(t, store)
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want one per key", rows)
	}
	var alice ports.LoginFailureRecord
	for _, r := range rows {
		if r.Account == "alice" {
			alice = r
		}
	}
	if alice.Count != 7 || !alice.FirstSeen.Equal(lfBase.Add(5*time.Second)) || !alice.LastSeen.Equal(lfBase.Add(2*time.Minute)) || !alice.WindowStart.Equal(lfBase) {
		t.Fatalf("alice = %+v", alice)
	}
}

func TestAddLoginFailuresIsAtomicPerCall(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	bad := lfRecord(lfBase, "bob", "203.0.113.7", 1)
	bad.Count = 0 // refused: a record must count at least one failure
	if err := store.AddLoginFailures(context.Background(), []ports.LoginFailureRecord{lfRecord(lfBase, "alice", "203.0.113.7", 1), bad}); err == nil {
		t.Fatal("AddLoginFailures() error = nil, want a validation error")
	}
	if rows := lfAll(t, store); len(rows) != 0 {
		t.Fatalf("rows = %+v, want none after a failed call", rows)
	}
}

func TestListLoginFailuresNewestFirstWithKeysetPagination(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	var records []ports.LoginFailureRecord
	for w := 0; w < 4; w++ {
		for i := 0; i < 3; i++ {
			records = append(records, lfRecord(lfBase.Add(time.Duration(w)*5*time.Minute), fmt.Sprintf("user%d", i), "203.0.113.7", 1))
		}
	}
	if err := store.AddLoginFailures(ctx, records); err != nil {
		t.Fatal(err)
	}

	var got []string
	var after *ports.LoginFailureCursor
	for page := 0; page < 10; page++ {
		rows, err := store.ListLoginFailures(ctx, ports.LoginFailureFilter{}, after, 5)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			got = append(got, r.WindowStart.Format("15:04")+"/"+r.Account)
		}
		if len(rows) < 5 {
			break
		}
		last := rows[len(rows)-1]
		after = &ports.LoginFailureCursor{WindowStart: last.WindowStart, Endpoint: last.Endpoint, Reason: last.Reason, Account: last.Account, IP: last.IP}
	}
	if len(got) != 12 {
		t.Fatalf("paged rows = %d (%v), want 12 with no repeat or gap", len(got), got)
	}
	seen := map[string]bool{}
	for _, g := range got {
		if seen[g] {
			t.Fatalf("duplicate %s in %v", g, got)
		}
		seen[g] = true
	}
	if !strings.HasPrefix(got[0], "12:15/") || !strings.HasPrefix(got[11], "12:00/") {
		t.Fatalf("order = %v, want newest window first", got)
	}
}

func TestListLoginFailuresFilters(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	hashed := lfRecord(lfBase, "hash:0a1b2c3d", "198.51.100.9", 2)
	hashed.Reason = "unknown user"
	records := []ports.LoginFailureRecord{
		lfRecord(lfBase, "alice", "203.0.113.7", 1),
		lfRecord(lfBase.Add(10*time.Minute), "alice", "198.51.100.9", 1),
		lfRecord(lfBase.Add(20*time.Minute), "bob", "203.0.113.7", 1),
		hashed,
	}
	if err := store.AddLoginFailures(ctx, records); err != nil {
		t.Fatal(err)
	}
	count := func(f ports.LoginFailureFilter) int {
		rows, err := store.ListLoginFailures(ctx, f, nil, 100)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	for name, tc := range map[string]struct {
		filter ports.LoginFailureFilter
		want   int
	}{
		"account":     {ports.LoginFailureFilter{Account: "alice"}, 2},
		"hash":        {ports.LoginFailureFilter{Account: "hash:0a1b2c3d"}, 1},
		"ip":          {ports.LoginFailureFilter{IP: "203.0.113.7"}, 2},
		"reason":      {ports.LoginFailureFilter{Reason: "unknown user"}, 1},
		"from":        {ports.LoginFailureFilter{From: lfBase.Add(15 * time.Minute)}, 1},
		"to":          {ports.LoginFailureFilter{To: lfBase.Add(5 * time.Minute)}, 2},
		"from and to": {ports.LoginFailureFilter{From: lfBase.Add(9 * time.Minute), To: lfBase.Add(11 * time.Minute)}, 1},
		"combined":    {ports.LoginFailureFilter{Account: "alice", IP: "198.51.100.9"}, 1},
		"none":        {ports.LoginFailureFilter{Account: "nobody"}, 0},
	} {
		if got := count(tc.filter); got != tc.want {
			t.Errorf("%s: rows = %d, want %d", name, got, tc.want)
		}
	}
}

func TestSummarizeLoginFailuresCountsDistinctAndIgnoresOverflowAndBearerBlanks(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	bearer := lfRecord(lfBase, "", "198.51.100.9", 2)
	overflow := ports.LoginFailureRecord{WindowStart: lfBase, Endpoint: "_overflow", Reason: "_overflow", Account: "_overflow", IP: "_overflow", Count: 5, FirstSeen: lfBase, LastSeen: lfBase.Add(time.Minute)}
	old := lfRecord(lfBase.Add(-time.Hour), "carol", "192.0.2.1", 9)
	if err := store.AddLoginFailures(ctx, []ports.LoginFailureRecord{
		lfRecord(lfBase, "alice", "203.0.113.7", 3), lfRecord(lfBase, "bob", "203.0.113.7", 1), bearer, overflow, old,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.SummarizeLoginFailures(ctx, lfBase.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	want := ports.LoginFailureSummary{Failures: 11, DistinctIPs: 2, DistinctAccounts: 2}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
	}
}

func TestPurgeLoginFailuresDeletesOldWindowsInBoundedBatches(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	ctx := context.Background()
	var records []ports.LoginFailureRecord
	for i := 0; i < 5; i++ {
		records = append(records, lfRecord(lfBase.Add(-time.Duration(40+i)*24*time.Hour), fmt.Sprintf("u%d", i), "203.0.113.7", 1))
	}
	records = append(records, lfRecord(lfBase, "fresh", "203.0.113.7", 1))
	if err := store.AddLoginFailures(ctx, records); err != nil {
		t.Fatal(err)
	}
	cutoff := lfBase.Add(-30 * 24 * time.Hour)
	n, err := store.PurgeLoginFailures(ctx, cutoff, 3)
	if err != nil || n != 3 {
		t.Fatalf("first purge = %d, %v; want 3 (the batch bound)", n, err)
	}
	n, err = store.PurgeLoginFailures(ctx, cutoff, 3)
	if err != nil || n != 2 {
		t.Fatalf("second purge = %d, %v; want 2", n, err)
	}
	if rows := lfAll(t, store); len(rows) != 1 || rows[0].Account != "fresh" {
		t.Fatalf("rows left = %+v, want only the fresh one", rows)
	}
}

func TestLoginFailureRowsNeverStoreAnUnknownRawName(t *testing.T) {
	t.Parallel()
	// The store keeps exactly what it is given; the guarantee is that callers
	// hand it hashes. This pins that nothing in the table is derived from
	// anything else (no extra column, no free text).
	store := newSQLiteBackedStore(t)
	defer store.Close()
	if err := store.AddLoginFailures(context.Background(), []ports.LoginFailureRecord{lfRecord(lfBase, "hash:0a1b2c3d", "203.0.113.7", 1)}); err != nil {
		t.Fatal(err)
	}
	var columns []string
	rows, err := store.db.Query(`SELECT name FROM pragma_table_info('auth_login_failures') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		columns = append(columns, name)
	}
	want := "window_start,endpoint,reason,account,ip,count,first_seen,last_seen"
	if strings.Join(columns, ",") != want {
		t.Fatalf("columns = %v, want %s", columns, want)
	}
}

func TestAddLoginFailuresFoldsNewKeysIntoOverflowWhenTheWindowIsAtCap(t *testing.T) {
	t.Parallel()
	store := newSQLiteBackedStore(t)
	defer store.Close()
	store.windowRowCap = 3
	ctx := context.Background()
	other := lfBase.Add(5 * time.Minute)

	if err := store.AddLoginFailures(ctx, []ports.LoginFailureRecord{
		lfRecord(lfBase, "a", "203.0.113.1", 1), lfRecord(lfBase, "b", "203.0.113.2", 1), lfRecord(lfBase, "c", "203.0.113.3", 1),
	}); err != nil {
		t.Fatal(err)
	}
	// At cap: an existing key still adds normally; new keys fold per window.
	if err := store.AddLoginFailures(ctx, []ports.LoginFailureRecord{
		lfRecord(lfBase, "a", "203.0.113.1", 4),
		lfRecord(lfBase, "d", "203.0.113.4", 2), lfRecord(lfBase, "e", "203.0.113.5", 3),
		lfRecord(other, "z", "203.0.113.9", 1), // another window has its own budget
	}); err != nil {
		t.Fatal(err)
	}
	// A later call also folds into the same overflow row.
	if err := store.AddLoginFailures(ctx, []ports.LoginFailureRecord{lfRecord(lfBase, "f", "203.0.113.6", 5)}); err != nil {
		t.Fatal(err)
	}

	got := map[string]int64{}
	var total int64
	for _, r := range lfAll(t, store) {
		got[r.WindowStart.Format("15:04")+"/"+r.Account] += r.Count
		total += r.Count
	}
	want := map[string]int64{"12:00/a": 5, "12:00/b": 1, "12:00/c": 1, "12:00/_overflow": 10, "12:05/z": 1}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("row %s = %d, want %d (all: %v)", k, got[k], v, got)
		}
	}
	if total != 18 {
		t.Fatalf("total = %d, want 18: nothing lost", total)
	}
	summary, err := store.SummarizeLoginFailures(ctx, lfBase.Add(-time.Minute))
	if err != nil || summary.Failures != 18 {
		t.Fatalf("summary = %+v, %v; want failures 18 including the overflow rows", summary, err)
	}
	if summary.DistinctAccounts != 4 || summary.DistinctIPs != 4 {
		t.Fatalf("summary = %+v; overflow must not count as an account or IP", summary)
	}
}

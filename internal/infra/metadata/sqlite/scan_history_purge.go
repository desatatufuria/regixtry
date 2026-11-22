package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// scanHistoryPurgeMarker names the schema_backfills row guarding
// purgeDuplicateScanHistory.
const scanHistoryPurgeMarker = "scan_runs_latest_only_purge_v1"

// scanHistoryPurgeBatchSize caps how many scan_runs rows one write
// transaction deletes, so the purge never holds the shared write queue long.
const scanHistoryPurgeBatchSize = 200

// purgeDuplicateScanHistory is the one-time migration behind the latest-only
// scan model: it deletes the duplicate scan history that older versions kept.
// Per (tenant, repository, digest) it keeps exactly one row:
//
//   - the newest COMPLETED run (its findings and db_freshness rows stay; the
//     deleted runs' rows go with them through ON DELETE CASCADE);
//   - else the newest FAILED run;
//   - else the newest SKIPPED row.
//
// Every other row of the digest is deleted EXCEPT in-flight (queued/running)
// runs, which are never touched. Skipped rows therefore only go when the
// digest has a completed or failed result. The keeper is chosen first and
// every delete is guarded on the keeper still existing, so a digest is never
// left without its newest completed run (the pull policy fails open without
// one). Secret-scan tables are out of scope.
//
// It is irreversible. It runs in batches of at most scanHistoryPurgeBatchSize
// deleted rows, each in its own beginWrite transaction (the FIFO write queue
// is released between batches, so pushes interleave). It is resumable: the
// marker is committed in the SAME transaction as the final batch, so a crash
// before it simply re-plans on the next open and finishes. A database with
// the marker returns after one point lookup.
//
// afterBatch is a test seam called after each committed batch.
func (s *Store) purgeDuplicateScanHistory(afterBatch func(batch int) error) error {
	var marker int
	err := s.db.QueryRow(`SELECT 1 FROM schema_backfills WHERE name = ?`, scanHistoryPurgeMarker).Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	ctx := context.Background()
	groups, err := s.duplicateScanGroups(ctx)
	if err != nil {
		return err
	}

	var pending []scanPurgePlan
	batch := 0
	flush := func(final bool) error {
		for {
			if !final && pendingScanPurgeCount(pending) < scanHistoryPurgeBatchSize {
				return nil
			}
			n, take := takeScanPurgeBatch(&pending, scanHistoryPurgeBatchSize)
			isFinal := final && len(pending) == 0
			if n == 0 && !isFinal {
				return nil
			}
			if err := s.commitScanPurgeBatch(ctx, take, isFinal); err != nil {
				return err
			}
			batch++
			if afterBatch != nil {
				if err := afterBatch(batch); err != nil {
					return err
				}
			}
			if isFinal {
				return nil
			}
		}
	}

	for _, group := range groups {
		plan, err := s.planScanGroupPurge(ctx, group)
		if err != nil {
			return err
		}
		if len(plan.ids) > 0 {
			pending = append(pending, plan)
		}
		if err := flush(false); err != nil {
			return err
		}
	}
	return flush(true)
}

type scanGroupKey struct{ tenant, repository, digest string }

type scanPurgePlan struct {
	tenant   string
	keeperID string
	ids      []string
}

func pendingScanPurgeCount(plans []scanPurgePlan) int {
	n := 0
	for _, p := range plans {
		n += len(p.ids)
	}
	return n
}

// takeScanPurgeBatch removes up to limit ids from the front of plans,
// splitting a plan when needed, and returns the taken plans.
func takeScanPurgeBatch(plans *[]scanPurgePlan, limit int) (int, []scanPurgePlan) {
	var taken []scanPurgePlan
	n := 0
	rest := *plans
	for len(rest) > 0 && n < limit {
		head := rest[0]
		room := limit - n
		if len(head.ids) <= room {
			taken = append(taken, head)
			n += len(head.ids)
			rest = rest[1:]
			continue
		}
		taken = append(taken, scanPurgePlan{tenant: head.tenant, keeperID: head.keeperID, ids: head.ids[:room]})
		rest[0] = scanPurgePlan{tenant: head.tenant, keeperID: head.keeperID, ids: head.ids[room:]}
		n += room
	}
	*plans = rest
	return n, taken
}

// duplicateScanGroups lists every digest that has more than one row; a digest
// with a single row has nothing to purge. Only the keys are held in memory.
func (s *Store) duplicateScanGroups(ctx context.Context) ([]scanGroupKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tenant, repository, digest FROM scan_runs
		GROUP BY tenant, repository, digest HAVING COUNT(*) > 1
		ORDER BY tenant, repository, digest
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []scanGroupKey
	for rows.Next() {
		var g scanGroupKey
		if err := rows.Scan(&g.tenant, &g.repository, &g.digest); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

type scanPurgeRow struct {
	id, status         string
	createdAt, updated time.Time
}

// scanInstant parses a stored timestamp. Rows are written as RFC3339Nano UTC
// with trailing fractional zeros trimmed, so raw strings do not sort by time
// ("...05Z" > "...05.5Z"); instants are compared instead. An unparseable
// value sorts oldest.
func scanInstant(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func newerScanRow(a, b scanPurgeRow) bool {
	if !a.createdAt.Equal(b.createdAt) {
		return a.createdAt.After(b.createdAt)
	}
	if !a.updated.Equal(b.updated) {
		return a.updated.After(b.updated)
	}
	return a.id > b.id
}

// planScanGroupPurge picks the keeper of one digest and lists the ids to
// delete. It only reads.
func (s *Store) planScanGroupPurge(ctx context.Context, group scanGroupKey) (scanPurgePlan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, created_at, updated_at FROM scan_runs
		WHERE tenant = ? AND repository = ? AND digest = ?
	`, group.tenant, group.repository, group.digest)
	if err != nil {
		return scanPurgePlan{}, err
	}
	defer rows.Close()

	var all []scanPurgeRow
	for rows.Next() {
		var r scanPurgeRow
		var created, updated string
		if err := rows.Scan(&r.id, &r.status, &created, &updated); err != nil {
			return scanPurgePlan{}, err
		}
		r.createdAt, r.updated = scanInstant(created), scanInstant(updated)
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return scanPurgePlan{}, err
	}

	var keeper *scanPurgeRow
	for _, class := range []string{"completed", "failed", "skipped"} {
		for i := range all {
			if all[i].status == class && (keeper == nil || newerScanRow(all[i], *keeper)) {
				keeper = &all[i]
			}
		}
		if keeper != nil {
			break
		}
	}
	if keeper == nil {
		return scanPurgePlan{}, nil
	}

	plan := scanPurgePlan{tenant: group.tenant, keeperID: keeper.id}
	for _, r := range all {
		if r.id == keeper.id || r.status == "queued" || r.status == "running" {
			continue
		}
		plan.ids = append(plan.ids, r.id)
	}
	return plan, nil
}

// commitScanPurgeBatch deletes the planned ids in one write transaction and,
// for the final batch, records the marker in that same transaction. Each
// delete is guarded on the keeper still existing and never removes in-flight
// runs, so a concurrent replacement can only make the purge delete less.
func (s *Store) commitScanPurgeBatch(ctx context.Context, plans []scanPurgePlan, final bool) error {
	tx, err := s.beginWrite(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// One prepared single-row delete: far cheaper than a large IN list.
	stmt, err := tx.PrepareContext(ctx, `
		DELETE FROM scan_runs
		WHERE tenant = ? AND id = ? AND status NOT IN ('queued', 'running')
		  AND EXISTS (SELECT 1 FROM scan_runs keeper WHERE keeper.tenant = ? AND keeper.id = ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, plan := range plans {
		for _, id := range plan.ids {
			if _, err = stmt.ExecContext(ctx, plan.tenant, id, plan.tenant, plan.keeperID); err != nil {
				return err
			}
		}
	}
	if final {
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO schema_backfills (name, completed_at) VALUES (?, ?)`, scanHistoryPurgeMarker, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

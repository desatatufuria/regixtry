package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

// loginFailureWindow mirrors the aggregator's bucket width: a window start is
// always a multiple of it, which lets a time filter use the primary key.
const loginFailureWindow = 5 * time.Minute

// loginFailureTimeLayout is fixed width, so the stored text sorts exactly like
// the instant it names (unlike RFC3339Nano with trimmed zeros).
const loginFailureTimeLayout = "2006-01-02T15:04:05.000000000Z"

func formatLoginFailureTime(value time.Time) string {
	return value.UTC().Format(loginFailureTimeLayout)
}

func parseLoginFailureTime(value string) time.Time {
	parsed, err := time.Parse(loginFailureTimeLayout, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// AddLoginFailures adds each record's count to its key in one transaction:
// either every record is written or none. first_seen only moves earlier and
// last_seen only later.
func (s *Store) AddLoginFailures(ctx context.Context, records []ports.LoginFailureRecord) error {
	if len(records) == 0 {
		return nil
	}
	for _, record := range records {
		if record.Count < 1 {
			return domainauth.NewValidationError("a login failure record must count at least one failure")
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, record := range records {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auth_login_failures (window_start, endpoint, reason, account, ip, count, first_seen, last_seen)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT(window_start, endpoint, reason, account, ip) DO UPDATE SET
				count = auth_login_failures.count + excluded.count,
				first_seen = CASE WHEN excluded.first_seen < auth_login_failures.first_seen THEN excluded.first_seen ELSE auth_login_failures.first_seen END,
				last_seen = CASE WHEN excluded.last_seen > auth_login_failures.last_seen THEN excluded.last_seen ELSE auth_login_failures.last_seen END
		`, formatLoginFailureTime(record.WindowStart), record.Endpoint, record.Reason, record.Account, record.IP, record.Count,
			formatLoginFailureTime(record.FirstSeen), formatLoginFailureTime(record.LastSeen)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// PurgeLoginFailures deletes at most limit rows whose window started before
// the cutoff, oldest first, and returns how many it deleted.
func (s *Store) PurgeLoginFailures(ctx context.Context, before time.Time, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM auth_login_failures
		WHERE (window_start, endpoint, reason, account, ip) IN (
			SELECT window_start, endpoint, reason, account, ip
			FROM auth_login_failures
			WHERE window_start < $1
			ORDER BY window_start
			LIMIT $2
		)
	`, formatLoginFailureTime(before.Truncate(loginFailureWindow)), limit)
	if err != nil {
		return 0, err
	}
	deleted, err := result.RowsAffected()
	return int(deleted), err
}

// ListLoginFailures returns rows newest window first, strictly after the
// cursor, at most limit. The keyset predicate and the ordering run in SQL over
// the primary key, so a page never reads the whole table; both use the same
// collation, so a cursor taken from one page is exact for the next.
func (s *Store) ListLoginFailures(ctx context.Context, filter ports.LoginFailureFilter, after *ports.LoginFailureCursor, limit int) ([]ports.LoginFailureRecord, error) {
	if limit <= 0 {
		return []ports.LoginFailureRecord{}, nil
	}

	var conditions []string
	var args []any
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if !filter.From.IsZero() {
		conditions = append(conditions,
			"window_start >= "+arg(formatLoginFailureTime(filter.From.Truncate(loginFailureWindow))),
			"last_seen >= "+arg(formatLoginFailureTime(filter.From)))
	}
	if !filter.To.IsZero() {
		conditions = append(conditions,
			"window_start <= "+arg(formatLoginFailureTime(filter.To)),
			"first_seen <= "+arg(formatLoginFailureTime(filter.To)))
	}
	if filter.Account != "" {
		conditions = append(conditions, "account = "+arg(filter.Account))
	}
	if filter.IP != "" {
		conditions = append(conditions, "ip = "+arg(filter.IP))
	}
	if filter.Reason != "" {
		conditions = append(conditions, "reason = "+arg(filter.Reason))
	}
	if after != nil {
		conditions = append(conditions, fmt.Sprintf("(window_start, endpoint, reason, account, ip) < (%s, %s, %s, %s, %s)",
			arg(formatLoginFailureTime(after.WindowStart)), arg(after.Endpoint), arg(after.Reason), arg(after.Account), arg(after.IP)))
	}
	query := `SELECT window_start, endpoint, reason, account, ip, count, first_seen, last_seen FROM auth_login_failures`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY window_start DESC, endpoint DESC, reason DESC, account DESC, ip DESC LIMIT " + arg(limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]ports.LoginFailureRecord, 0, min(limit, 64))
	for rows.Next() {
		var record ports.LoginFailureRecord
		var window, first, last string
		if err := rows.Scan(&window, &record.Endpoint, &record.Reason, &record.Account, &record.IP, &record.Count, &first, &last); err != nil {
			return nil, err
		}
		record.WindowStart = parseLoginFailureTime(window)
		record.FirstSeen = parseLoginFailureTime(first)
		record.LastSeen = parseLoginFailureTime(last)
		result = append(result, record)
	}
	return result, rows.Err()
}

// SummarizeLoginFailures totals the rows last seen at or after since. A row
// counts whole even if part of it predates since: rows are 5-minute buckets.
// Bearer failures (empty account) and the overflow key add to the total but
// not to the distinct counts.
func (s *Store) SummarizeLoginFailures(ctx context.Context, since time.Time) (ports.LoginFailureSummary, error) {
	var summary ports.LoginFailureSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT
			CAST(COALESCE(SUM(count), 0) AS BIGINT),
			COUNT(DISTINCT CASE WHEN ip NOT IN ('', '_overflow') THEN ip END),
			COUNT(DISTINCT CASE WHEN account NOT IN ('', '_overflow') THEN account END)
		FROM auth_login_failures
		WHERE window_start >= $1 AND last_seen >= $2
	`, formatLoginFailureTime(since.Truncate(loginFailureWindow)), formatLoginFailureTime(since)).
		Scan(&summary.Failures, &summary.DistinctIPs, &summary.DistinctAccounts)
	return summary, err
}

var _ ports.LoginFailureStore = (*Store)(nil)

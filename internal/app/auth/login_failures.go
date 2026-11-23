package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	domainauth "regixtry/internal/domain/auth"
	"regixtry/internal/ports"
)

const (
	loginFailuresMaxLimit     = 200
	loginFailuresDefaultLimit = 50
	loginFailuresCursorMax    = 1024
	loginSummaryWindow        = 15 * time.Minute
	loginBackoffListMax       = 100
	loginFlushBeforeReadTime  = 2 * time.Second
)

var _ ports.LoginSecurityAdmin = (*Service)(nil)

// SetLoginFailureAggregator lets the security endpoints flush the in-memory
// counters before they read, so a listing includes the failures of the last
// minute. Optional: without it they read what the store already holds.
func (s *Service) SetLoginFailureAggregator(aggregator *LoginFailureAggregator) {
	s.failureAggregator = aggregator
}

// failureStore returns the store's login failure support.
func (s *Service) failureStore() (ports.LoginFailureStore, error) {
	store, ok := s.store.(ports.LoginFailureStore)
	if !ok {
		return nil, domainauth.NewNotFoundError("login failures", "")
	}
	return store, nil
}

// flushBeforeRead is best effort: a store that is down fails the read anyway.
func (s *Service) flushBeforeRead(ctx context.Context) {
	if s.failureAggregator == nil {
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, loginFlushBeforeReadTime)
	defer cancel()
	_ = s.failureAggregator.Flush(flushCtx)
}

// ListAdminLoginFailures pages the aggregated login failures newest first.
// Global admin only. The account filter is normalized like a login name; a
// "hash:<8 hex>" label filters the rows of an unknown username.
func (s *Service) ListAdminLoginFailures(ctx context.Context, actor domainauth.Principal, filter ports.LoginFailureFilter, limit int, after string) (ports.AdminLoginFailurePage, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.AdminLoginFailurePage{}, err
	}
	if limit == 0 {
		limit = loginFailuresDefaultLimit
	}
	if limit < 1 || limit > loginFailuresMaxLimit {
		return ports.AdminLoginFailurePage{}, domainauth.NewValidationError("limit must be a whole number between 1 and 200")
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		return ports.AdminLoginFailurePage{}, domainauth.NewValidationError("from must not be after to")
	}
	for _, text := range []string{filter.Account, filter.IP, filter.Reason} {
		if !utf8.ValidString(text) || strings.ContainsRune(text, 0) || len(text) > 256 {
			return ports.AdminLoginFailurePage{}, domainauth.NewValidationError("filters must be short valid text")
		}
	}
	filter.Account = normalizeUsername(filter.Account)
	filter.IP = strings.TrimSpace(filter.IP)
	filter.Reason = strings.TrimSpace(filter.Reason)
	cursor, err := decodeLoginFailureCursor(after)
	if err != nil {
		return ports.AdminLoginFailurePage{}, err
	}
	store, err := s.failureStore()
	if err != nil {
		return ports.AdminLoginFailurePage{}, err
	}
	s.flushBeforeRead(ctx)

	// One extra row tells whether another page exists.
	rows, err := store.ListLoginFailures(ctx, filter, cursor, limit+1)
	if err != nil {
		return ports.AdminLoginFailurePage{}, err
	}
	page := ports.AdminLoginFailurePage{Failures: rows}
	if len(rows) > limit {
		page.Failures = rows[:limit]
		page.Next = encodeLoginFailureCursor(page.Failures[limit-1])
	}
	if page.Failures == nil {
		page.Failures = []ports.LoginFailureRecord{}
	}
	return page, nil
}

func encodeLoginFailureCursor(r ports.LoginFailureRecord) string {
	raw, _ := json.Marshal([]string{r.WindowStart.UTC().Format(time.RFC3339Nano), r.Endpoint, r.Reason, r.Account, r.IP})
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeLoginFailureCursor refuses anything that is not a cursor this service
// produced: the fields reach the store as text parameters, where NUL bytes and
// invalid UTF-8 would surface as a 500 on a real Postgres.
func decodeLoginFailureCursor(after string) (*ports.LoginFailureCursor, error) {
	if after == "" {
		return nil, nil
	}
	invalid := domainauth.NewValidationError("cursor is not a valid page cursor")
	raw, err := base64.RawURLEncoding.DecodeString(after)
	if err != nil || len(raw) == 0 || len(raw) > loginFailuresCursorMax || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, invalid
	}
	var fields []string
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 5 {
		return nil, invalid
	}
	window, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return nil, invalid
	}
	for _, field := range fields {
		if strings.ContainsRune(field, 0) || !utf8.ValidString(field) {
			return nil, invalid
		}
	}
	return &ports.LoginFailureCursor{WindowStart: window.UTC(), Endpoint: fields[1], Reason: fields[2], Account: fields[3], IP: fields[4]}, nil
}

// AdminLoginSummary reports the failures of the last 15 minutes and how many
// accounts are waiting out a backoff right now. Global admin only.
func (s *Service) AdminLoginSummary(ctx context.Context, actor domainauth.Principal) (ports.AdminLoginSummary, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.AdminLoginSummary{}, err
	}
	store, err := s.failureStore()
	if err != nil {
		return ports.AdminLoginSummary{}, err
	}
	s.flushBeforeRead(ctx)
	totals, err := store.SummarizeLoginFailures(ctx, s.now().Add(-loginSummaryWindow))
	if err != nil {
		return ports.AdminLoginSummary{}, err
	}
	return ports.AdminLoginSummary{
		WindowMinutes:     int(loginSummaryWindow / time.Minute),
		Failures:          totals.Failures,
		DistinctIPs:       totals.DistinctIPs,
		DistinctAccounts:  totals.DistinctAccounts,
		AccountsInBackoff: len(s.backoff.waiting()),
	}, nil
}

// AdminLoginBackoff reports the effective login policy and the accounts
// waiting out a backoff (at most 100, longest wait first). An existing account
// is shown by its name, any other key by its hash. Global admin only.
func (s *Service) AdminLoginBackoff(ctx context.Context, actor domainauth.Principal) (ports.AdminLoginBackoff, error) {
	if err := requireAdmin(actor); err != nil {
		return ports.AdminLoginBackoff{}, err
	}
	policy := s.backoff.policy
	result := ports.AdminLoginBackoff{
		Mode:           string(policy.Mode),
		Threshold:      policy.Threshold,
		BaseSeconds:    int(policy.Base / time.Second),
		MaxSeconds:     int(policy.Max / time.Second),
		MaxConcurrency: s.limiter.capacity(),
		Accounts:       []ports.AdminBackoffAccount{},
	}
	if !s.backoff.enabled() {
		return result, nil
	}
	waiting := s.backoff.waiting()
	if len(waiting) > loginBackoffListMax {
		waiting = waiting[:loginBackoffListMax]
	}
	for _, entry := range waiting {
		label, err := s.backoffLabel(ctx, entry.key)
		if err != nil {
			return ports.AdminLoginBackoff{}, err
		}
		seconds := int((entry.remaining + time.Second - 1) / time.Second)
		result.Accounts = append(result.Accounts, ports.AdminBackoffAccount{Account: label, Failures: entry.failures, RemainingSeconds: seconds})
	}
	return result, nil
}

// backoffLabel names a backoff key: the account name when the account exists,
// else the hash label an unknown username is shown by everywhere.
func (s *Service) backoffLabel(ctx context.Context, key string) (string, error) {
	_, err := s.store.GetUserByUsername(ctx, key)
	if err == nil {
		return key, nil
	}
	if domainauth.IsCode(err, domainauth.ErrorCodeNotFound) {
		return LoginHashLabel(key), nil
	}
	return "", err
}

// LoginHashLabel is how an unknown username is written in the failures table,
// the backoff listing and the reset request: "hash:" and the short hash the
// login log line carries as account_hash.
func LoginHashLabel(normalized string) string {
	return "hash:" + accountHash(normalized)
}

// ResetAdminLoginBackoff is implemented with the reset endpoint.
func (s *Service) ResetAdminLoginBackoff(context.Context, domainauth.Principal, string) error {
	return domainauth.NewNotFoundError("login backoff", "")
}

package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"regixtry/internal/ports"
)

// fakeLoginAPI is the state behind fakeAdminClient's login security methods:
// the summary, backoff state and failures the screens read, the errors to
// inject and the calls the fake received, in order.
type fakeLoginAPI struct {
	summary  ports.AdminLoginSummary
	backoff  ports.AdminLoginBackoff
	failures []ports.LoginFailureRecord
	// pageSize pages the failures (0 serves everything at once).
	pageSize int

	summaryErr, backoffErr, failuresErr error
	// pageErr fails every failures read that carries a cursor.
	pageErr error
	// resetErr fails every reset; resetErrFor fails the reset of one account.
	resetErr    error
	resetErrFor map[string]error

	summaryCalls, backoffCalls int
	failureReads               []AdminLoginFailuresQuery
	resetCalls                 []string

	// resetGate, when set, blocks every reset until it is closed or receives.
	resetGate chan struct{}
	// onReset runs inside a reset, before it returns (to change what the next
	// read sees, as a reorder on the server would).
	onReset func()
}

func (f *fakeAdminClient) GetLoginSummary(context.Context, AdminSession) (ports.AdminLoginSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loginAPI.summaryCalls++
	if f.loginAPI.summaryErr != nil {
		return ports.AdminLoginSummary{}, f.loginAPI.summaryErr
	}
	return f.loginAPI.summary, nil
}

func (f *fakeAdminClient) GetLoginBackoff(context.Context, AdminSession) (ports.AdminLoginBackoff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loginAPI.backoffCalls++
	if f.loginAPI.backoffErr != nil {
		return ports.AdminLoginBackoff{}, f.loginAPI.backoffErr
	}
	state := f.loginAPI.backoff
	state.Accounts = slices.Clone(state.Accounts)
	return state, nil
}

func (f *fakeAdminClient) ListLoginFailures(_ context.Context, _ AdminSession, query AdminLoginFailuresQuery) (ports.AdminLoginFailurePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loginAPI.failureReads = append(f.loginAPI.failureReads, query)
	if f.loginAPI.failuresErr != nil {
		return ports.AdminLoginFailurePage{}, f.loginAPI.failuresErr
	}
	if query.Cursor != "" && f.loginAPI.pageErr != nil {
		return ports.AdminLoginFailurePage{}, f.loginAPI.pageErr
	}
	all := f.loginAPI.failures
	offset := 0
	if query.Cursor != "" {
		if _, err := fmt.Sscanf(query.Cursor, "c%d", &offset); err != nil {
			return ports.AdminLoginFailurePage{}, fmt.Errorf("invalid cursor %q", query.Cursor)
		}
	}
	size := f.loginAPI.pageSize
	if query.Limit > 0 && (size <= 0 || query.Limit < size) {
		size = query.Limit
	}
	if size <= 0 {
		size = len(all) + 1
	}
	end := min(offset+size, len(all))
	page := ports.AdminLoginFailurePage{Failures: slices.Clone(all[min(offset, end):end])}
	if end < len(all) {
		page.Next = fmt.Sprintf("c%d", end)
	}
	return page, nil
}

func (f *fakeAdminClient) ResetLoginBackoff(_ context.Context, _ AdminSession, account string) (string, error) {
	f.mu.Lock()
	f.loginAPI.resetCalls = append(f.loginAPI.resetCalls, account)
	gate, onReset := f.loginAPI.resetGate, f.loginAPI.onReset
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if onReset != nil {
		onReset()
	}
	if err := f.loginAPI.resetErrFor[account]; err != nil {
		return "", err
	}
	if f.loginAPI.resetErr != nil {
		return "", f.loginAPI.resetErr
	}
	key := strings.ToLower(strings.TrimSpace(account))
	index := slices.IndexFunc(f.loginAPI.backoff.Accounts, func(a ports.AdminBackoffAccount) bool { return a.Account == key })
	if index < 0 {
		return "", adminNotFoundError{message: "write admin resource: login backoff not found", detail: "login backoff not found"}
	}
	f.loginAPI.backoff.Accounts = slices.Delete(slices.Clone(f.loginAPI.backoff.Accounts), index, index+1)
	f.loginAPI.summary.AccountsInBackoff = len(f.loginAPI.backoff.Accounts)
	return key, nil
}

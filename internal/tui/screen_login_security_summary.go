package tui

import (
	"fmt"

	"regixtry/internal/ports"
)

// The warning threshold of the login row and the Login security strip is a
// board PROPOSAL (login-w2 board 38), not an agreed figure: both numbers are
// named so they can change in one place.
const (
	// loginWarnFailures is the number of failed logins in the summary window
	// at which the failures turn amber.
	loginWarnFailures = 20
	// loginWarnWaitingEnforce is the number of accounts waiting at which they
	// turn amber, in enforce mode only: in log mode nobody is delayed.
	loginWarnWaitingEnforce = 1
	// loginSummaryWindowMinutes is the window of the summary when the server
	// does not say (it always does: docs/api.md login-summary).
	loginSummaryWindowMinutes = 15
)

// Backoff modes as the server reports them.
const (
	loginModeLog     = "log"
	loginModeEnforce = "enforce"
	loginModeOff     = "off"
)

// loginFailuresWarn reports that the failures reached the warning threshold.
func loginFailuresWarn(failures int64) bool { return failures >= loginWarnFailures }

// loginWaitingWarn reports that the accounts waiting call for attention: only
// in enforce mode, where somebody really is being delayed.
func loginWaitingWarn(waiting int, mode string) bool {
	return mode == loginModeEnforce && waiting >= loginWarnWaitingEnforce
}

// loginWarning is the board's warning condition for the Overview row.
func loginWarning(failures int64, waiting int, mode string) bool {
	return loginFailuresWarn(failures) || loginWaitingWarn(waiting, mode)
}

// loginAttentionRow is what the Overview keeps of the login reads: the row is
// shown only when both reads succeeded.
type loginAttentionRow struct {
	shown    bool
	failures int64
	waiting  int
	window   int
	mode     string
}

func newLoginAttentionRow(summary ports.AdminLoginSummary, backoff ports.AdminLoginBackoff, err error) loginAttentionRow {
	if err != nil || (summary.Failures == 0 && summary.AccountsInBackoff == 0) {
		return loginAttentionRow{}
	}
	window := summary.WindowMinutes
	if window <= 0 {
		window = loginSummaryWindowMinutes
	}
	return loginAttentionRow{shown: true, failures: summary.Failures, waiting: summary.AccountsInBackoff, window: window, mode: backoff.Mode}
}

// text is "42 failed logins in 15 min; 3 accounts waiting (log mode)".
func (r loginAttentionRow) text() string {
	text := fmt.Sprintf("%s in %d min; %s waiting", pluralUnit(r.failures, "failed login"), r.window, pluralUnit(int64(r.waiting), "account"))
	if r.mode == loginModeLog {
		text += " (log mode)"
	}
	return text
}

func (r loginAttentionRow) tone() attentionTone {
	if loginWarning(r.failures, r.waiting, r.mode) {
		return attentionWarning
	}
	return attentionMuted
}

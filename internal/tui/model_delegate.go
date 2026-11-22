package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// openRepoAdminGrants is the repository administrator's entry point, reached
// from the Console Repositories screen's grant key instead of the global
// admin's tab. It carries the selected repository through the shared login
// screen when the operator is not signed in yet.
func (m Model) openRepoAdminGrants() (tea.Model, tea.Cmd) {
	if m.adminClient == nil {
		m.status = "Admin API is unavailable for this session."
		return m, nil
	}
	repository, ok := m.selectedRepository()
	if !ok {
		m.status = "Select a repository to manage grants."
		return m, nil
	}
	m.adminIntent = adminIntentRepoGrants
	m.adminIntentRepository = repository
	m.adminReturn = m.screen
	m.showMutationNotice = false
	m.err = nil
	if m.adminSession.IsAuthenticated() {
		if m.adminSession.IsExpired(m.now()) {
			return m.expireAdminSession(AdminSessionExpiredReasonExpired), nil
		}
		m.adminAuth = adminAuthStateAuthenticated
		m.adminIntent = adminIntentOperator
		m.adminIntentRepository = ""
		return m.mountDelegate(repository)
	}
	if strings.TrimSpace(m.adminSession.ExpiredReason) != "" {
		m.adminAuth = adminAuthStateExpired
		m.status = m.adminSession.ExpiredReason
	} else {
		m.adminAuth = adminAuthStateUnauthenticated
		m.status = ""
	}
	m.screen = screenAdminLogin
	return m, nil
}

// mountDelegate mounts a FRESH delegateScreen for one repository and starts
// its read: nothing of an earlier visit, another repository's grants
// included, survives into it.
func (m Model) mountDelegate(repository string) (Model, tea.Cmd) {
	next := newDelegateScreen(repository)
	m.adminScreens[slotDelegate] = next
	m.screen = screenRepoAdminGrants
	m.status = ""
	return m, next.Init(m.screenEnv())
}

// routeDelegateMsg delivers a delegate screen result. A result of another
// screen instance is dropped FIRST, so a stale expiry result can never log
// the operator out.
func (m Model) routeDelegateMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	if gen, ok := delegateMsgGen(msg); ok {
		if current, isDelegate := m.adminScreens[slotDelegate].(delegateScreen); !isDelegate || current.gen != gen {
			return m, nil
		}
	}
	if expired, ok := msg.(interface{ expiredReason() (string, bool) }); ok {
		if reason, isExpired := expired.expiredReason(); isExpired {
			return m.expireAdminSession(reason), nil
		}
	}
	var cmd tea.Cmd
	m.adminScreens, cmd = routeAdminMsg(m.screenEnv(), m.adminScreens, msg)
	return m, cmd
}

// dropLeftDelegate un-mounts the delegate screen once the operator is
// anywhere else, so the grants of the repository do not outlive the visit.
func (m *Model) dropLeftDelegate() {
	if m.screen != screenRepoAdminGrants {
		m.adminScreens[slotDelegate] = nil
	}
}

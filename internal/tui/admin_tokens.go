package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	domainauth "registry/internal/domain/auth"
	"registry/internal/ports"
)

type AdminTokensModel struct {
	User         domainauth.User
	Items        []domainauth.Token
	Selected     int
	IssuedSecret string
}

func (m Model) loadAdminTokensCmd(user domainauth.User) tea.Cmd {
	return func() tea.Msg {
		tokens, err := m.authService.ListAdminTokens(m.ctx, m.actor, user.ID)
		return adminTokensLoadedMsg{user: user, tokens: tokens, err: err}
	}
}

func (m Model) selectedToken() (domainauth.Token, bool) {
	if len(m.adminTokens.Items) == 0 {
		return domainauth.Token{}, false
	}
	return m.adminTokens.Items[m.adminTokens.Selected], true
}

func (m Model) updateAdminTokensKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "backspace":
		m.screen = screenAdminUsers
		m.status = ""
		return m, nil
	case "n":
		m.form = &adminFormModel{kind: adminFormCreateToken, title: fmt.Sprintf("Create token · %s", m.adminTokens.User.Username), userID: m.adminTokens.User.ID, fields: []adminField{{Label: "Display name"}, {Label: "TTL hours (blank = default)"}}}
		return m, nil
	case "x":
		token, ok := m.selectedToken()
		if !ok {
			return m, nil
		}
		m.status = "Revoking token..."
		return m, func() tea.Msg {
			err := m.authService.RevokeAdminToken(m.ctx, m.actor, token.Accessor)
			if err != nil {
				return adminTokenMutationMsg{err: err}
			}
			tokens, loadErr := m.authService.ListAdminTokens(m.ctx, m.actor, m.adminTokens.User.ID)
			return adminTokenMutationMsg{user: m.adminTokens.User, tokens: tokens, status: fmt.Sprintf("Token %s revoked.", token.Accessor), err: loadErr}
		}
	}
	return m, nil
}

func submitTokenForm(ctx context.Context, m Model, form adminFormModel) tea.Msg {
	ttl, err := parseTTLHours(form.fields[1].Value)
	if err != nil {
		return adminTokenMutationMsg{err: err}
	}
	created, err := m.authService.CreateAdminToken(ctx, m.actor, ports.CreateAdminTokenInput{UserID: form.userID, Name: form.fields[0].Value, TTL: ttl})
	if err != nil {
		return adminTokenMutationMsg{err: err}
	}
	tokens, loadErr := m.authService.ListAdminTokens(ctx, m.actor, form.userID)
	return adminTokenMutationMsg{user: created.TargetUser, tokens: tokens, selectedAccessor: created.Accessor, issuedSecret: created.Plaintext, status: fmt.Sprintf("Token %s created. Copy the secret now.", created.Accessor), err: loadErr}
}

func selectedTokenIndex(tokens []domainauth.Token, accessor string) int {
	if accessor == "" {
		return boundedIndex(0, len(tokens))
	}
	for index, token := range tokens {
		if token.Accessor == accessor {
			return index
		}
	}
	return boundedIndex(0, len(tokens))
}

func renderAdminTokens(model AdminTokensModel) string {
	lines := []string{fmt.Sprintf("Admin · Tokens · %s", model.User.Username)}
	if len(model.Items) == 0 {
		lines = append(lines, "No admin-issued credential tokens are active.")
	} else {
		for index, token := range model.Items {
			prefix := "  "
			if index == model.Selected {
				prefix = "> "
			}
			name := strings.TrimSpace(token.Name)
			if name == "" {
				name = "(unnamed)"
			}
			state := "active"
			if token.RevokedAt != nil {
				state = "revoked"
			}
			lines = append(lines, fmt.Sprintf("%s%s · %s · expires %s", prefix, token.Accessor, name+" · "+state, token.ExpiresAt.Format("2006-01-02 15:04 UTC")))
		}
	}
	if model.IssuedSecret != "" {
		lines = append(lines, "", fmt.Sprintf("Last issued secret: %s", model.IssuedSecret))
	}
	return strings.Join(lines, "\n")
}

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"regixtry/internal/ports"
)

// A row that does not fit ends with an ellipsis; it is never cut silently,
// and the time left of an expiring token outlives the owner's name.
func TestOverviewRowsNeverClipWithoutAnEllipsis(t *testing.T) {
	t.Parallel()
	owner := strings.Repeat("a-very-long-owner-name-", 4)
	robot := strings.Repeat("robot-with-a-long-name-", 5)
	for _, size := range []struct{ w, h int }{{150, 24}, {151, 25}, {160, 30}, {200, 40}, {220, 60}} {
		client := overviewClient()
		client.robots = append(client.robots, ports.AdminRobot{ID: "r-3", Username: robot, Enabled: true})
		client.expiringTokens = []ports.AdminExpiringToken{
			{ID: "t-1", Username: owner, ExpiresAt: fixedTestTime.Add(5 * time.Hour)},
			{ID: "t-2", Username: "x", ExpiresAt: fixedTestTime.Add(9 * time.Hour)},
		}
		out := overviewRender(overviewModelAt(t, client, size.w, size.h))
		var expiring string
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > size.w {
				t.Errorf("%dx%d: line %d columns wide", size.w, size.h, ansi.StringWidth(line))
			}
			if strings.Contains(line, "Tokens expiring soon") {
				expiring = line
			}
			if strings.Contains(line, "Robot robot-with") && !strings.Contains(line, "…") {
				t.Errorf("%dx%d: robot row clipped without an ellipsis: %q", size.w, size.h, line)
			}
		}
		if expiring == "" {
			t.Fatalf("%dx%d: no expiring row:\n%s", size.w, size.h, out)
		}
		if !strings.Contains(expiring, "…") || !strings.Contains(expiring, " in 5 hours") {
			t.Errorf("%dx%d: owner not shortened with an ellipsis before the time left: %q", size.w, size.h, expiring)
		}
	}
}

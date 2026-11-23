package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

// projectsMany builds n projects with long names and counts.
func projectsMany(n int) []ports.ProjectSummary {
	projects := make([]ports.ProjectSummary, 0, n)
	for i := range n {
		projects = append(projects, ports.ProjectSummary{
			Name:         fmt.Sprintf("project-%03d-%s", i, strings.Repeat("long-name-", 8)),
			CreatedAt:    projectDay(time.Month(1+i%11), 1+i%27),
			Repositories: i % 4,
			Tags:         i * 7,
			LastPushed:   projectDay(time.Month(1+i%11), 1+i%27),
		})
	}
	return projects
}

func TestProjectsGeometryAtEverySize(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 100} {
		for _, size := range geometrySizes {
			t.Run(fmt.Sprintf("%d projects at %dx%d", count, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				client := &fakeAdminClient{projectsAPI: fakeProjectsAPI{projects: projectsMany(count)}}
				m := projectsModelAt(t, client, size.w, size.h)
				label := fmt.Sprintf("list, %d projects at %dx%d", count, size.w, size.h)
				repoAccessNoOverflow(t, label, m.View().Content, size.w, size.h)
				steps := [][]keyMsg{
					projectCreateKeys("a-very-long-project-name-" + strings.Repeat("x", 60)), // too long
					{testKeyEsc(), testKeyRunes("n"), testKeyRunes("o"), testKeyRunes("k")},
					{testKeyEsc()},
				}
				for i, keys := range steps {
					m = projectsPress(t, m, keys...)
					repoAccessNoOverflow(t, fmt.Sprintf("%s, dialog step %d", label, i), m.View().Content, size.w, size.h)
				}
				if count > 0 {
					m = projectsPress(t, m, testKeyRunes("d"))
					repoAccessNoOverflow(t, label+", delete dialog", m.View().Content, size.w, size.h)
					m = projectsPress(t, m, testKeyEsc())
					for range 150 {
						m = projectsPress(t, m, testKeyDown())
					}
					out := projectsRender(m)
					repoAccessNoOverflow(t, label+", scrolled", m.View().Content, size.w, size.h)
					selected := projectsScreenOf(t, m).selectedName
					if !strings.Contains(out, "▸ "+selected[:10]) {
						t.Fatalf("%s: the selected row %q is not on screen:\n%s", label, selected, out)
					}
				}
			})
		}
	}
}

func TestProjectsViewGoldens(t *testing.T) {
	cases := []struct {
		name  string
		build func(t *testing.T, w, h int) Model
	}{
		{"list", func(t *testing.T, w, h int) Model { return projectsModelAt(t, projectsClient(), w, h) }},
		{"list_odabank_selected", func(t *testing.T, w, h int) Model {
			return projectsPress(t, projectsModelAt(t, projectsClient(), w, h), testKeyDown(), testKeyDown(), testKeyRunes("s"))
		}},
		{"empty", func(t *testing.T, w, h int) Model { return projectsModelAt(t, &fakeAdminClient{}, w, h) }},
		{"error", func(t *testing.T, w, h int) Model {
			c := projectsClient()
			c.projectsAPI.listErr = fmt.Errorf("connection refused")
			return projectsModelAt(t, c, w, h)
		}},
		{"not_allowed", func(t *testing.T, w, h int) Model {
			c := projectsClient()
			c.projectsAPI.listErr = adminStatusError{status: 403, message: "x", detail: "forbidden"}
			return projectsModelAt(t, c, w, h)
		}},
		{"create_empty", func(t *testing.T, w, h int) Model {
			return projectsPress(t, projectsModelAt(t, projectsClient(), w, h), testKeyRunes("n"))
		}},
		{"create_valid", func(t *testing.T, w, h int) Model {
			return projectsPress(t, projectsModelAt(t, projectsClient(), w, h), projectCreateKeys("odabank")...)
		}},
		{"create_invalid", func(t *testing.T, w, h int) Model {
			return projectsPress(t, projectsModelAt(t, projectsClient(), w, h), projectCreateKeys("Oda/bank")...)
		}},
		{"create_exists", func(t *testing.T, w, h int) Model {
			c := projectsClient()
			c.projectsAPI.createErr = adminStatusError{status: 409, message: "x", detail: "x"}
			m := projectsPress(t, projectsModelAt(t, c, w, h), projectCreateKeys("odabank")...)
			return projectsPress(t, m, testKeyEnter())
		}},
		{"delete_confirm", func(t *testing.T, w, h int) Model {
			return projectsPress(t, projectsModelAt(t, projectsClient(), w, h), testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d"))
		}},
		{"delete_refused_not_empty", func(t *testing.T, w, h int) Model {
			return projectsPress(t, projectsModelAt(t, projectsClient(), w, h), testKeyDown(), testKeyDown(), testKeyRunes("d"))
		}},
		{"delete_refused_disabled", func(t *testing.T, w, h int) Model {
			c := projectsClient()
			c.projectsAPI.deleteErr = adminStatusError{status: 422, message: "x", detail: "project deletion is not enabled"}
			m := projectsPress(t, projectsModelAt(t, c, w, h), testKeyDown(), testKeyDown(), testKeyDown(), testKeyRunes("d"))
			return projectsPress(t, m, testKeyEnter())
		}},
	}
	for _, tc := range cases {
		for _, size := range []struct{ w, h int }{{150, 24}, {220, 60}} {
			t.Run(fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), func(t *testing.T) {
				m := tc.build(t, size.w, size.h)
				repoAccessNoOverflow(t, tc.name, m.View().Content, size.w, size.h)
				assertProjectsGolden(t, fmt.Sprintf("%s_%dx%d", tc.name, size.w, size.h), projectsRender(m))
			})
		}
	}
}

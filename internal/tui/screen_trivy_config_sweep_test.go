package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"regixtry/internal/ports"
)

func sweepSection(finished time.Time, errs int, refresh string, dbChanged bool) ports.FeatureSection {
	return ports.FeatureSection{ID: "sweep", Title: "Last sweep", Kind: "fields", Fields: []ports.FeatureField{
		{Label: "Finished", Value: finished.UTC().Format(time.RFC3339)},
		{Label: "Visited", Value: "750"}, {Label: "Skipped Unchanged", Value: "742"}, {Label: "Queued", Value: "8"},
		{Label: "Errors", Value: fmt.Sprintf("%d", errs)},
		{Label: "DB Refresh", Value: refresh},
		{Label: "DB Refreshed At", Value: finished.UTC().Format(time.RFC3339)},
		{Label: "DB Changed", Value: fmt.Sprintf("%t", dbChanged)},
	}}
}

func sweepClient(section *ports.FeatureSection, mutate func(*ports.FeaturePage)) *fakeAdminClient {
	client := securityClient()
	page := securityTrivyPage()
	if section != nil {
		page.Sections = append(page.Sections, *section)
	}
	if mutate != nil {
		mutate(&page)
	}
	client.featurePages = map[string]ports.FeaturePage{trivyFeatureName: page}
	return client
}

func TestTrivyConfigShowsTheLastSweepCard(t *testing.T) {
	t.Parallel()
	section := sweepSection(fixedTestTime.Add(-5*time.Hour), 0, "ok", true)
	out := securityRender(securityModelAt(t, sweepClient(&section, nil), screenSecurityTrivy, 200, 40))
	for _, want := range []string{"LAST SWEEP", "finished 5 h ago", "visited 750", "unchanged 742", "queued 8", "no errors",
		"DB refresh ok 5 h ago", "DB changed, digests rescanned", "next sweep in about 19 h", "PER-REPOSITORY OVERRIDES"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
}

func TestTrivyConfigLastSweepStates(t *testing.T) {
	t.Parallel()
	failed := sweepSection(fixedTestTime.Add(-30*time.Hour), 3, "failed: network unreachable", false)
	overdue := sweepSection(fixedTestTime.Add(-3*24*time.Hour), 0, "ok", false)
	for _, tc := range []struct {
		name    string
		client  *fakeAdminClient
		want    []string
		notWant []string
	}{
		{"no sweep yet", sweepClient(nil, nil), []string{"no sweep yet"}, []string{"visited"}},
		{"refresh failed with errors", sweepClient(&failed, nil), []string{"DB refresh failed: network unreachable", "3 errors", "DB unchanged, nothing rescanned", "finished 1 d ago", "next sweep due"}, nil},
		{"overdue", sweepClient(&overdue, nil), []string{"finished 3 d ago", "next sweep due"}, nil},
		{"scheduled scans off", sweepClient(&overdue, func(p *ports.FeaturePage) {
			for i, s := range p.Sections {
				if s.ID == "config" {
					p.Sections[i].Fields[0].Value = "false"
				}
			}
		}), []string{"scheduled scans off"}, []string{"next sweep"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := securityRender(securityModelAt(t, tc.client, screenSecurityTrivy, 200, 40))
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("view lacks %q:\n%s", want, out)
				}
			}
			for _, bad := range tc.notWant {
				if strings.Contains(out, bad) {
					t.Errorf("view shows %q:\n%s", bad, out)
				}
			}
		})
	}
}

func TestTrivyConfigLastSweepFitsEverySizeAndSqueezesOnShortTerminals(t *testing.T) {
	t.Parallel()
	section := sweepSection(fixedTestTime.Add(-5*time.Hour), 2, "failed: "+strings.Repeat("a very long refresh failure ", 8), true)
	clients := map[string]func() *fakeAdminClient{
		"sweep":    func() *fakeAdminClient { return sweepClient(&section, nil) },
		"none":     func() *fakeAdminClient { return sweepClient(nil, nil) },
		"older":    func() *fakeAdminClient { return securityClient() }, // a server without the section
		"overflow": func() *fakeAdminClient { return sweepClient(&section, nil) },
	}
	for name, build := range clients {
		for _, size := range scanViewSizes {
			t.Run(fmt.Sprintf("%s_%dx%d", name, size.w, size.h), func(t *testing.T) {
				t.Parallel()
				out := securityRender(securityModelAt(t, build(), screenSecurityTrivy, size.w, size.h))
				securityNoOverflow(t, "trivy config", out, size.w, size.h)
				if name != "older" && !strings.Contains(strings.ToLower(out), "sweep") {
					t.Errorf("the sweep is nowhere on screen:\n%s", out)
				}
				if size.h <= 25 {
					// Short terminal: no card, the summary rides on the tabs row and
					// the overrides keep their rows.
					if strings.Contains(out, "LAST SWEEP") || !strings.Contains(out, "team/api") && name != "older" {
						t.Errorf("short terminal layout is wrong:\n%s", out)
					}
				} else if name != "older" && !strings.Contains(out, "LAST SWEEP") {
					t.Errorf("no card on a roomy terminal:\n%s", out)
				}
			})
		}
	}
}

func TestTrivyConfigSweepGoldens(t *testing.T) {
	section := sweepSection(fixedTestTime.Add(-5*time.Hour), 1, "ok", true)
	for _, size := range []struct{ w, h int }{{150, 24}, {200, 40}} {
		name := fmt.Sprintf("trivy_config_sweep_%dx%d", size.w, size.h)
		t.Run(name, func(t *testing.T) {
			assertSecurityGolden(t, name, securityRender(securityModelAt(t, sweepClient(&section, nil), screenSecurityTrivy, size.w, size.h)))
		})
	}
}

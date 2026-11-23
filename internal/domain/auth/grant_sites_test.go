package auth

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// exactGrantMatchSites lists every function in non-test source that decides
// something by comparing a grant's repository name with == or !=. A grant on
// a project covers the repositories under it, so any such comparison is a
// place where a project grant could be silently ignored. A new site must be
// added here deliberately, with the reason it is exact on purpose.
var exactGrantMatchSites = map[string]string{
	"internal/domain/auth/principal.go:hasGrantedRepositoryAccess": "the shared predicate; walks project grants right after",
	"internal/domain/auth/grant.go:EffectiveRole":                  "the max rule; walks project grants right after",
	"internal/app/auth/service.go:PutRepositoryGrant":              "escalation bound on exact grants (project awareness is the delegation unit's)",
	"internal/app/auth/service.go:DeleteRepositoryGrant":           "escalation bound on exact grants (project awareness is the delegation unit's)",
	"internal/app/auth/access_events.go:grantRoleOn":               "history text of an exact grant change",
	"internal/tui/screen_edit_user.go:withGrants":                  "presentation only, not a security boundary",
	"internal/tui/screen_edit_user.go:applyRevoked":                "presentation only, not a security boundary",

	// Not grants: other records that carry a repository name.
	"internal/app/regixtry/service.go:ensureUpload":                         "upload state belongs to one repository, not a grant",
	"internal/app/regixtry/service.go:checkOpenUploadCaps":                  "upload state belongs to one repository, not a grant",
	"internal/app/regixtry/repository_overrides.go:GetRepositoryOverride":   "feature override keyed by repository, not a grant",
	"internal/tui/overrides_list.go:applySaved":                             "override rows in the TUI, not a grant",
	"internal/tui/overrides_list.go:rebuild":                                "override rows in the TUI, not a grant",
	"internal/tui/screen_scan_runs_digests.go:digestRows":                   "scan runs in the TUI, not a grant",
	"internal/tui/screen_scan_runs_digests.go:repositoryListed":             "scan rows in the TUI, not a grant",
	"internal/tui/admin_scan_history.go:adminScanHistorySecretsMatchCursor": "scan history cursor, not a grant",
}

var (
	// Any identifier, any alias: x.Repository.String() ==/!= and
	// x.Repository ==/!= (a struct comparison), except against the empty
	// string literal (an emptiness check, not a match).
	exactGrantMatch = regexp.MustCompile(`\.Repository(\.String\(\))?\s*[!=]=\s*[^"\s]`)
	funcDecl        = regexp.MustCompile(`^func (?:\([^)]*\) )?([A-Za-z0-9_]+)`)
)

// TestExactGrantMatchSitesAreAllowlisted scans the production source for
// grant.Repository.String() == style comparisons and fails on any site not in
// the allowlist, so a new exact-match site cannot be added silently.
func TestExactGrantMatchSitesAreAllowlisted(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}

	found := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		if _, err := os.Stat(filepath.Join(root, dir)); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			current := ""
			for _, line := range strings.Split(string(data), "\n") {
				if m := funcDecl.FindStringSubmatch(line); m != nil {
					current = m[1]
				}
				if exactGrantMatch.MatchString(line) {
					found[rel+":"+current] = true
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	for site := range found {
		if _, ok := exactGrantMatchSites[site]; !ok {
			t.Errorf("new exact grant comparison in %s: a project grant would not apply here. Handle project grants (use Principal.HasGrantedRepoAdminAccess, EffectiveRole or the Has*Access predicates) or add the site to exactGrantMatchSites with the reason", site)
		}
	}
	for site := range exactGrantMatchSites {
		if !found[site] {
			t.Errorf("allowlisted site %s no longer exists: remove it from exactGrantMatchSites", site)
		}
	}
}

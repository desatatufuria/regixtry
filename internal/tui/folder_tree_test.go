package tui

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	appregixtry "regixtry/internal/app/regixtry"
)

var treeTime = time.Date(2026, time.November, 21, 8, 41, 0, 0, time.UTC)

func treeRepos(names ...string) []appregixtry.RepositorySummary {
	out := make([]appregixtry.RepositorySummary, len(names))
	for i, name := range names {
		out[i] = appregixtry.RepositorySummary{Name: name, TagCount: i + 1, LastPushed: treeTime.Add(-time.Duration(i) * time.Hour)}
	}
	return out
}

// randomTreeRepos is n distinct ASCII names with a depth of 1 to 8 and a small
// vocabulary, so folders hold several repositories and repositories are often
// folders too.
func randomTreeRepos(rng *rand.Rand, n int) []appregixtry.RepositorySummary {
	words := []string{"odabank", "odabank2", "payments", "strike", "worker", "queue", "a", "b", "c", "d", "e", "f", "g", "metalab", "platform", "runtime"}
	seen := map[string]bool{}
	var out []appregixtry.RepositorySummary
	for len(out) < n {
		depth := 1 + rng.Intn(8)
		parts := make([]string, depth)
		for i := range parts {
			parts[i] = words[rng.Intn(len(words))]
		}
		name := strings.Join(parts, "/")
		if seen[name] {
			if len(seen) > n*20 {
				name += fmt.Sprint(len(out))
			} else {
				continue
			}
		}
		seen[name] = true
		out = append(out, appregixtry.RepositorySummary{Name: name, TagCount: rng.Intn(50), LastPushed: treeTime.Add(-time.Duration(rng.Intn(100000)) * time.Minute)})
	}
	return out
}

// bruteForce is what a folder holds, found by looking at every name.
func bruteForce(repositories []appregixtry.RepositorySummary, path string) (repos, tags int, last time.Time) {
	for _, r := range repositories {
		var in bool
		switch {
		case path == "":
			in = true
		case path == projectUngroupedLabel:
			in = !strings.Contains(r.Name, "/")
		case strings.HasPrefix(path, projectUngroupedLabel+"/"):
			in = r.Name == strings.TrimPrefix(path, projectUngroupedLabel+"/") && !strings.Contains(r.Name, "/")
		default:
			in = strings.HasPrefix(r.Name, path+"/") || (r.Name == path && strings.Contains(path, "/")) // a flat name is never in a project
		}
		if in {
			repos++
			tags += r.TagCount
			if r.LastPushed.After(last) {
				last = r.LastPushed
			}
		}
	}
	return
}

func walkTree(n *folderNode, visit func(*folderNode)) {
	for _, c := range n.children {
		visit(c)
		walkTree(c, visit)
	}
}

func TestBuildFolderTreeEveryRepositoryAppearsExactlyOnceWithBruteForceCounts(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 100, 5000} {
		for seed := int64(1); seed <= 3; seed++ {
			repositories := randomTreeRepos(rand.New(rand.NewSource(seed)), n)
			tree := buildFolderTree(repositories)

			found := map[string]int{}
			visited := 0
			walkTree(tree.root, func(node *folderNode) {
				if node.isRepository {
					found[node.repository.Name]++
				}
				// Brute force is quadratic: look at every node of the small
				// catalogs and at a spread of the 5000-repository one.
				if visited++; n >= 5000 && visited%40 != 0 {
					return
				}
				repos, tags, last := bruteForce(repositories, node.path)
				if node.repositoryCount != repos || node.tagCount != tags || !node.lastPushed.Equal(last) {
					t.Fatalf("n=%d %s: got %d repos %d tags %v, brute force %d %d %v", n, node.path, node.repositoryCount, node.tagCount, node.lastPushed, repos, tags, last)
				}
			})
			for _, r := range repositories {
				if found[r.Name] != 1 {
					t.Fatalf("n=%d: %s appears %d times", n, r.Name, found[r.Name])
				}
			}
			if len(found) != len(repositories) || tree.root.repositoryCount != len(repositories) {
				t.Fatalf("n=%d: %d distinct nodes, root counts %d, want %d", n, len(found), tree.root.repositoryCount, len(repositories))
			}
		}
	}
}

func TestBuildFolderTreeARepositoryThatIsAlsoAFolderIsOneNodeWithBothRoles(t *testing.T) {
	t.Parallel()
	tree := buildFolderTree(treeRepos("odabank/payments", "odabank/payments/strike", "odabank/payments/strike/worker", "odabank/ledger"))
	payments, ok := tree.node("odabank/payments")
	if !ok || !payments.isRepository || !payments.isFolder() || len(payments.children) != 1 || payments.children[0].name != "strike" {
		t.Fatalf("odabank/payments = %+v, want one node that is a repository and a folder", payments)
	}
	if payments.repositoryCount != 3 || payments.tagCount != 1+2+3 {
		t.Fatalf("payments counts %d repositories %d tags, want 3 and 6 (itself included)", payments.repositoryCount, payments.tagCount)
	}
	odabank, _ := tree.node("odabank")
	if odabank.isRepository || odabank.repositoryCount != 4 || len(odabank.children) != 2 {
		t.Fatalf("odabank = %+v, want a folder only holding 4 repositories", odabank)
	}
	// A folder that is not a repository is never one: it has no summary.
	if strike, _ := tree.node("odabank/payments/strike"); !strike.isRepository || strike.repository.Name != "odabank/payments/strike" {
		t.Fatalf("strike = %+v", strike)
	}
}

func TestBuildFolderTreeFlatNamesGoUnderTheUngroupedBucketAndNeverCollideWithProjects(t *testing.T) {
	t.Parallel()
	tree := buildFolderTree(treeRepos("odabank", "odabank2/x", "odabank/a/b/c/d/e/f/g", "zeta"))
	var top []string
	for _, c := range tree.root.children {
		top = append(top, c.path)
	}
	if want := []string{"odabank", "odabank2", projectUngroupedLabel}; !slices.Equal(top, want) {
		t.Fatalf("top level = %v, want %v (the bucket closes the list)", top, want)
	}
	bucket, _ := tree.node(projectUngroupedLabel)
	if bucket.isRepository || bucket.repositoryCount != 2 || len(bucket.children) != 2 {
		t.Fatalf("bucket = %+v", bucket)
	}
	flat, ok := tree.node(projectUngroupedLabel + "/odabank")
	if !ok || !flat.isRepository || flat.repository.Name != "odabank" || flat.isFolder() {
		t.Fatalf("flat repository odabank = %+v", flat)
	}
	folder, _ := tree.node("odabank")
	if folder.isRepository || folder.repositoryCount != 1 {
		t.Fatalf("project odabank = %+v: the flat repository odabank must not merge into it", folder)
	}
	deep, ok := tree.node("odabank/a/b/c/d/e/f/g")
	if !ok || !deep.isRepository || deep.name != "g" {
		t.Fatalf("depth-8 name = %+v", deep)
	}
}

func TestBuildFolderTreeOrderIsStableWhateverTheInputOrder(t *testing.T) {
	t.Parallel()
	repositories := randomTreeRepos(rand.New(rand.NewSource(7)), 300)
	want := dumpTree(buildFolderTree(repositories))
	rng := rand.New(rand.NewSource(8))
	for range 5 {
		shuffled := slices.Clone(repositories)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := dumpTree(buildFolderTree(shuffled)); got != want {
			t.Fatal("the tree depends on the order the catalog came in")
		}
	}
	walkTree(buildFolderTree(repositories).root, func(n *folderNode) {
		if !slices.IsSortedFunc(n.children, func(a, b *folderNode) int { return strings.Compare(a.name, b.name) }) && n.path != "" {
			t.Fatalf("children of %s are not sorted by name", n.path)
		}
	})
}

func dumpTree(tree folderTree) string {
	var b strings.Builder
	walkTree(tree.root, func(n *folderNode) {
		fmt.Fprintf(&b, "%s|%t|%d|%d|%v\n", n.path, n.isRepository, n.repositoryCount, n.tagCount, n.lastPushed)
	})
	return b.String()
}

func TestBuildFolderTreeEmptyAndDuplicateCatalogs(t *testing.T) {
	t.Parallel()
	if tree := buildFolderTree(nil); len(tree.root.children) != 0 || tree.root.repositoryCount != 0 {
		t.Fatalf("empty catalog tree = %+v", tree.root)
	}
	tree := buildFolderTree(treeRepos("a/x", "a/x"))
	if tree.root.repositoryCount != 1 {
		t.Fatalf("a repository listed twice counts %d, want 1", tree.root.repositoryCount)
	}
}

func TestFlattenFolderRowsShowsOnlyExpandedFoldersAndPinsADualRoot(t *testing.T) {
	t.Parallel()
	tree := buildFolderTree(treeRepos("odabank/payments", "odabank/payments/strike", "odabank/ledger", "odabank/zeta/deep/one"))
	odabank, _ := tree.node("odabank")
	names := func(rows []folderRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, fmt.Sprintf("%d:%s", r.depth, r.node.name))
		}
		return strings.Join(out, " ")
	}
	if got := names(flattenFolderRows(odabank, nil)); got != "0:ledger 0:payments 0:zeta" {
		t.Fatalf("collapsed rows = %q", got)
	}
	open := map[string]bool{"odabank/payments": true, "odabank/zeta": true, "odabank/zeta/deep": true}
	if got := names(flattenFolderRows(odabank, open)); got != "0:ledger 0:payments 1:strike 0:zeta 1:deep 2:one" {
		t.Fatalf("expanded rows = %q", got)
	}
	payments, _ := tree.node("odabank/payments")
	rows := flattenFolderRows(payments, nil)
	if len(rows) != 2 || !rows[0].self || rows[0].node != payments || rows[1].self {
		t.Fatalf("a root that is also a repository must pin its own row first: %+v", rows)
	}
	if flattenFolderRows(nil, nil) != nil {
		t.Fatal("no root, no rows")
	}
}

func TestParentPath(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{"odabank": "", "odabank/payments": "odabank", "a/b/c": "a/b", "": ""} {
		if got := parentPath(path); got != want {
			t.Fatalf("parentPath(%q) = %q, want %q", path, got, want)
		}
	}
}

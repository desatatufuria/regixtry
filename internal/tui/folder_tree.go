package tui

import (
	"slices"
	"strings"
	"time"

	appregixtry "regixtry/internal/app/regixtry"
)

// folderNode is one row of the folder tree the Console draws over repository
// names. A folder is never a backend record: it is a prefix of the
// slash-separated names, so a node is a folder (it has children), a repository
// (it is a catalog entry) or both, as odabank/payments is when
// odabank/payments/strike exists too.
type folderNode struct {
	// name is the last path segment; path is the whole slash path and is unique
	// in a tree. The flat repositories (no slash) hang under the ungrouped
	// bucket, so their path is "(ungrouped)/<name>" while repository.Name keeps
	// the real name every action uses.
	name         string
	path         string
	isRepository bool
	repository   appregixtry.RepositorySummary
	children     []*folderNode

	// The aggregates cover the node and everything below it: a repository that
	// is also a folder counts itself.
	repositoryCount int
	tagCount        int
	lastPushed      time.Time
}

// isFolder reports a node with something below it.
func (n *folderNode) isFolder() bool { return len(n.children) > 0 }

// folderTree is the derived tree: root is the nameless node above every
// project. It is immutable once built.
type folderTree struct {
	root   *folderNode
	byPath map[string]*folderNode
}

// node finds the node at a path; the empty path is the root.
func (t folderTree) node(path string) (*folderNode, bool) {
	if path == "" {
		return t.root, t.root != nil
	}
	n, ok := t.byPath[path]
	return n, ok
}

// parentPath is the path one level up: "" for a top-level path.
func parentPath(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// buildFolderTree derives the tree from the catalog. It is pure: the same
// repositories give the same tree whatever their order. Children are sorted by
// name, and the ungrouped bucket closes the top level. A repository listed
// twice appears once (the first entry wins).
func buildFolderTree(repositories []appregixtry.RepositorySummary) folderTree {
	tree := folderTree{root: &folderNode{}, byPath: make(map[string]*folderNode, len(repositories))}
	child := func(parent *folderNode, name, path string) *folderNode {
		if n, ok := tree.byPath[path]; ok {
			return n
		}
		n := &folderNode{name: name, path: path}
		tree.byPath[path] = n
		parent.children = append(parent.children, n)
		return n
	}
	for _, repository := range repositories {
		segments := slices.DeleteFunc(strings.Split(repository.Name, "/"), func(s string) bool { return s == "" })
		if len(segments) == 0 {
			continue
		}
		var leaf *folderNode
		if !strings.Contains(repository.Name, "/") {
			bucket := child(tree.root, projectUngroupedLabel, projectUngroupedLabel)
			leaf = child(bucket, repository.Name, projectUngroupedLabel+"/"+repository.Name)
		} else {
			node, path := tree.root, ""
			for _, segment := range segments {
				if path != "" {
					path += "/"
				}
				path += segment
				node = child(node, segment, path)
			}
			leaf = node
		}
		if !leaf.isRepository {
			leaf.isRepository, leaf.repository = true, repository
		}
	}
	tree.aggregate(tree.root)
	return tree
}

// aggregate sorts the children of n and sums the counts, bottom-up.
func (t folderTree) aggregate(n *folderNode) {
	slices.SortStableFunc(n.children, func(a, b *folderNode) int {
		// The ungrouped bucket always closes the top level.
		if bucketA, bucketB := a.path == projectUngroupedLabel, b.path == projectUngroupedLabel; bucketA != bucketB {
			if bucketA {
				return 1
			}
			return -1
		}
		return strings.Compare(a.name, b.name)
	})
	if n.isRepository {
		n.repositoryCount, n.tagCount, n.lastPushed = 1, n.repository.TagCount, n.repository.LastPushed
	}
	for _, c := range n.children {
		t.aggregate(c)
		n.repositoryCount += c.repositoryCount
		n.tagCount += c.tagCount
		if c.lastPushed.After(n.lastPushed) {
			n.lastPushed = c.lastPushed
		}
	}
}

// folderRow is one visible line of the tree: a node at a depth below the
// current root. self is the pinned first row of a root that is also a
// repository.
type folderRow struct {
	node  *folderNode
	depth int
	self  bool
	// expanded is meaningful for folders.
	expanded bool
}

// flattenFolderRows lists the visible rows below root: its children, each
// followed by its own children when it is expanded. A root that is a
// repository too is pinned first.
func flattenFolderRows(root *folderNode, expanded map[string]bool) []folderRow {
	if root == nil {
		return nil
	}
	rows := make([]folderRow, 0, len(root.children)+1)
	if root.isRepository {
		rows = append(rows, folderRow{node: root, self: true})
	}
	var walk func(nodes []*folderNode, depth int)
	walk = func(nodes []*folderNode, depth int) {
		for _, n := range nodes {
			open := n.isFolder() && expanded[n.path]
			rows = append(rows, folderRow{node: n, depth: depth, expanded: open})
			if open {
				walk(n.children, depth+1)
			}
		}
	}
	walk(root.children, 0)
	return rows
}

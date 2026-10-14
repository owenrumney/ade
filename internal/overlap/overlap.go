// Package overlap relates a repository's worktrees by the commits they hold.
// The model is in .git/ade/docs/worktree-navigator.md.
package overlap

import (
	"context"
	"path/filepath"
	"sort"
	"sync"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/worktrees"
)

// Edge kinds. Absorbed is about one worktree, the rest about a pair.
const (
	// KindAbsorbed: every patch of From is on To, so From can go. Directed.
	KindAbsorbed = "absorbed"
	// KindShares: the same commits by SHA — one was started from the other.
	KindShares = "shares"
	// KindDuplicates: the same patch under different SHAs. A cherry-pick.
	KindDuplicates = "duplicates"
)

// Node is one worktree and the commits it holds that the base does not.
type Node struct {
	// Embedded, so one read answers the whole panel: what stands in the way of
	// removing a worktree is the scan's, and is wanted beside this verdict.
	worktrees.Worktree

	Commits []gitdiff.Rev `json:"commits"`

	// Redundant holds nothing of its own: every commit is on the base.
	Redundant bool `json:"redundant"`

	// Landed means merging it into the base would change nothing, though its
	// commits are not on the base: a squash or a rebase on the forge.
	Landed bool `json:"landed,omitempty"`

	// Why is the read that failed: commits unknown rather than empty.
	Why string `json:"why,omitempty"`
}

// Edge is one relation between two worktrees, named by path.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Kind  string `json:"kind"`
	Count int    `json:"count,omitempty"`
}

// Graph is the whole answer: the base everything is measured against, a node
// per worktree, and every relation between them.
type Graph struct {
	// Base is the commit, BaseRef what named it (origin/main, or the branch
	// itself with no remote) and BaseBranch the repository's base branch.
	Base       string `json:"base"`
	BaseRef    string `json:"baseRef,omitempty"`
	BaseBranch string `json:"baseBranch,omitempty"`

	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`

	// Removable is the commit-only verdict, carried with the graph that earned
	// it so no caller has to derive it a second way.
	Removable []string `json:"removable"`
}

// Cache holds each worktree's read, keyed by the commit it was on: the
// patch-id pass walks every diff on the branch. A zero Cache is ready.
type Cache struct {
	mu   sync.Mutex
	held map[string]reading
}

type reading struct {
	tip     string
	base    string
	commits []gitdiff.Rev
	landed  bool
}

// Build reads every worktree of the repository and relates them, with no
// caching. Equivalent to a fresh Cache, for a caller that reads once.
func Build(ctx context.Context, root string) (Graph, error) {
	return new(Cache).Build(ctx, root)
}

// Build reads every worktree and relates them. A review checkout is left
// out: detached at someone else's head, base..HEAD is not work done here.
func (c *Cache) Build(ctx context.Context, root string) (Graph, error) {
	found, err := worktrees.Switchable(ctx, root)
	if err != nil {
		return Graph{}, err
	}
	main, err := mainOf(ctx, root)
	if err != nil {
		return Graph{}, err
	}
	branch := gitdiff.BaseBranch(ctx, main)
	ref := gitdiff.RemoteBase(ctx, main, branch)
	if ref == "" {
		ref = branch
	}
	// A commit, not a ref: the cache key has to move when the base does.
	base, err := gitdiff.Resolve(ctx, main, ref)
	if err != nil {
		return Graph{}, err
	}

	g := Graph{Base: base, BaseRef: ref, BaseBranch: branch, Nodes: []Node{}, Edges: []Edge{}}
	for _, wt := range found {
		if wt.Kind == worktrees.KindReview {
			continue
		}
		g.Nodes = append(g.Nodes, c.read(ctx, wt, base))
	}
	g.Edges = Relate(g.Nodes)
	g.Removable = Removable(g)
	c.forget(g.Nodes)
	return g, nil
}

// Relate is every edge the nodes earn. Split out from Build so the arithmetic
// is testable without a repository.
func Relate(nodes []Node) []Edge {
	shas := make([]map[string]bool, len(nodes))
	patches := make([]map[string]bool, len(nodes))
	for i, node := range nodes {
		shas[i], patches[i] = sets(node)
	}

	out := []Edge{}
	for i := range nodes {
		for j := range nodes {
			if i == j || nodes[i].Why != "" || nodes[j].Why != "" {
				continue
			}
			if absorbs(nodes[i], nodes[j], patches[i], patches[j]) {
				out = append(out, Edge{From: nodes[i].Path, To: nodes[j].Path, Kind: KindAbsorbed})
			}
			if j <= i {
				continue
			}
			shared := overlapCount(shas[i], shas[j])
			if shared > 0 {
				out = append(out, Edge{
					From: nodes[i].Path, To: nodes[j].Path,
					Kind: KindShares, Count: shared,
				})
			}
			// Only the copies: one SHA on both is shared, not duplicated.
			if copies := overlapCount(patches[i], patches[j]) - shared; copies > 0 {
				out = append(out, Edge{
					From: nodes[i].Path, To: nodes[j].Path,
					Kind: KindDuplicates, Count: copies,
				})
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].From != out[b].From {
			return out[a].From < out[b].From
		}
		if out[a].Kind != out[b].Kind {
			return out[a].Kind < out[b].Kind
		}
		return out[a].To < out[b].To
	})
	return out
}

// Removable is redundant, landed or absorbed. A verdict about commits only:
// what is uncommitted or unpushed is the scan's, and stands separately.
func Removable(g Graph) []string {
	absorbed := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind == KindAbsorbed {
			absorbed[e.From] = true
		}
	}
	out := []string{}
	for _, node := range g.Nodes {
		if node.Main || node.Why != "" {
			continue
		}
		if node.Redundant || node.Landed || absorbed[node.Path] {
			out = append(out, node.Path)
		}
	}
	return out
}

// mainOf is the repository's own checkout, where the base is resolved: a
// worktree shares the refs and not the HEAD.
func mainOf(ctx context.Context, root string) (string, error) {
	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return "", err
	}
	return filepath.Dir(common), nil
}

func (c *Cache) read(ctx context.Context, wt worktrees.Worktree, base string) Node {
	node := Node{Worktree: wt, Commits: []gitdiff.Rev{}}
	head := wt.Branch
	if head == "" {
		head = "HEAD"
	}

	// One cheap call; the two behind it are not. Unreadable means uncached.
	tip, tipErr := gitdiff.HeadSHA(ctx, wt.Path)
	if tipErr == nil {
		if held, landed, ok := c.get(wt.Path, tip, base); ok {
			node.Commits = held
			node.Landed = landed
			node.Redundant = !wt.Main && len(held) == 0
			return node
		}
	}

	commits, err := gitdiff.Commits(ctx, wt.Path, base, head)
	if err != nil {
		node.Why = err.Error()
		return node
	}
	if commits != nil {
		node.Commits = commits
	}
	node.Redundant = !wt.Main && len(node.Commits) == 0
	// Only worth asking of a branch that still looks like it holds work: the
	// test is a merge git throws away, and redundant already answers the rest.
	if !wt.Main && !node.Redundant && head != "HEAD" {
		node.Landed, _ = gitdiff.LandedIn(ctx, wt.Path, base, head)
	}
	if tipErr == nil {
		c.put(wt.Path, tip, base, node.Commits, node.Landed)
	}
	return node
}

func (c *Cache) get(path, tip, base string) ([]gitdiff.Rev, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.held[path]
	if !ok || held.tip != tip || held.base != base {
		return nil, false, false
	}
	return held.commits, held.landed, true
}

func (c *Cache) put(path, tip, base string, commits []gitdiff.Rev, landed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.held == nil {
		c.held = map[string]reading{}
	}
	c.held[path] = reading{tip: tip, base: base, commits: commits, landed: landed}
}

// forget drops what is no longer a worktree.
func (c *Cache) forget(nodes []Node) {
	live := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		live[node.Path] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.held {
		if !live[path] {
			delete(c.held, path)
		}
	}
}

// sets are a node's commits by SHA and by patch-id. A merge has no patch-id,
// so it counts as shared history and never as duplicated work.
func sets(node Node) (shas, patches map[string]bool) {
	shas = map[string]bool{}
	patches = map[string]bool{}
	for _, c := range node.Commits {
		shas[c.SHA] = true
		if c.PatchID != "" {
			patches[c.PatchID] = true
		}
	}
	return shas, patches
}

// absorbs is whether from can go because to holds all of its patches. An
// equal pair absorbs both ways, so the tie breaks on path: one must survive.
func absorbs(from, to Node, fromPatches, toPatches map[string]bool) bool {
	if from.Main || len(fromPatches) == 0 || !subset(fromPatches, toPatches) {
		return false
	}
	if len(fromPatches) == len(toPatches) {
		return from.Path > to.Path
	}
	return true
}

func subset(small, big map[string]bool) bool {
	if len(small) > len(big) {
		return false
	}
	for key := range small {
		if !big[key] {
			return false
		}
	}
	return true
}

func overlapCount(a, b map[string]bool) int {
	if len(a) > len(b) {
		a, b = b, a
	}
	n := 0
	for key := range a {
		if b[key] {
			n++
		}
	}
	return n
}

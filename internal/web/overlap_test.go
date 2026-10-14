package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/overlap"
)

type overlapBody struct {
	Base       string         `json:"base"`
	BaseBranch string         `json:"baseBranch"`
	Nodes      []overlap.Node `json:"nodes"`
	Edges      []overlap.Edge `json:"edges"`
	Removable  []string       `json:"removable"`
}

func askOverlap(t *testing.T, s *Server) overlapBody {
	t.Helper()
	rec := do(t, s, "GET", "/api/worktrees/overlap", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out overlapBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

func TestOverlapNamesTheBaseAndEveryWorktree(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, one, "one"))

	got := askOverlap(t, s)
	assert.Equal(t, "main", got.BaseBranch)
	assert.Len(t, got.Base, 40, "the base is a resolved commit, not a ref")

	// The main checkout and the one worktree. newTestServer's own checkout is
	// on feature, which is a commit ahead of main.
	paths := []string{}
	for _, n := range got.Nodes {
		paths = append(paths, n.Path)
	}
	assert.Contains(t, paths, one)
	assert.Len(t, got.Nodes, 2)
}

// A worktree branched straight off the base holds nothing of its own, so
// removing it loses nothing — which is the answer the panel pre-ticks.
func TestOverlapCallsAWorktreeOffTheBaseRedundant(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	empty := gitdiff.WorktreePath(root, "empty")
	require.NoError(t, gitdiff.AddWorktreeFrom(context.Background(), root, empty, "empty", "main"))

	got := askOverlap(t, s)
	assert.Equal(t, []string{empty}, got.Removable)
	for _, n := range got.Nodes {
		if n.Path == empty {
			assert.True(t, n.Redundant)
			assert.Empty(t, n.Commits)
		}
	}
}

func TestOverlapFindsTheSamePatchInTwoWorktrees(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	ctx := context.Background()

	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktreeFrom(ctx, root, one, "one", "main"))
	require.NoError(t, os.WriteFile(filepath.Join(one, "shared.go"),
		[]byte("package main\n\nfunc shared() {}\n"), 0o600))
	gitIn(t, one, "add", ".")
	gitIn(t, one, "commit", "-qm", "shared work")

	two := gitdiff.WorktreePath(root, "two")
	require.NoError(t, gitdiff.AddWorktreeFrom(ctx, root, two, "two", "main"))
	require.NoError(t, os.WriteFile(filepath.Join(two, "own.go"),
		[]byte("package main\n"), 0o600))
	gitIn(t, two, "add", ".")
	gitIn(t, two, "commit", "-qm", "own work")
	gitIn(t, two, "cherry-pick", "one")

	got := askOverlap(t, s)
	dupes := []overlap.Edge{}
	for _, e := range got.Edges {
		if e.Kind == overlap.KindDuplicates {
			dupes = append(dupes, e)
		}
	}
	require.Len(t, dupes, 1, "edges: %v", got.Edges)
	assert.Equal(t, 1, dupes[0].Count)
	assert.ElementsMatch(t, []string{one, two}, []string{dupes[0].From, dupes[0].To})
}

// Asked twice with nothing moved, the answer is the same: the cache must not
// change what is reported, only what it costs.
func TestOverlapIsTheSameAnswerTwice(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	one := gitdiff.WorktreePath(root, "one")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, one, "one"))

	first, again := askOverlap(t, s), askOverlap(t, s)
	assert.Equal(t, first.Edges, again.Edges)
	assert.Equal(t, first.Removable, again.Removable)
	assert.Equal(t, first.Base, again.Base)
}

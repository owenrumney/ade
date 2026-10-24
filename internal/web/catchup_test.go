package web

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// behindBy puts n commits on main that the feature branch does not have, in a
// file the branch never touches, so the replay is clean.
func behindBy(t *testing.T, dir string, n int) {
	t.Helper()
	gitIn(t, dir, "checkout", "-q", "main")
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, "base.go")
		require.NoError(t, os.WriteFile(name, []byte("package main\n\n// "+string(rune('a'+i))+"\n"), 0o600))
		gitIn(t, dir, "add", ".")
		gitIn(t, dir, "commit", "-qm", "on main")
	}
	gitIn(t, dir, "checkout", "-q", "feature")
}

func catchUp(t *testing.T, s *Server) (int, CaughtUp) {
	t.Helper()
	rec := do(t, s, http.MethodPost, "/api/base/catch-up", "")
	var got CaughtUp
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	}
	return rec.Code, got
}

func TestCatchingUpStashesReplaysAndBringsTheWorkBack(t *testing.T) {
	s := newTestServer(t)
	dir := s.space().homeRoot
	behindBy(t, dir, 6)

	// Uncommitted work the old rebase route would have refused outright.
	mine := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(mine, []byte("package main\n\nvar x = 2\nvar mine = true\n"), 0o600))

	code, got := catchUp(t, s)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 6, got.Behind)
	assert.True(t, got.Stashed)
	assert.Empty(t, got.Stuck)

	body, err := os.ReadFile(mine)
	require.NoError(t, err)
	assert.Contains(t, string(body), "var mine = true", "the uncommitted work came back")

	out, err := exec.Command("git", "-C", dir, "log", "--oneline", "-3").CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(out), "on main", "the branch sits on the new base")
}

// Nothing to replay is not a failure, and it must not stash and unstash for
// the sake of it.
func TestCatchingUpOnAnUpToDateBranchDoesNothing(t *testing.T) {
	s := newTestServer(t)
	code, got := catchUp(t, s)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 0, got.Behind)
	assert.False(t, got.Stashed)
}

// The rebase is all or nothing: a conflict the resolver cannot settle (no
// agent answers in a test) puts the checkout back as it was found.
func TestARebaseThatCannotBeSettledIsUndone(t *testing.T) {
	s := newTestServer(t)
	dir := s.space().homeRoot

	// main changes the same line the branch changed, so the replay conflicts.
	gitIn(t, dir, "checkout", "-q", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"),
		[]byte("package main\n\nvar x = 99\n"), 0o600))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "main moved the same line")
	gitIn(t, dir, "checkout", "-q", "feature")

	mine := filepath.Join(dir, "b.go")
	require.NoError(t, os.WriteFile(mine, []byte("package main\n\nvar mine = true\n"), 0o600))
	gitIn(t, dir, "add", "b.go")

	before := headOf(t, dir)
	code, _ := catchUp(t, s)
	assert.Equal(t, http.StatusConflict, code)

	assert.Equal(t, before, headOf(t, dir), "the branch did not move")
	body, err := os.ReadFile(mine)
	require.NoError(t, err)
	assert.Contains(t, string(body), "var mine = true", "the work is back in the tree")
	assert.False(t, midRebase(t, dir), "no rebase was left in progress")
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	require.NoError(t, err)
	return string(out)
}

// A rebase left in progress is the worst outcome: the next thing anyone does
// in the checkout fails in a way that has nothing to do with what they asked.
func midRebase(t *testing.T, dir string) bool {
	t.Helper()
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(dir, ".git", name)); err == nil {
			return true
		}
	}
	return false
}

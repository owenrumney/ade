package web

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullBaseFailsWithoutARemote(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/base/pull", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "fetch origin/main")
}

func TestPullBranchFailsWithoutARemote(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/branch/pull", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "fetch origin/")
}

// A rebase replays commits; uncommitted work is not replayed, it is lost. The
// server refuses rather than trusting the page to have disabled the button.
func TestRebaseRefusesOverUncommittedWork(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.startRoot, "a.go"), []byte("package main\n\nvar x = 3\n"), 0o600))

	rec := do(t, s, "POST", "/api/base/rebase", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "commit or stash")
	assert.Contains(t, rec.Body.String(), "loses them")
}

// The fetch comes before the replay, so a branch is rebased onto the base as
// it is now rather than onto a stale copy of it.
func TestRebaseStopsWhenTheBaseCannotBeFetched(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/base/rebase", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "fetch origin/main")
}

// originWithMovedMain gives the test checkout a remote whose main has a
// commit this checkout has not fetched, and answers that commit.
func originWithMovedMain(t *testing.T, root string) string {
	t.Helper()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	origin := t.TempDir()
	git(origin, "init", "--bare", "-q", "-b", "main")
	git(root, "remote", "add", "origin", origin)
	git(root, "push", "-q", "origin", "--all")
	other := t.TempDir()
	git(other, "clone", "-q", "-b", "main", origin, "wt")
	git(filepath.Join(other, "wt"), "commit", "-q", "--allow-empty", "-m", "main moved on")
	git(filepath.Join(other, "wt"), "push", "-q", "origin", "main")
	return git(filepath.Join(other, "wt"), "rev-parse", "HEAD")
}

func originMain(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "rev-parse", "origin/main").Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// A branch pulled without its base is diffed against a main it may have
// been rebased past, which shows main's commits as the branch's own.
func TestPullBranchBringsTheBaseAlong(t *testing.T) {
	s := newTestServer(t)
	moved := originWithMovedMain(t, s.startRoot)
	rec := do(t, s, "POST", "/api/branch/pull", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, moved, originMain(t, s.startRoot), "origin/main was fetched with the branch")
}

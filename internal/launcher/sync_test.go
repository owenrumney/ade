package launcher

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/worktrees"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// syncFixture is a repository under a fixture HOME's scan directory, so the
// launcher's own scan offers it and a sync can name it.
func syncFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, "code", "proj")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	gitIn(t, dir, "init", "-b", "main", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o600))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	return dir
}

func TestSyncingIsLimitedToRepositoriesThatWereFound(t *testing.T) {
	s := newTestLauncher(t)

	rec := do(t, s, "POST", "/api/sync", "{not json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	for _, body := range []string{`{"repo":"/etc"}`, `{"repo":""}`} {
		rec := do(t, s, "POST", "/api/sync", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "not one of the repositories")
	}
}

func TestSyncReportsOnEveryCheckoutOfTheRepository(t *testing.T) {
	syncFixture(t)
	s := newTestLauncher(t)

	repos := listRepos(t, s)
	require.Len(t, repos, 1, "the fixture is meant to be the only repository")
	body := `{"repo":` + strconv.Quote(repos[0].Path) + `,"dryRun":true}`

	rec := do(t, s, "POST", "/api/sync", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var report worktrees.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	assert.Equal(t, "main", report.Base, "no origin, so the local branch is the base")
	require.NotEmpty(t, report.Results)
	assert.True(t, report.Results[0].Main)
	assert.Equal(t, worktrees.OutcomeUpToDate, report.Results[0].Outcome)
}

// A sync in flight refuses another of the same repository, and the slot is
// given back afterwards rather than held for the life of the launcher.
func TestASecondSyncOfOneRepositoryIsRefused(t *testing.T) {
	syncFixture(t)
	s := newTestLauncher(t)

	repos := listRepos(t, s)
	require.Len(t, repos, 1)
	path := repos[0].Path
	body := `{"repo":` + strconv.Quote(path) + `,"dryRun":true}`

	require.True(t, s.claimSync(path))
	rec := do(t, s, "POST", "/api/sync", body)
	assert.Equal(t, http.StatusConflict, rec.Code)

	s.releaseSync(path)
	rec = do(t, s, "POST", "/api/sync", body)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestSyncingOneRepositoryLeavesAnotherFree(t *testing.T) {
	s := newTestLauncher(t)

	require.True(t, s.claimSync("/one"))
	assert.False(t, s.claimSync("/one"), "the same repository is held")
	assert.True(t, s.claimSync("/another"), "a sync of one repository is no reason to refuse another")
}

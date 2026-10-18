package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
)

// sweptRepo is a repository under the fixture home's scan root, with one
// commit, so a worktree can be added to it.
func sweptRepo(t *testing.T, home, name string) string {
	return repoAt(t, filepath.Join(home, "code", name))
}

// repoAt is one commit in a directory, so a worktree can be added to it.
func repoAt(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "owen@example.com"},
		{"config", "user.name", "Owen"},
	} {
		run(t, dir, args...)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o600))
	run(t, dir, "add", ".")
	run(t, dir, "commit", "-qm", "base")
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func sweptWorktree(t *testing.T, root, slug string) string {
	t.Helper()
	path := gitdiff.WorktreePath(root, slug)
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, path, slug))
	return path
}

func swept(t *testing.T, s *Server, query string) sweepResponse {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/worktrees/all"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got sweepResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

func TestTheSweepGroupsEveryWorktreeByRepository(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := newTestLauncher(t)

	one := sweptRepo(t, home, "one")
	two := sweptRepo(t, home, "two")
	sweptRepo(t, home, "bare")
	clean := sweptWorktree(t, one, "clean")
	dirty := sweptWorktree(t, two, "dirty")
	require.NoError(t, os.WriteFile(filepath.Join(dirty, "b.go"), []byte("package main\n"), 0o600))
	run(t, dirty, "add", ".")

	got := swept(t, s, "")
	names := []string{}
	for _, repo := range got.Repos {
		names = append(names, repo.Name)
	}
	assert.Equal(t, []string{"one", "two"}, names, "a repository with no worktrees is left out")

	require.Len(t, got.Repos[0].Tree, 1)
	assert.Equal(t, clean, got.Repos[0].Tree[0].Path)
	assert.True(t, got.Repos[0].Tree[0].Removable)

	require.Len(t, got.Repos[1].Tree, 1)
	assert.False(t, got.Repos[1].Tree[0].Removable)
	assert.Equal(t, "uncommitted", got.Repos[1].Tree[0].Keep)
}

// A repository ade cannot read is a row saying why, so one unreadable
// checkout does not cost the sweep every other one.
func TestARepositoryThatCannotBeScannedIsARowAndNotAFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	broken := sweptRepo(t, home, "broken")
	require.NoError(t, os.RemoveAll(filepath.Join(broken, ".git")))
	require.NoError(t, os.WriteFile(filepath.Join(broken, ".git"), []byte("not a git dir\n"), 0o600))

	repos := sessions.ReposIn([]string{filepath.Join(home, "code")}, nil, sessions.Everything())
	require.NotEmpty(t, repos)
	rows := sweepAll(context.Background(), repos)
	require.Len(t, rows, 1)
	assert.Equal(t, "broken", rows[0].Name)
	assert.NotEmpty(t, rows[0].Why)
	assert.Empty(t, rows[0].Tree)
}

func TestASweepInsideTheTTLIsTheOneAlreadyTaken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := newTestLauncher(t)

	one := sweptRepo(t, home, "one")
	sweptWorktree(t, one, "first")
	first := swept(t, s, "")
	require.Len(t, first.Repos, 1)
	require.Len(t, first.Repos[0].Tree, 1)

	sweptWorktree(t, one, "second")
	again := swept(t, s, "")
	assert.Equal(t, first.At, again.At, "the cached sweep stands")
	require.Len(t, again.Repos[0].Tree, 1)

	fresh := swept(t, s, "?fresh=1")
	assert.Len(t, fresh.Repos[0].Tree, 2, "Refresh rescans")
}

// A client that walks away cancels the request, and every repository then
// fails at once. That is not a reading and must not stand for the TTL.
func TestASweepTheClientAbandonedIsNotTheCachedReading(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := newTestLauncher(t)

	one := sweptRepo(t, home, "one")
	sweptWorktree(t, one, "spare")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/worktrees/all", nil).WithContext(ctx)
	req.Header.Set("X-Ade-Token", s.token)
	s.routes().ServeHTTP(httptest.NewRecorder(), req)

	got := swept(t, s, "")
	require.Len(t, got.Repos, 1)
	assert.Empty(t, got.Repos[0].Why)
	assert.Len(t, got.Repos[0].Tree, 1, "the next read scans rather than serving the abandoned one")
}

// A scan that started before a removal knows nothing of it, so its result is
// dropped rather than written over the forget.
func TestASweepStartedBeforeAPruneIsDropped(t *testing.T) {
	var c sweepCache
	gen := c.mark()
	c.forget()
	c.put(sweepResponse{At: time.Now(), Repos: []sweepRepo{{Name: "gone"}}}, gen)

	_, ok := c.warm()
	assert.False(t, ok)
}

// Removal is what makes the list wrong, so a prune drops the reading.
func TestAPruneForgetsTheSweep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := newTestLauncher(t)

	one := sweptRepo(t, home, "one")
	path := sweptWorktree(t, one, "spare")
	require.Len(t, swept(t, s, "").Repos, 1)

	body, err := json.Marshal(map[string]any{"repo": one, "paths": []string{path}})
	require.NoError(t, err)
	rec := do(t, s, http.MethodPost, "/api/prune", string(body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Empty(t, swept(t, s, "").Repos, "what went is not still on the list")
}

// A worker clones each session into <work root>/<16 hex>/<repo>. Those have
// no linked worktrees, so until now the sweep dropped them and the abandoned
// ones were the part of a machine's disk nothing listed.
func TestTheSweepListsAnAbandonedSessionClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(home, "work")
	t.Setenv("ADE_WORK_DIR", work)
	s := newTestLauncher(t)

	origin := sweptRepo(t, t.TempDir(), "ade")
	spare := clonedRepo(t, work, origin, "3ab3e90b3add1647", "ade")
	held := clonedRepo(t, work, origin, "45e04b86dbab639b", "ade")
	require.NoError(t, os.WriteFile(filepath.Join(held, "b.go"), []byte("package main\n"), 0o600))
	run(t, held, "add", ".")

	byPath := map[string]int{}
	for _, repo := range swept(t, s, "").Repos {
		for _, tree := range repo.Tree {
			assert.Equal(t, "clone", tree.Kind)
			byPath[tree.Path] = tree.Uncommitted
			if tree.Path == spare {
				assert.True(t, tree.Removable, "nothing would be lost")
				assert.Empty(t, tree.Keep)
			}
			if tree.Path == held {
				assert.False(t, tree.Removable, "there is work in it")
				assert.Equal(t, "uncommitted", tree.Keep)
			}
		}
	}
	assert.Contains(t, byPath, spare)
	assert.Contains(t, byPath, held)
}

// clonedRepo is a checkout where the provisioner puts one: a random session
// directory under the work root, holding a clone of the repository. Cloned
// rather than init'd, so it has an origin and its commits are pushed, which
// is what a worker's checkout of a GitHub repository looks like.
func clonedRepo(t *testing.T, work, origin, session, name string) string {
	t.Helper()
	dir := filepath.Join(work, session)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	run(t, dir, "clone", "-q", origin, name)
	return filepath.Join(dir, name)
}

// Removing a clone takes the session directory around it, which is what
// `ade done` does from inside one. The clone is a whole checkout, so the
// panel's row is the repository itself.
func TestPruningAnAbandonedCloneTakesItsSessionDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(home, "work")
	t.Setenv("ADE_WORK_DIR", work)
	s := newTestLauncher(t)

	origin := sweptRepo(t, t.TempDir(), "ade")
	spare := clonedRepo(t, work, origin, "3ab3e90b3add1647", "ade")
	held := clonedRepo(t, work, origin, "45e04b86dbab639b", "ade")
	require.NoError(t, os.WriteFile(filepath.Join(held, "b.go"), []byte("package main\n"), 0o600))
	run(t, held, "add", ".")

	rec := do(t, s, http.MethodPost, "/api/prune",
		`{"repo":"`+spare+`","paths":["`+spare+`"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), spare)
	assert.NoDirExists(t, filepath.Join(work, "3ab3e90b3add1647"), "the session directory goes with it")

	// Work in it is work nobody asked to lose.
	rec = do(t, s, http.MethodPost, "/api/prune",
		`{"repo":"`+held+`","paths":["`+held+`"]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "uncommitted")
	assert.DirExists(t, held)
}

// The panel names a path, and a request is not the panel.
func TestPruningACloneRefusesAPathThatIsNotIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := filepath.Join(home, "work")
	t.Setenv("ADE_WORK_DIR", work)
	s := newTestLauncher(t)

	origin := sweptRepo(t, t.TempDir(), "ade")
	clone := clonedRepo(t, work, origin, "3ab3e90b3add1647", "ade")
	other := clonedRepo(t, work, origin, "45e04b86dbab639b", "ade")

	rec := do(t, s, http.MethodPost, "/api/prune",
		`{"repo":"`+clone+`","paths":["`+other+`"]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "not that clone")
	assert.DirExists(t, other)
	assert.DirExists(t, clone)
}

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/review"
	"github.com/owenrumney/ade/internal/sandbox"
)

// otherRepo builds a second repository with an uncommitted edit, which is the
// shape a sandbox root is usually in: its own main branch, work not committed.
func otherRepo(t *testing.T, name string) string {
	t.Helper()
	// Not t.TempDir: a run still writing here as the test ends fails its
	// removal, and t.TempDir fails the test on that. Here it is ignored.
	base, err := os.MkdirTemp("", "ade-other-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	// The canonical path, because that is what ade stores and reports back:
	// on a Mac the temp directory is /var/..., a symlink to /private/var/...,
	// and comparing the two spellings fails on the machine but not in CI.
	base, err = filepath.EvalSymlinks(base)
	require.NoError(t, err)
	dir := filepath.Join(base, name)
	require.NoError(t, os.MkdirAll(dir, 0o750))

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	git("init", "-b", "main", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ci.yml"), []byte("steps: []\n"), 0o600))
	git("add", ".")
	git("commit", "-qm", "base")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ci.yml"), []byte("steps: [build]\n"), 0o600))
	return dir
}

// mount writes the sandbox file the roots are snapshotted from.
func mount(t *testing.T, s *Server, paths ...sandbox.Path) {
	t.Helper()
	file := s.agent.workspace
	require.NotEmpty(t, file, "the server should have a sandbox file to write")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o750))
	data, err := json.Marshal(sandbox.Workspace{Paths: paths})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, data, 0o600))
}

// withRoot is a server whose sandbox mounts other read-write, snapshotted.
func withRoot(t *testing.T, other string) *Server {
	t.Helper()
	s := newTestServer(t)
	mount(t, s, sandbox.Path{Path: other, RW: true})
	s.snapshotRoots(context.Background())
	return s
}

func TestSwitchingRootServesTheOtherRepositorysDiff(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)

	roots := s.currentRoots()
	require.Len(t, roots, 2)
	assert.True(t, roots[0].Home, "the session's own checkout comes first")
	assert.Equal(t, "ci", roots[1].Name)

	rec := do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	var paths []string
	for _, file := range diff.Files {
		paths = append(paths, file.Path)
	}
	assert.Contains(t, paths, "ci.yml", "the diff should be the other repository's")
	assert.NotContains(t, paths, "a.go")

	// A root sitting on its own main branch with nothing ahead of its base
	// shows nothing under branch scope, so it opens on what is uncommitted.
	assert.Equal(t, "uncommitted", s.space().opts.Scope)
}

func TestSwitchingRootLeavesTheAgentWhereItWas(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	home, docs := s.space().homeRoot, s.docsDir

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	assert.Equal(t, home, s.agent.dir, "the agent must not follow the diff")
	assert.Equal(t, docs, s.docsDir, "design documents stay with the session")
	assert.Equal(t, home, s.space().homeRoot)
	assert.Equal(t, other, s.space().viewRoot)
}

func TestFixingAForeignFindingIsRefused(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/checks/fix", `{}`},
		{"POST", "/api/drift", `{}`},
		{"POST", "/api/pr-create", `{}`},
		{"PUT", "/api/pr", `{"body":"x"}`},
		{"POST", "/api/worktrees/prune", `{}`},
	} {
		rec := do(t, s, route.method, route.path, route.body)
		assert.Equalf(t, http.StatusConflict, rec.Code, "%s %s", route.method, route.path)
		assert.Contains(t, rec.Body.String(), "ci", "the refusal should name the root on screen")
	}
	assert.False(t, s.agent.State().Running, "no cold run should have started")

	// And they work again on the way back.
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":""}`).Code)
	assert.NotEqual(t, http.StatusConflict, do(t, s, "POST", "/api/pr-create", `{}`).Code)
}

// TestResolvingAForeignFindingEditsThatRepository is the case an earlier draft
// of the design refused. The run is a one-shot, so its directory is an
// argument, and the threads it answers are that repository's own.
func TestResolvingAForeignFindingEditsThatRepository(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)
	createThread(t, s, `{"scope":"repo","body":"the publish step runs before the tests"}`)

	var got bridge.OneShotOpts
	done := make(chan struct{})
	was := runOneShot
	runOneShot = func(_ context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
		got = opts
		close(done)
		return &bridge.Turn{Text: "done"}, nil
	}
	t.Cleanup(func() { runOneShot = was })

	rec := do(t, s, "POST", "/api/resolve", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	<-done

	assert.Equal(t, other, got.Dir, "the fix belongs in the repository the comment is about")
	assert.Contains(t, got.SystemPrompt, other, "and it is told it is not the session's")
}

// TestGitAndReviewFollowTheDiffRoot holds the other half of the rule: what
// takes a directory acts on the one on screen rather than being refused.
func TestGitAndReviewFollowTheDiffRoot(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	t.Run("staging stages the other repository", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/stage", `{"paths":["ci.yml"]}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		assert.Contains(t, gitOut(t, other, "diff", "--staged", "--name-only"), "ci.yml")
		assert.Empty(t, gitOut(t, s.space().homeRoot, "diff", "--staged", "--name-only"),
			"the session's own index must not move")
	})

	t.Run("committing commits it there", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/commit", `{"title":"wire up publish"}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		assert.Contains(t, gitOut(t, other, "log", "-1", "--format=%s"), "wire up publish")
		assert.NotContains(t, gitOut(t, s.space().homeRoot, "log", "-1", "--format=%s"), "wire up publish")
	})

	t.Run("a review is accepted and reads the other repository", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(other, "ci.yml"), []byte("steps: [test]\n"), 0o600))
		require.NoError(t, s.refresh(context.Background()))

		rec := do(t, s, "POST", "/api/review", `{"level":"medium"}`)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		t.Cleanup(func() { s.agent.Cancel() })
	})
}

// TestAReviewerThatCanWriteIsRefusedAwayFromHome keeps the one agent case the
// read-only argument does not cover.
func TestAReviewerThatCanWriteIsRefusedAwayFromHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	defineUserAgent(t, "fixit", "---\nname: fixit\ntools: Read, Edit\n---\n\nFix things.\n")
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	rec := do(t, s, "POST", "/api/review", `{"agents":["fixit"]}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "can edit a checkout")
}

func TestThreadsFiledAwayFromHomeLandInThatRepository(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	homeStore := s.space().store.Path()
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	thread := createThread(t, s, `{"scope":"repo","body":"this workflow is wrong"}`)
	require.NotEmpty(t, thread.ID)

	path := s.space().store.Path()
	assert.Contains(t, path, filepath.Join(other, ".git"), "the thread belongs to the repository it is about")

	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "this workflow is wrong")

	_, err = os.Stat(homeStore)
	assert.True(t, os.IsNotExist(err), "the home store should be untouched")
}

func TestSandboxFileWrittenAfterStartGrantsNoRoot(t *testing.T) {
	other := otherRepo(t, "ci")
	s := newTestServer(t)
	s.snapshotRoots(context.Background())
	require.Empty(t, s.currentRoots(), "nothing is mounted yet")

	// The agent can write this file mid-turn. Nothing reads it until asked to.
	mount(t, s, sandbox.Path{Path: other, RW: true})

	rec := do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Empty(t, s.space().viewRoot)
	assert.Empty(t, s.currentRoots())

	// Saving the mounts is a user act, and is what lets it in.
	s.snapshotRoots(context.Background())
	assert.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)
}

func TestRootsResolveThroughSymlinks(t *testing.T) {
	other := otherRepo(t, "ci")
	link := filepath.Join(t.TempDir(), "ci-link")
	require.NoError(t, os.Symlink(other, link))

	s := newTestServer(t)
	mount(t, s, sandbox.Path{Path: other, RW: true}, sandbox.Path{Path: link, RW: true})
	s.snapshotRoots(context.Background())

	assert.Len(t, s.currentRoots(), 2, "a symlink and its target are one repository")

	// Either spelling selects it.
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+link+`"}`).Code)
	assert.Equal(t, resolveRoot(other), s.space().viewRoot)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":""}`).Code)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)
	assert.Equal(t, resolveRoot(other), s.space().viewRoot)
}

func TestOnlyReadWriteGitRootsAreOffered(t *testing.T) {
	repo := otherRepo(t, "ci")
	plain := t.TempDir()
	readOnly := otherRepo(t, "docs")

	s := newTestServer(t)
	mount(t, s,
		sandbox.Path{Path: repo, RW: true},
		sandbox.Path{Path: plain, RW: true},
		sandbox.Path{Path: readOnly},
	)
	s.snapshotRoots(context.Background())

	var names []string
	for _, root := range s.currentRoots() {
		names = append(names, root.Name)
	}
	assert.Equal(t, []string{filepath.Base(s.space().homeRoot), "ci"}, names)
}

func TestEachRootCarriesItsOwnCounts(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)

	store, err := comments.Open(filepath.Join(other, ".git"), "main")
	require.NoError(t, err)
	_, err = store.Add(comments.Thread{
		Scope:    comments.ScopeRepo,
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "look at this"}},
	})
	require.NoError(t, err)
	s.refreshRoots(context.Background())

	found := s.currentRoots()[1]
	assert.Equal(t, "ci", found.Name)
	assert.Equal(t, "main", found.Branch)
	assert.Equal(t, 1, found.Changes, "ci.yml is edited and not committed")
	assert.Equal(t, 1, found.Threads)
}

func TestDirtyCheckDoesNotRunOnTheStatePoll(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)

	// Committed away, so a fresh read would report nothing pending.
	cmd := exec.Command("git", "commit", "-aqm", "done")
	cmd.Dir = other
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))

	require.NoError(t, s.refresh(context.Background()))
	require.Equal(t, http.StatusOK, do(t, s, "GET", "/api/state", "").Code)
	assert.Equal(t, 1, s.currentRoots()[1].Changes, "the poll must not re-read another root")

	s.refreshRoots(context.Background())
	assert.Equal(t, 0, s.currentRoots()[1].Changes, "a settled turn is what re-reads it")
}

func TestARunInFlightHoldsTheDiffWhereItIs(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	t.Cleanup(func() { s.agent.Cancel() })

	rec := do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "running")
	assert.Empty(t, s.space().viewRoot, "a review in flight is filing against this diff")
}

func TestTheBranchPollDoesNotImportIntoAnotherRepository(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	// The pull request is the home checkout's, and this runs on a timer.
	n := s.importComments([]ghpr.Thread{{Root: ghpr.ReviewComment{
		ID: 7, Path: "a.go", Side: comments.SideRight, Line: 3, Body: "upstream",
	}}})
	assert.Zero(t, n)
	assert.Empty(t, s.space().store.List(), "nothing of home's lands in ci")
}

func TestFindingsLandInTheRepositoryTheyAreAbout(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	homeStore := s.space().store.Path()
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	filed := s.fileFindings(
		[]review.Finding{{File: "ci.yml", Line: 1, Severity: "high", Category: "correctness", Headline: "publish runs first"}},
		comments.AuthorAgent, func(string) {})

	require.Len(t, filed, 1)
	assert.Contains(t, s.space().store.Path(), filepath.Join(other, ".git"))

	_, err := os.Stat(homeStore)
	assert.True(t, os.IsNotExist(err), "the session's own store is untouched")
}

func TestAReviewAwayFromHomeIsToldWhereItIs(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)

	// At home it is the session's design and the session's memory.
	assert.Empty(t, s.elsewhereNote())
	assert.Equal(t, s.currentDocPath(), s.reviewDocPath())

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	note := s.elsewhereNote()
	assert.Contains(t, note, other)
	assert.Contains(t, note, "not the repository this ade session is working in")

	// The session's design document is not what a change in ci implements.
	assert.Empty(t, s.reviewDocPath())
	assert.Equal(t, note, s.sideProject(), "and its project memory is not ci's either")
}

func TestTheColdReviewerCarriesTheNote(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/diff/root", `{"root":"`+other+`"}`).Code)

	var got bridge.OneShotOpts
	was := runOneShot
	runOneShot = func(_ context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
		got = opts
		return &bridge.Turn{Text: "[]"}, nil
	}
	t.Cleanup(func() { runOneShot = was })

	_, err := s.reviewOnce(context.Background(), "medium")
	require.NoError(t, err)

	assert.Equal(t, other, got.Dir, "it reads the repository the diff is of")
	assert.Contains(t, got.SystemPrompt, other)
	// The prompt has to keep starting with the slash command.
	assert.True(t, strings.HasPrefix(got.Prompt, "/code-review"), got.Prompt[:40])
}

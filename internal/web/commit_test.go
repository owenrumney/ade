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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/outgoing"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func stage(t *testing.T, root, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(body), 0o600))

	cmd := exec.Command("git", "add", name)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git add: %s", out)
}

func TestCommitMessageRequiresStagedChanges(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/commit-message", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing is staged")
	assert.False(t, s.agent.State().Running, "a rejected request must not claim the agent")
}

// Asking while a turn is in flight is the usual case: you stage and describe
// while the agent is still working. It waits its turn rather than refusing.
func TestCommitMessageQueuesBehindARunningTurn(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "a.go", "package main\n\nvar x = 3\n")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/commit-message", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	waiting := s.agentState().Waiting
	require.Len(t, waiting, 1)
	assert.Equal(t, "Commit message", waiting[0].Kind)
	assert.Equal(t, "write a commit message", waiting[0].What, "a queue of bare kinds says nothing")
}

// Nothing staged is still refused outright: the queue is for a busy agent,
// not for a request that could never run.
func TestCommitMessageWithNothingStagedDoesNotQueue(t *testing.T) {
	s := newTestServer(t)

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/commit-message", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, s.agentState().Waiting)
}

// The index moves while the agent works, so the message describes what is
// staged when it is written rather than when it was asked for.
func TestAQueuedCommitMessageWithNothingLeftStagedLetsTheQueueGoOn(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "a.go", "package main\n\nvar x = 3\n")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	require.Equal(t, http.StatusAccepted, do(t, s, "POST", "/api/commit-message", `{}`).Code)

	unstage(t, s.ws.homeRoot)
	s.agent.finish(nil)
	s.drain()

	assert.Empty(t, s.agentState().Waiting, "the entry ran and left")
	assert.False(t, s.agent.State().Running, "an empty index must not leave the agent claimed")
}

func unstage(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "reset")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git reset: %s", out)
}

func TestDraftIsExposedInState(t *testing.T) {
	s := newTestServer(t)

	t.Run("absent until a run produces one", func(t *testing.T) {
		var got stateResponse
		require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/state", "").Body.Bytes(), &got))
		assert.Nil(t, got.Draft)
	})

	t.Run("survives a reload once set", func(t *testing.T) {
		s.setDraft(&CommitDraft{
			Title:  "fix(cart): stop the nil map panic [ABC-1]",
			Body:   "Index wrote to a nil map.",
			Staged: []string{"internal/cart/cart.go"},
		})

		var got stateResponse
		require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/state", "").Body.Bytes(), &got))
		require.NotNil(t, got.Draft)
		assert.Equal(t, "fix(cart): stop the nil map panic [ABC-1]", got.Draft.Title)
		assert.Equal(t, "Index wrote to a nil map.", got.Draft.Body)
		assert.Equal(t, []string{"internal/cart/cart.go"}, got.Draft.Staged)
	})

	t.Run("an unparsed reply is kept so the modal can show it", func(t *testing.T) {
		s.setDraft(&CommitDraft{Staged: []string{"a.go"}, Raw: "I could not find any staged changes."})

		var got stateResponse
		require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/state", "").Body.Bytes(), &got))
		require.NotNil(t, got.Draft)
		assert.Empty(t, got.Draft.Title)
		assert.Contains(t, got.Draft.Raw, "could not find")
	})
}

func TestStagedFilesReflectsTheIndex(t *testing.T) {
	s := newTestServer(t)
	// Point at a binary that cannot exist so the accepted run fails immediately
	// instead of spawning a real agent from a test.
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")

	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")

	rec := do(t, s, "POST", "/api/commit-message", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"staged":1`)

	require.Eventually(t, func() bool {
		return !s.agent.State().Running
	}, 5*time.Second, 20*time.Millisecond, "the failed run should release the agent")
	assert.NotEmpty(t, s.agent.State().Error)
}

func TestCommitRequiresATitleAndStagedFiles(t *testing.T) {
	s := newTestServer(t)

	t.Run("no title", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/commit", `{"title":"  ","body":"x"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "needs a title")
	})

	t.Run("nothing staged", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/commit", `{"title":"chore: nothing"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "nothing is staged")
	})
}

func TestCommitWritesACommit(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n\nvar y = 1\n")

	rec := do(t, s, "POST", "/api/commit",
		`{"title":"feat(b): add y [ABC-1]","body":"Needed a y.\n\nSo here it is."}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"files":1`)

	subject := gitOut(t, s.ws.homeRoot, "log", "-1", "--format=%s")
	body := gitOut(t, s.ws.homeRoot, "log", "-1", "--format=%b")
	assert.Equal(t, "feat(b): add y [ABC-1]", subject)
	assert.Equal(t, "Needed a y.\n\nSo here it is.", body)

	assert.Empty(t, gitOut(t, s.ws.homeRoot, "diff", "--staged", "--name-only"), "the index should be clean")
	assert.Nil(t, s.currentDraft(), "the draft is consumed by committing")
}

func TestCommitOnlyIncludesTheIndex(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "staged.go", "package main\n")
	// Unstaged work must be left behind, so no -a.
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "loose.go"), []byte("package main\n"), 0o600))

	rec := do(t, s, "POST", "/api/commit", `{"title":"chore: staged only"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	files := gitOut(t, s.ws.homeRoot, "show", "--name-only", "--format=", "HEAD")
	assert.Equal(t, "staged.go", files)
	assert.Contains(t, gitOut(t, s.ws.homeRoot, "status", "--porcelain"), "loose.go")
}

func TestCommitMessageKeepsMarkdownAndDropsAIAttribution(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
		gone string
	}{
		{
			name: "claude co-author is dropped",
			body: "Real reason.\n\nCo-Authored-By: Claude <noreply@anthropic.com>",
			want: "Real reason.",
			gone: "Claude",
		},
		{
			name: "generated-with line is dropped",
			body: "Real reason.\n\nGenerated with Claude Code",
			want: "Real reason.",
			gone: "Generated with",
		},
		{
			name: "a human co-author is kept",
			body: "Paired on this.\n\nCo-Authored-By: Alice <alice@example.com>",
			want: "Co-Authored-By: Alice <alice@example.com>",
		},
		{
			name: "a markdown heading survives cleanup",
			body: "# Why\n\nBecause.",
			want: "# Why",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := message("fix: thing", tt.body)
			assert.Contains(t, got, tt.want)
			if tt.gone != "" {
				assert.NotContains(t, got, tt.gone)
			}
		})
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err, "git %v", args)
	return strings.TrimSpace(string(out))
}

func TestStageAddsAnUnstagedFile(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "b.go"), []byte("package main\n"), 0o600))
	require.NoError(t, s.refresh(context.Background()))

	rec := do(t, s, "POST", "/api/stage", `{"paths":["b.go"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	staged, err := gitdiff.StagedFiles(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.Equal(t, []string{"b.go"}, staged)
}

func TestStageRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "no paths", body: `{"paths":[]}`, want: "no files selected"},
		{name: "a path with nothing unstaged", body: `{"paths":["a.go"]}`, want: "no unstaged changes"},
		{name: "a path outside the repo", body: `{"paths":["../escape.go"]}`, want: "no unstaged changes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			rec := do(t, s, "POST", "/api/stage", tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
		})
	}
}

func TestUnstageTakesAFileBackOutOfTheIndex(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "b.go"), []byte("package main\n"), 0o600))
	require.NoError(t, s.refresh(context.Background()))
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/stage", `{"paths":["b.go"]}`).Code)

	rec := do(t, s, "POST", "/api/unstage", `{"paths":["b.go"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	staged, err := gitdiff.StagedFiles(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.Empty(t, staged, "the index is back to empty")

	// Unstaging is not discarding: the file is still there to stage again.
	status, err := gitdiff.WorktreeStatus(context.Background(), s.ws.homeRoot)
	require.NoError(t, err)
	assert.Contains(t, changed(status.Unstaged), "b.go")
}

func TestUnstageRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "no paths", body: `{"paths":[]}`, want: "no files selected to unstage"},
		{name: "a path with nothing staged", body: `{"paths":["a.go"]}`, want: "no staged changes"},
		{name: "a path outside the repo", body: `{"paths":["../escape.go"]}`, want: "no staged changes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			rec := do(t, s, "POST", "/api/unstage", tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
		})
	}
}

// An unstaged file cannot be unstaged, and a staged one cannot be staged: the
// two sides of the index reject each other's paths rather than shelling out.
func TestEachSideOfTheIndexRefusesTheOthersPaths(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "b.go"), []byte("package main\n"), 0o600))
	require.NoError(t, s.refresh(context.Background()))

	rec := do(t, s, "POST", "/api/unstage", `{"paths":["b.go"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "b.go has no staged changes")

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/stage", `{"paths":["b.go"]}`).Code)
	rec = do(t, s, "POST", "/api/stage", `{"paths":["b.go"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "b.go has no unstaged changes")
}

func changed(changes []gitdiff.Change) []string {
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	return paths
}

func TestPushFailsWithoutARemote(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/push", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "git push failed")
}

func TestStateCarriesTheWorktree(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "b.go"), []byte("package main\n"), 0o600))
	require.NoError(t, s.refresh(context.Background()))

	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body stateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotNil(t, body.Worktree)
	require.Len(t, body.Worktree.Unstaged, 1)
	assert.Equal(t, "b.go", body.Worktree.Unstaged[0].Path)
	assert.Equal(t, "??", body.Worktree.Unstaged[0].Status)
	assert.Empty(t, body.Worktree.Upstream)
}

func TestAmendNeedsSomethingToAmend(t *testing.T) {
	s := newTestServer(t)

	// The test repository has commits, so make the other case: nothing staged
	// and no new message is nothing to fold in.
	rec := do(t, s, "POST", "/api/commit", `{"amend":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing to amend")
}

func TestANewCommitStillNeedsATitleAndAnIndex(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/commit", `{"body":"just a body"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "needs a title")

	rec = do(t, s, "POST", "/api/commit", `{"title":"nothing staged"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing is staged")
}

func TestAmendingOnlyTheMessageIsAllowed(t *testing.T) {
	s := newTestServer(t)

	// Nothing staged, but a new message: that is a legitimate amend, and the
	// index check must not refuse it.
	rec := do(t, s, "POST", "/api/commit", `{"title":"a better subject","amend":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"amended":true`)

	head, err := gitdiff.HeadMessage(context.Background(), s.space().homeRoot)
	require.NoError(t, err)
	assert.Equal(t, "a better subject", head)
}

func TestTheHeadMessageIsOfferedForEditing(t *testing.T) {
	s := newTestServer(t)

	// Amending with an edit starts from what the commit says now, not blank.
	rec := do(t, s, "GET", "/api/commit/head", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"title":"change"`)
}

// Describing a diff is the same size of job as rewriting a note, and this one
// inherited whatever the conversation was on — Opus, for a sentence.
func TestTheCommitMessageRunsOnTheFastModel(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	var opts bridge.OneShotOpts
	old := runOneShot
	runOneShot = func(_ context.Context, got bridge.OneShotOpts) (*bridge.Turn, error) {
		opts = got
		return &bridge.Turn{Text: "```ade-commit\nfeat: a thing\n```"}, nil
	}
	t.Cleanup(func() { runOneShot = old })
	s.agent.mu.Lock()
	s.agent.model = "claude-opus-5"
	s.agent.mu.Unlock()

	_ = s.commitMessageOnce(context.Background(), []string{"a.go"})

	assert.Equal(t, outgoing.FastModel, opts.Model, "not the conversation's model")
	assert.True(t, opts.NoMCP, "it reads the diff with git and needs nothing else")
	assert.Contains(t, opts.AllowedTools, "Bash", "it still reads the staged diff")
}

func TestBranchStartsOneAndCarriesTheWork(t *testing.T) {
	s := newTestServer(t)
	stage(t, s.ws.homeRoot, "b.go", "package main\n")

	rec := do(t, s, "POST", "/api/branch", `{"name":"feat/thing"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"branch":"feat/thing"`)

	assert.Equal(t, "feat/thing", gitOut(t, s.ws.homeRoot, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.Equal(t, "b.go", gitOut(t, s.ws.homeRoot, "diff", "--staged", "--name-only"),
		"the staged work should come with the branch")
}

func TestBranchRejectsBadNames(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: `{"name":"  "}`, want: "name the branch"},
		{name: "a space", body: `{"name":"two words"}`, want: "not a branch name"},
		{name: "an option", body: `{"name":"--force"}`, want: "not a branch name"},
		{name: "not a ref", body: `{"name":"feat/.."}`, want: "not a branch name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			rec := do(t, s, "POST", "/api/branch", tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.want)
		})
	}
}

func TestBranchRefusesANameAlreadyTaken(t *testing.T) {
	s := newTestServer(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/branch", `{"name":"taken"}`).Code)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/branch", `{"name":"other"}`).Code)

	rec := do(t, s, "POST", "/api/branch", `{"name":"taken"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "already exists")
}

// git runs a command that changes the test repository.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// Without a --base and without a pull request the base was empty, so the count
// was never taken and Rebase read "up to date" however far behind you were.
func TestBehindIsCountedWithNoBaseConfigured(t *testing.T) {
	s := newTestServer(t)
	require.Empty(t, s.ws.opts.Base, "the usual case: nothing set the base")

	// Move main on, so the branch is genuinely behind it.
	gitRun(t, s.ws.homeRoot, "checkout", "-q", "main")
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "c.go"), []byte("package main\n"), 0o600))
	gitRun(t, s.ws.homeRoot, "add", "c.go")
	gitRun(t, s.ws.homeRoot, "commit", "-qm", "main moves on", "--no-gpg-sign")
	gitRun(t, s.ws.homeRoot, "checkout", "-q", "feature")

	s.refreshWorktree(context.Background())

	require.NotNil(t, s.currentWorktree())
	assert.Equal(t, 1, s.currentWorktree().Behind, "a branch one commit behind main")
	assert.Equal(t, "main", s.currentBase(), "the button has to name what it rebases onto")
}

// An explicit base is still the one used, and costs no lookup.
func TestAConfiguredBaseWins(t *testing.T) {
	s := newTestServer(t)
	s.ws.opts.Base = "main"

	s.refreshWorktree(context.Background())

	assert.Equal(t, "main", s.currentBase())
}

// The label and the count come from one answer, so they cannot disagree.
func TestTheResolvedBaseReachesTheState(t *testing.T) {
	s := newTestServer(t)
	s.refreshWorktree(context.Background())

	var got stateResponse
	require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/state", "").Body.Bytes(), &got))
	assert.Equal(t, "main", got.Meta.Base, "an empty base drew \"Rebase onto the base branch\"")
}

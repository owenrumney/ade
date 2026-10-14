package worktrees

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/session"
)

// commit writes a file in dir and commits it, so a checkout can be moved on.
func commit(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", name)
}

// resultFor picks one checkout's line out of a report.
func resultFor(t *testing.T, report Report, path string) Result {
	t.Helper()
	for _, res := range report.Results {
		if SamePath(res.Path, path) {
			return res
		}
	}
	t.Fatalf("%s is not in the report", path)
	return Result{}
}

func TestAWorktreeBehindItsBaseSaysSo(t *testing.T) {
	root := repo(t)
	path := add(t, root, "behind")
	commit(t, root, "later.go", "package main\n")

	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.Equal(t, 1, wt.Behind)
	assert.Empty(t, wt.Keep, "being behind is not a reason to keep anything")
	assert.True(t, wt.Removable)
}

func TestAMergedWorktreeIsStillRemovableWhenBehind(t *testing.T) {
	root := repo(t)
	path := add(t, root, "done")
	commit(t, path, "b.go", "package main\n")
	git(t, root, "merge", "-q", "done")
	commit(t, root, "later.go", "package main\n")

	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.Positive(t, wt.Behind)
	assert.True(t, wt.Merged)
	assert.True(t, wt.Removable)
}

func TestAReviewWorktreeIsNeverBehind(t *testing.T) {
	root := repo(t)
	commit(t, root, "later.go", "package main\n")

	path, err := gitdiff.ReviewWorktreePath(context.Background(), root, "pr-1")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	git(t, root, "worktree", "add", "-q", "--detach", path, "HEAD~1")

	wt := find(t, must(Scan(context.Background(), root, false))(t), path)
	assert.Equal(t, KindReview, wt.Kind)
	assert.Zero(t, wt.Behind, "a detached review head has no base to be behind")
}

func TestSyncRebasesACleanWorktreeThatIsBehind(t *testing.T) {
	root := repo(t)
	path := add(t, root, "fix")
	commit(t, path, "mine.go", "package main\n")
	commit(t, root, "theirs.go", "package main\n")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRebased, resultFor(t, report, path).Outcome)

	// Rebased, so the base's commit is now an ancestor of the worktree's.
	behind, err := gitdiff.CommitsBehind(context.Background(), path, "main")
	require.NoError(t, err)
	assert.Zero(t, behind)
	assert.FileExists(t, filepath.Join(path, "theirs.go"))
	assert.FileExists(t, filepath.Join(path, "mine.go"))
}

func TestSyncLeavesADirtyWorktreeAloneAndSaysWhy(t *testing.T) {
	root := repo(t)
	path := add(t, root, "messy")
	require.NoError(t, os.WriteFile(filepath.Join(path, "scratch.go"), []byte("package main\n"), 0o600))
	commit(t, root, "theirs.go", "package main\n")
	before, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	res := resultFor(t, report, path)
	assert.Equal(t, OutcomeSkipped, res.Outcome)
	assert.Contains(t, res.Detail, "uncommitted")

	after, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestSyncAbortsOnConflictAndLeavesHeadWhereItWas(t *testing.T) {
	root := repo(t)
	path := add(t, root, "clash")
	commit(t, path, "a.go", "package mine\n")
	commit(t, root, "a.go", "package theirs\n")
	before, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	res := resultFor(t, report, path)
	assert.Equal(t, OutcomeConflicts, res.Outcome)

	after, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "an aborted rebase leaves the worktree exactly as it was")
	assert.NoDirExists(t, filepath.Join(path, ".git", "rebase-merge"))
}

func TestSyncFastForwardsTheMainCheckoutOnlyWhenClean(t *testing.T) {
	root := repo(t)
	// A second clone standing in for origin, so the main checkout has
	// somewhere to be behind.
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, root, "clone", "--bare", "-q", root, bare)
	git(t, root, "remote", "add", "origin", bare)
	git(t, root, "fetch", "-q", "origin")

	elsewhere := filepath.Join(t.TempDir(), "other")
	git(t, root, "clone", "-q", bare, elsewhere)
	commit(t, elsewhere, "theirs.go", "package main\n")
	git(t, elsewhere, "push", "-q", "origin", "main")
	git(t, root, "branch", "--set-upstream-to=origin/main", "main")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	assert.Equal(t, "origin/main", report.Base)
	assert.Equal(t, 1, report.Fetched)
	assert.Equal(t, OutcomeFastForwarded, resultFor(t, report, root).Outcome)
	assert.FileExists(t, filepath.Join(root, "theirs.go"))
}

func TestSyncSkipsAMainCheckoutCarryingWork(t *testing.T) {
	root := repo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "wip.go"), []byte("package main\n"), 0o600))

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	res := resultFor(t, report, root)
	assert.True(t, res.Main)
	assert.Equal(t, OutcomeSkipped, res.Outcome)
	assert.Contains(t, res.Detail, "uncommitted")
}

func TestSyncWithoutAnOriginStillRuns(t *testing.T) {
	root := repo(t)
	path := add(t, root, "local")
	commit(t, root, "theirs.go", "package main\n")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	assert.Equal(t, "main", report.Base, "no origin, so the local branch is the base")
	assert.NotEmpty(t, report.Note)
	assert.Equal(t, OutcomeRebased, resultFor(t, report, path).Outcome)
}

func TestADryRunChangesNothing(t *testing.T) {
	root := repo(t)
	path := add(t, root, "fix")
	commit(t, path, "mine.go", "package main\n")
	commit(t, root, "theirs.go", "package main\n")
	before, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)

	report, err := Sync(context.Background(), root, true)
	require.NoError(t, err)
	assert.Equal(t, OutcomeWouldRebase, resultFor(t, report, path).Outcome)

	after, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// A dry run still fetches: the counts it reports are against origin as it is
// now, and skipping the fetch would make them quietly stale.
func TestADryRunStillFetches(t *testing.T) {
	root := repo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, root, "clone", "--bare", "-q", root, bare)
	git(t, root, "remote", "add", "origin", bare)
	git(t, root, "fetch", "-q", "origin")
	git(t, root, "branch", "--set-upstream-to=origin/main", "main")

	elsewhere := filepath.Join(t.TempDir(), "other")
	git(t, root, "clone", "-q", bare, elsewhere)
	commit(t, elsewhere, "theirs.go", "package main\n")
	git(t, elsewhere, "push", "-q", "origin", "main")

	before := gitdiff.RefSHA(context.Background(), root, "origin/main")
	head, err := gitdiff.HeadSHA(context.Background(), root)
	require.NoError(t, err)

	report, err := Sync(context.Background(), root, true)
	require.NoError(t, err)
	assert.Equal(t, "origin/main", report.Base)
	assert.Equal(t, 1, report.Fetched)
	assert.NotEqual(t, before, gitdiff.RefSHA(context.Background(), root, "origin/main"),
		"the dry run fetched, so origin/main moved")
	assert.Equal(t, OutcomeWouldFastForward, resultFor(t, report, root).Outcome)

	now, err := gitdiff.HeadSHA(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, head, now, "fetching is all a dry run does")
	assert.NoFileExists(t, filepath.Join(root, "theirs.go"))
}

// serving makes a worktree look like one an ade is in: a session record a
// live /healthz answers for, which is what inspect probes.
func serving(t *testing.T, path string) {
	t.Helper()
	gitDir, err := gitdiff.GitDir(context.Background(), path)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(session.Health{Ade: true, Root: path, PID: os.Getpid()})
	}))
	t.Cleanup(srv.Close)
	require.NoError(t, session.Write(session.Path(gitDir), session.Record{
		PID: os.Getpid(), URL: srv.URL, Root: path,
	}))
}

// The design said only KeepUncommitted blocks a rebase. inspect stops at
// KeepServing without counting edits, so Keep alone would rebase over them.
func TestSyncLeavesADirtyServedWorktreeAlone(t *testing.T) {
	root := repo(t)
	path := add(t, root, "live")
	serving(t, path)
	commit(t, root, "later.go", "package main\n")
	require.NoError(t, os.WriteFile(filepath.Join(path, "a.go"), []byte("edited\n"), 0o600))

	found := must(Scan(context.Background(), root, false))(t)
	require.Equal(t, KeepServing, find(t, found, path).Keep, "the fixture is meant to be serving")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	res := resultFor(t, report, path)
	assert.Equal(t, OutcomeSkipped, res.Outcome)
	assert.Contains(t, res.Detail, "uncommitted")

	body, err := os.ReadFile(filepath.Join(path, "a.go"))
	require.NoError(t, err)
	assert.Equal(t, "edited\n", string(body), "the edit survives the sync")
}

// A clean served worktree is still rebased: it is the one being worked in,
// and unpushed commits are exactly what wants replaying.
func TestSyncStillRebasesACleanServedWorktree(t *testing.T) {
	root := repo(t)
	path := add(t, root, "live-clean")
	serving(t, path)
	commit(t, root, "later.go", "package main\n")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRebased, resultFor(t, report, path).Outcome)
}

// The caller's cancellation is dropped, so a closed tab cannot stop a sync
// between a rebase and the abort that would undo it.
func TestSyncRunsOnAfterItsCallerIsCancelled(t *testing.T) {
	root := repo(t)
	path := add(t, root, "cancelled")
	commit(t, root, "later.go", "package main\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := Sync(ctx, root, false)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRebased, resultFor(t, report, path).Outcome)
}

func TestAFailedFetchSyncsAgainstTheLocalBase(t *testing.T) {
	root := repo(t)
	git(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "nowhere.git"))
	path := add(t, root, "offline")
	commit(t, root, "later.go", "package main\n")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	assert.Contains(t, report.Note, "could not fetch")
	assert.Equal(t, "main", report.Base, "a fetch that failed is no reason to use origin's stale copy")
	assert.Equal(t, OutcomeRebased, resultFor(t, report, path).Outcome)
}

// A sync that ran leaves nothing for the next one: an off-by-one in the
// behind count would show up as a rebase of an already-rebased worktree.
func TestASecondSyncHasNothingLeftToDo(t *testing.T) {
	root := repo(t)
	path := add(t, root, "settled")
	commit(t, path, "mine.go", "package main\n")
	commit(t, root, "theirs.go", "package main\n")

	first, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	require.Equal(t, OutcomeRebased, resultFor(t, first, path).Outcome)
	head, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)

	second, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	assert.Equal(t, OutcomeUpToDate, resultFor(t, second, path).Outcome)
	assert.Equal(t, OutcomeUpToDate, resultFor(t, second, root).Outcome)

	after, err := gitdiff.HeadSHA(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, head, after, "the second sync rebased nothing")
}

// Only the base branch is fast-forwarded: a main checkout parked elsewhere
// would have that branch's tip moved onto the base and the work lost.
func TestSyncLeavesAMainCheckoutOnAnotherBranchAlone(t *testing.T) {
	root := repo(t)
	git(t, root, "branch", "spike")
	commit(t, root, "theirs.go", "package main\n")
	git(t, root, "checkout", "-q", "spike")
	before, err := gitdiff.HeadSHA(context.Background(), root)
	require.NoError(t, err)

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	res := resultFor(t, report, root)
	assert.Equal(t, OutcomeSkipped, res.Outcome)
	assert.Contains(t, res.Detail, "not main")

	after, err := gitdiff.HeadSHA(context.Background(), root)
	require.NoError(t, err)
	assert.Equal(t, before, after, "spike still points where it did")
}

// The gate and the target have to be one ref: Scan counts against origin's
// copy, and a failed fetch leaves the sync rebasing onto the local branch.
func TestAStaleOriginDoesNotDecideWhatAFailedFetchRebases(t *testing.T) {
	root := repo(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	git(t, root, "clone", "--bare", "-q", root, bare)
	git(t, root, "remote", "add", "origin", bare)
	git(t, root, "fetch", "-q", "origin")

	// Behind the local base, level with origin's stale copy of it.
	path := add(t, root, "offline")
	commit(t, root, "theirs.go", "package main\n")
	git(t, root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "nowhere.git"))

	found := must(Scan(context.Background(), root, false))(t)
	require.Zero(t, find(t, found, path).Behind, "the fixture is meant to look level with origin")

	report, err := Sync(context.Background(), root, false)
	require.NoError(t, err)
	require.Equal(t, "main", report.Base)
	assert.Equal(t, OutcomeRebased, resultFor(t, report, path).Outcome)
	assert.FileExists(t, filepath.Join(path, "theirs.go"))
}

func TestSwitchableOffersTheMainCheckoutAndNotAReview(t *testing.T) {
	root := repo(t)
	path := add(t, root, "one")

	found, err := Switchable(context.Background(), root)
	require.NoError(t, err)
	require.Len(t, found, 2)
	assert.True(t, found[0].Main, "the main checkout comes first")
	assert.True(t, SamePath(found[0].Path, root))
	assert.Equal(t, path, found[1].Path)

	_, ok := Find(found, root)
	assert.True(t, ok, "a switch back to main has to be gated in")
}

package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func unstagedPaths(status Status) []string {
	out := make([]string, 0, len(status.Unstaged))
	for _, change := range status.Unstaged {
		out = append(out, change.Path)
	}
	return out
}

func TestWorktreeStatusSeparatesTheIndexFromTheRest(t *testing.T) {
	// testRepo leaves kept.go modified and scratch.go untracked.
	dir := testRepo(t)
	gitIn(t, dir, "add", "kept.go")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kept.go"), []byte("package main\n\nfunc a() { return 1 }\n"), 0o600))

	status, err := WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)

	assert.Equal(t, []string{"kept.go"}, status.Staged)
	assert.ElementsMatch(t, []string{"kept.go", "scratch.go"}, unstagedPaths(status))

	byPath := map[string]string{}
	for _, change := range status.Unstaged {
		byPath[change.Path] = change.Status
	}
	assert.Equal(t, "M", byPath["kept.go"])
	assert.Equal(t, "??", byPath["scratch.go"], "an untracked file is not a modification")
}

func TestWorktreeStatusCountsTheLinesEachPathChanges(t *testing.T) {
	dir := testRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kept.go"), []byte("package main\n\nfunc a() int { return 1 }\n"), 0o600))
	gitIn(t, dir, "add", "kept.go")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kept.go"), []byte("package main\n\nfunc a() int { return 1 }\n\nfunc b() {}\n"), 0o600))

	status, err := WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)

	// The index alone would be +1 −1, which is not what committing it carries.
	assert.Equal(t, Count{Added: 3, Removed: 1}, status.Lines["kept.go"])
	assert.NotContains(t, status.Lines, "scratch.go", "git has no line count for an untracked file")
}

func TestWorktreeStatusCountsARenameUnderItsNewPath(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "mv", "base.go", "moved.go")

	status, err := WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)
	assert.Contains(t, status.Lines, "moved.go")
	assert.NotContains(t, status.Lines, "", "the rename's two path records are not read as a count")
}

func TestWorktreeStatusIsCleanAfterCommitting(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "everything")

	status, err := WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)
	assert.Empty(t, status.Staged)
	assert.Empty(t, status.Unstaged)
}

func TestWorktreeStatusHasNoUpstreamUntilPushed(t *testing.T) {
	status, err := WorktreeStatus(context.Background(), testRepo(t))
	require.NoError(t, err)
	assert.Empty(t, status.Upstream)
	assert.Zero(t, status.Ahead)
}

func TestStage(t *testing.T) {
	dir := testRepo(t)
	require.NoError(t, Stage(context.Background(), dir, []string{"scratch.go"}))

	status, err := WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, []string{"scratch.go"}, status.Staged)
	assert.Equal(t, []string{"kept.go"}, unstagedPaths(status))
}

func TestStageRejectsAnEmptyList(t *testing.T) {
	require.Error(t, Stage(context.Background(), testRepo(t), nil))
}

func TestPushSetsTheUpstreamAndReportsAhead(t *testing.T) {
	origin := t.TempDir()
	gitIn(t, origin, "init", "--bare", "-q", "-b", "main")

	dir := testRepo(t)
	gitIn(t, dir, "remote", "add", "origin", origin)

	upstreamName, err := Push(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, "origin/feature", upstreamName)

	status, err := WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, "origin/feature", status.Upstream)
	assert.Zero(t, status.Ahead, "nothing is unpushed straight after a push")

	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "more")

	status, err = WorktreeStatus(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, 1, status.Ahead)
}

func TestPushFailsWithNoRemote(t *testing.T) {
	_, err := Push(context.Background(), testRepo(t))
	require.Error(t, err)
}

func TestParsePorcelainKeepsTheOldPathOfARename(t *testing.T) {
	entries := parsePorcelain("R  new.go\x00old.go\x00 M other.go\x00")
	require.Len(t, entries, 2)
	assert.Equal(t, "new.go", entries[0].path)
	assert.Equal(t, "old.go", entries[0].from)
	assert.Equal(t, "other.go", entries[1].path)
}

func TestAStagedRenameIsBothHalvesOfTheIndex(t *testing.T) {
	ctx := context.Background()
	dir := testRepo(t)
	gitIn(t, dir, "mv", "base.go", "moved.go")

	status, err := WorktreeStatus(ctx, dir)
	require.NoError(t, err)
	assert.Contains(t, status.Staged, "moved.go")
	assert.Contains(t, status.Staged, "base.go", "the delete of the old path is staged too")

	// Unstaging what is listed has to leave the index clean; the old path left
	// behind would be committed as a deletion with no replacement.
	require.NoError(t, Unstage(ctx, dir, status.Staged))
	status, err = WorktreeStatus(ctx, dir)
	require.NoError(t, err)
	assert.Empty(t, status.Staged)
}

func TestWorktreePath(t *testing.T) {
	tests := []struct {
		name string
		root string
		slug string
		want string
	}{
		{name: "beside the repo", root: "/code/owenrumney/ade", slug: "cart", want: "/code/owenrumney/ade-cart"},
		{name: "trailing separator", root: "/code/ade/", slug: "cart", want: "/code/ade-cart"},
		{name: "nested slug", root: "/code/ade", slug: "cart-redesign", want: "/code/ade-cart-redesign"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WorktreePath(tt.root, tt.slug))
		})
	}
}

func TestAddWorktree(t *testing.T) {
	dir := testRepo(t)
	path := filepath.Join(t.TempDir(), "ade-cart")

	require.NoError(t, AddWorktree(context.Background(), dir, path, "cart"))

	assert.FileExists(t, filepath.Join(path, "base.go"), "the worktree is a real checkout")
	assert.Equal(t, "cart", Branch(context.Background(), path))

	// It branches off HEAD, so the feature commits are already in it and its own
	// diff starts empty.
	assert.FileExists(t, filepath.Join(path, "added.go"))

	status, err := WorktreeStatus(context.Background(), path)
	require.NoError(t, err)
	assert.Empty(t, status.Staged)
	assert.Empty(t, status.Unstaged)
}

func TestAddWorktreeRefusesBadInput(t *testing.T) {
	dir := testRepo(t)
	taken := t.TempDir()

	tests := []struct {
		name   string
		path   string
		branch string
	}{
		{name: "existing path", path: taken, branch: "cart"},
		{name: "no path", branch: "cart"},
		{name: "no branch", path: filepath.Join(t.TempDir(), "x")},
		{name: "branch already exists", path: filepath.Join(t.TempDir(), "y"), branch: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, AddWorktree(context.Background(), dir, tt.path, tt.branch))
		})
	}
}

func TestScopesShowDifferentWork(t *testing.T) {
	// testRepo commits base.go and added.go on feature, then leaves kept.go
	// modified and scratch.go untracked.
	dir := testRepo(t)

	tests := []struct {
		name    string
		scope   string
		want    []string
		notWant []string
	}{
		{
			name:  "branch is everything on top of the base",
			scope: ScopeBranch,
			want:  []string{"base.go", "added.go", "kept.go", "scratch.go"},
		},
		{
			name:    "committed ignores the working tree",
			scope:   ScopeCommitted,
			want:    []string{"base.go", "added.go"},
			notWant: []string{"kept.go", "scratch.go"},
		},
		{
			name:    "uncommitted is only what is pending",
			scope:   ScopeUncommitted,
			want:    []string{"kept.go", "scratch.go"},
			notWant: []string{"base.go", "added.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff, err := Load(context.Background(), Options{Repo: dir, Scope: tt.scope})
			require.NoError(t, err)

			got := paths(diff.Files)
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			for _, notWant := range tt.notWant {
				assert.NotContains(t, got, notWant)
			}
		})
	}
}

func TestCommitsAheadIsTheSameWhateverTheScope(t *testing.T) {
	dir := testRepo(t)

	// Create PR is gated on this. Uncommitted scope resolves the diff spec to
	// HEAD, which must not make the branch look like it has no commits.
	var counts []int
	for _, scope := range Scopes() {
		diff, err := Load(context.Background(), Options{Repo: dir, Scope: scope})
		require.NoError(t, err, scope)
		counts = append(counts, diff.Commits)
	}

	assert.Equal(t, 1, counts[0])
	for i, got := range counts {
		assert.Equalf(t, counts[0], got, "scope %s disagrees", Scopes()[i])
	}
}

func TestValidScope(t *testing.T) {
	for _, scope := range Scopes() {
		assert.True(t, ValidScope(scope), scope)
	}
	for _, scope := range []string{"", "nonsense", "Branch"} {
		assert.False(t, ValidScope(scope), scope)
	}
}

func TestCommonGitDirIsSharedByWorktrees(t *testing.T) {
	main := testRepo(t)
	wt := filepath.Join(t.TempDir(), "repo-fix-123")
	require.NoError(t, AddWorktree(context.Background(), main, wt, "fix-123"))

	ctx := context.Background()
	mainGit, err := GitDir(ctx, main)
	require.NoError(t, err)
	wtGit, err := GitDir(ctx, wt)
	require.NoError(t, err)

	// A worktree keeps its own git dir, which is why anything belonging to the
	// repository as a whole cannot live under it.
	assert.NotEqual(t, mainGit, wtGit)
	assert.Contains(t, wtGit, "worktrees")

	mainCommon, err := CommonGitDir(ctx, main)
	require.NoError(t, err)
	wtCommon, err := CommonGitDir(ctx, wt)
	require.NoError(t, err)

	assert.Equal(t, mainCommon, wtCommon, "the common dir is what they share")
	assert.Equal(t, mainGit, mainCommon, "in an ordinary checkout they are the same")
}

func TestEnsureWorktree(t *testing.T) {
	ctx := context.Background()

	t.Run("makes one when it is not there", func(t *testing.T) {
		dir := testRepo(t)
		path := filepath.Join(t.TempDir(), "repo-fix-623")

		added, err := EnsureWorktree(ctx, dir, path, "fix-623")
		require.NoError(t, err)
		assert.True(t, added)
		assert.Equal(t, "fix-623", Branch(ctx, path))
	})

	t.Run("reuses the one already there", func(t *testing.T) {
		dir := testRepo(t)
		path := filepath.Join(t.TempDir(), "repo-fix-623")
		_, err := EnsureWorktree(ctx, dir, path, "fix-623")
		require.NoError(t, err)

		// Coming back to a ticket has to land in the checkout you left, with
		// whatever work is in it.
		require.NoError(t, os.WriteFile(filepath.Join(path, "wip.go"), []byte("package main"), 0o600))

		added, err := EnsureWorktree(ctx, dir, path, "fix-623")
		require.NoError(t, err)
		assert.False(t, added)
		assert.FileExists(t, filepath.Join(path, "wip.go"))
	})

	t.Run("refuses a path that is something else", func(t *testing.T) {
		dir := testRepo(t)
		occupied := t.TempDir()

		_, err := EnsureWorktree(ctx, dir, occupied, "fix-623")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a checkout")
	})

	t.Run("refuses a checkout on another branch", func(t *testing.T) {
		dir := testRepo(t)
		path := filepath.Join(t.TempDir(), "repo-other")
		_, err := EnsureWorktree(ctx, dir, path, "other")
		require.NoError(t, err)

		_, err = EnsureWorktree(ctx, dir, path, "fix-623")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is on other")
	})
}

func TestIsWorktree(t *testing.T) {
	ctx := context.Background()
	main := testRepo(t)
	wt := filepath.Join(t.TempDir(), "repo-fix-1")
	require.NoError(t, AddWorktree(ctx, main, wt, "fix-1"))

	assert.False(t, IsWorktree(ctx, main), "the checkout that owns the repo is not one")
	assert.True(t, IsWorktree(ctx, wt))
}

func TestWorktrees(t *testing.T) {
	ctx := context.Background()
	main := testRepo(t)
	wt := filepath.Join(t.TempDir(), "repo-fix-1")
	require.NoError(t, AddWorktree(ctx, main, wt, "fix-1"))

	paths, err := Worktrees(ctx, main)
	require.NoError(t, err)
	require.Len(t, paths, 2)
	assert.Contains(t, paths[0], filepath.Base(main), "the main checkout comes first")

	// Listing from inside a worktree sees the same set.
	fromWorktree, err := Worktrees(ctx, wt)
	require.NoError(t, err)
	assert.ElementsMatch(t, paths, fromWorktree)
}

func TestRemoveWorktree(t *testing.T) {
	ctx := context.Background()

	t.Run("removes a clean one", func(t *testing.T) {
		main := testRepo(t)
		wt := filepath.Join(t.TempDir(), "repo-fix-1")
		require.NoError(t, AddWorktree(ctx, main, wt, "fix-1"))

		require.NoError(t, RemoveWorktree(ctx, main, wt))
		assert.NoDirExists(t, wt)

		// The branch survives, which is the point of keeping it.
		out, err := run(ctx, main, "rev-parse", "--verify", "fix-1")
		require.NoError(t, err)
		assert.NotEmpty(t, out)
	})

	t.Run("refuses one with uncommitted work", func(t *testing.T) {
		main := testRepo(t)
		wt := filepath.Join(t.TempDir(), "repo-fix-2")
		require.NoError(t, AddWorktree(ctx, main, wt, "fix-2"))
		require.NoError(t, os.WriteFile(filepath.Join(wt, "wip.go"), []byte("package main"), 0o600))

		require.Error(t, RemoveWorktree(ctx, main, wt), "git will not drop work that exists nowhere else")
		assert.DirExists(t, wt)
	})
}

func TestFetchBaseBringsOriginsBaseUpToDate(t *testing.T) {
	ctx := context.Background()
	origin := t.TempDir()
	gitIn(t, origin, "init", "--bare", "-q", "-b", "main")

	// The repository pushes main, then main moves on somewhere else.
	dir := testRepo(t)
	gitIn(t, dir, "remote", "add", "origin", origin)
	gitIn(t, dir, "push", "-q", "origin", "main")
	assert.Equal(t, "origin/main", RemoteBase(ctx, dir, "main"))
	assert.Equal(t, "origin/main", RemoteBase(ctx, dir, "origin/main"))
	assert.Equal(t, "develop", RemoteBase(ctx, dir, "develop"), "no such remote branch: as given")

	other := t.TempDir()
	gitIn(t, other, "clone", "-q", origin, ".")
	require.NoError(t, os.WriteFile(filepath.Join(other, "elsewhere.go"), []byte("package main\n"), 0o600))
	gitIn(t, other, "add", ".")
	gitIn(t, other, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "moved on")
	gitIn(t, other, "push", "-q", "origin", "main")

	before, _ := run(ctx, dir, "rev-parse", "origin/main")
	fetched, err := FetchBase(ctx, dir, "origin/main")
	require.NoError(t, err)
	assert.Equal(t, "origin/main", fetched)
	after, _ := run(ctx, dir, "rev-parse", "origin/main")
	assert.NotEqual(t, before, after, "origin/main moved")
	tip, _ := run(ctx, other, "rev-parse", "HEAD")
	assert.Equal(t, tip, after)

	// Empty means the repository's default; a bare name works too.
	_, err = FetchBase(ctx, dir, "")
	require.NoError(t, err)
	_, err = FetchBase(ctx, dir, "main")
	require.NoError(t, err)
}

func TestFetchBaseFailsWithNoRemote(t *testing.T) {
	_, err := FetchBase(context.Background(), testRepo(t), "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetch origin/main")
}

// The bug this guards: ade makes a session branch with `worktree add -b x
// origin/main`, git's autoSetupMerge records origin/main as its upstream, and
// a bare push is then refused by push.default simple with "the upstream
// branch of your current branch does not match the name of your current
// branch". Pushing by name sets the upstream right as it goes.
func TestPushWorksWhenTheBranchTracksSomethingElse(t *testing.T) {
	origin := t.TempDir()
	gitIn(t, origin, "init", "--bare", "-q", "-b", "main")

	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o600))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	gitIn(t, dir, "remote", "add", "origin", origin)
	gitIn(t, dir, "push", "-q", "-u", "origin", "main")

	// A worktree ade made before AddWorktreeFrom passed --no-track: the
	// tracking is set by hand here because it can no longer be made this way.
	wt := filepath.Join(t.TempDir(), "wt")
	require.NoError(t, AddWorktreeFrom(context.Background(), dir, wt, "fix-remote", "origin/main"))
	gitIn(t, wt, "branch", "--set-upstream-to=origin/main", "fix-remote")
	require.NoError(t, os.WriteFile(filepath.Join(wt, "b.go"), []byte("package main\n"), 0o600))
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-qm", "work")

	was, _ := upstream(context.Background(), wt)
	require.Equal(t, "origin/main", was, "the branch tracks the wrong thing to begin with")

	name, err := Push(context.Background(), wt)
	require.NoError(t, err)
	assert.Equal(t, "origin/fix-remote", name, "it went to the branch of its own name")

	status, err := WorktreeStatus(context.Background(), wt)
	require.NoError(t, err)
	assert.Zero(t, status.Ahead, "and the ahead count means something now")
}

func TestTracksItsOwnNameReadsTheBranchNotJustTheRemote(t *testing.T) {
	ctx := context.Background()
	origin := t.TempDir()
	gitIn(t, origin, "init", "--bare", "-q", "-b", "main")

	dir := testRepo(t)
	gitIn(t, dir, "remote", "add", "origin", origin)
	assert.False(t, tracksItsOwnName(ctx, dir), "no upstream at all")

	_, err := Push(ctx, dir)
	require.NoError(t, err)
	assert.True(t, tracksItsOwnName(ctx, dir), "origin/feature on feature")

	// A branch whose name contains a slash still compares whole.
	gitIn(t, dir, "checkout", "-qb", "feat/nested")
	gitIn(t, dir, "push", "-q", "-u", "origin", "feat/nested")
	assert.True(t, tracksItsOwnName(ctx, dir))
}

// The root cause, fixed at the source: a branch made from a remote-tracking
// start point must not take that start point as its upstream.
func TestAWorktreeBranchDoesNotTrackWhateverItWasMadeFrom(t *testing.T) {
	origin := t.TempDir()
	gitIn(t, origin, "init", "--bare", "-q", "-b", "main")

	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0o600))
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "base")
	gitIn(t, dir, "remote", "add", "origin", origin)
	gitIn(t, dir, "push", "-q", "-u", "origin", "main")

	wt := filepath.Join(t.TempDir(), "wt")
	require.NoError(t, AddWorktreeFrom(context.Background(), dir, wt, "fresh", "origin/main"))

	name, _ := upstream(context.Background(), wt)
	assert.Empty(t, name, "a session branch starts with no upstream of its own")

	// And the first push gives it the right one, with no special casing.
	require.NoError(t, os.WriteFile(filepath.Join(wt, "b.go"), []byte("package main\n"), 0o600))
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-qm", "work")
	got, err := Push(context.Background(), wt)
	require.NoError(t, err)
	assert.Equal(t, "origin/fresh", got)
}

// Opening a branch by name is three different git calls depending on what is
// already there, and `worktree add -b` refuses a branch the repository has.
func TestEnsureWorktreeForBranch(t *testing.T) {
	ctx := context.Background()

	t.Run("uses the worktree the branch is already checked out in", func(t *testing.T) {
		dir := testRepo(t)
		made := filepath.Join(t.TempDir(), "repo-fix-623")
		_, err := EnsureWorktree(ctx, dir, made, "fix-623")
		require.NoError(t, err)

		path, added, err := EnsureWorktreeForBranch(ctx, dir, "fix-623", nil)
		require.NoError(t, err)
		assert.False(t, added)
		assert.Equal(t, made, path)
	})

	// The case `worktree add -b` cannot do: the branch exists, nothing has it out.
	t.Run("checks out a branch that has no worktree", func(t *testing.T) {
		dir := testRepo(t)
		_, err := run(ctx, dir, "branch", "fix-623")
		require.NoError(t, err)

		path, added, err := EnsureWorktreeForBranch(ctx, dir, "fix-623", nil)
		require.NoError(t, err)
		assert.True(t, added)
		assert.Equal(t, "fix-623", Branch(ctx, path))
	})

	t.Run("makes the branch when the repository has none", func(t *testing.T) {
		dir := testRepo(t)

		path, added, err := EnsureWorktreeForBranch(ctx, dir, "fix-623", nil)
		require.NoError(t, err)
		assert.True(t, added)
		assert.Equal(t, "fix-623", Branch(ctx, path))
	})

	t.Run("needs a branch", func(t *testing.T) {
		_, _, err := EnsureWorktreeForBranch(ctx, testRepo(t), "", nil)
		require.Error(t, err)
	})
}

// A branch name is not a directory name: `feat/ADE-211` would otherwise put
// the worktree inside a `repo-feat` directory, away from its siblings.
func TestAWorktreeForABranchWithASlashSitsBesideTheRepository(t *testing.T) {
	ctx := context.Background()
	dir := testRepo(t)
	_, err := run(ctx, dir, "branch", "feat/ADE-211")
	require.NoError(t, err)

	path, added, err := EnsureWorktreeForBranch(ctx, dir, "feat/ADE-211", nil)
	require.NoError(t, err)
	assert.True(t, added)
	assert.Equal(t, filepath.Dir(dir), filepath.Dir(path))
	assert.Equal(t, filepath.Base(dir)+"-feat-ADE-211", filepath.Base(path))
	assert.Equal(t, "feat/ADE-211", Branch(ctx, path), "the branch keeps its name")
}

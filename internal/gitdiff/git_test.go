package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
)

// testRepo builds a repo with one commit on main, one commit on a branch, an
// uncommitted edit and an untracked file.
func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	git := func(args ...string) {
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
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	git("init", "-b", "main", "-q")
	write("kept.go", "package main\n\nfunc a() {}\n")
	write("base.go", "package main\n\nvar x = 1\n")
	git("add", ".")
	git("commit", "-qm", "base")

	git("checkout", "-qb", "feature")
	write("base.go", "package main\n\nvar x = 2\n")
	write("added.go", "package main\n\nfunc b() {}\n")
	git("add", ".")
	git("commit", "-qm", "feature work")

	write("kept.go", "package main\n\nfunc a() { return }\n") // uncommitted
	write("scratch.go", "package main\n")                     // untracked

	return dir
}

func paths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func find(t *testing.T, files []File, path string) File {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no file %q in %v", path, paths(files))
	return File{}
}

func TestLoadCommittedMode(t *testing.T) {
	dir := testRepo(t)

	diff, err := Load(context.Background(), Options{Repo: dir, Scope: ScopeCommitted})
	require.NoError(t, err)

	assert.Equal(t, ModeCommitted, diff.Mode)
	assert.Equal(t, "feature", diff.Head)
	// Only what is committed on the branch: no uncommitted edit, no untracked file.
	assert.ElementsMatch(t, []string{"base.go", "added.go"}, paths(diff.Files))
	assert.Equal(t, StatusAdded, find(t, diff.Files, "added.go").Status)
}

func TestLoadWorktreeMode(t *testing.T) {
	dir := testRepo(t)

	diff, err := Load(context.Background(), Options{Repo: dir})
	require.NoError(t, err)

	assert.Equal(t, ModeWorktree, diff.Mode)
	assert.Equal(t, "working tree", diff.Head)
	assert.ElementsMatch(t,
		[]string{"base.go", "added.go", "kept.go", "scratch.go"},
		paths(diff.Files))

	untracked := find(t, diff.Files, "scratch.go")
	assert.True(t, untracked.Untracked)
	assert.Equal(t, StatusAdded, untracked.Status)
	assert.Empty(t, untracked.OldPath, "the /dev/null path must not leak through")
	require.Len(t, untracked.Hunks, 1)
	assert.Equal(t, "package main", untracked.Hunks[0].Rows[0].Right.Content)
}

func TestLoadIgnoresGitignoredUntracked(t *testing.T) {
	dir := testRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret.txt\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("shh\n"), 0o600))

	diff, err := Load(context.Background(), Options{Repo: dir})
	require.NoError(t, err)

	assert.NotContains(t, paths(diff.Files), "secret.txt")
	assert.Contains(t, paths(diff.Files), ".gitignore")
}

func TestLoadRevMode(t *testing.T) {
	dir := testRepo(t)

	diff, err := Load(context.Background(), Options{Repo: dir, Rev: "main..feature"})
	require.NoError(t, err)

	assert.Equal(t, ModeRev, diff.Mode)
	assert.Equal(t, "main..feature", diff.Head)
	assert.ElementsMatch(t, []string{"base.go", "added.go"}, paths(diff.Files))
}

func TestLoadExplicitBase(t *testing.T) {
	dir := testRepo(t)

	diff, err := Load(context.Background(), Options{Repo: dir, Base: "main", Scope: ScopeCommitted})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"base.go", "added.go"}, paths(diff.Files))
}

func TestLoadUnknownBaseFails(t *testing.T) {
	dir := testRepo(t)

	_, err := Load(context.Background(), Options{Repo: dir, Base: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no merge base")
}

func TestLoadNotARepo(t *testing.T) {
	_, err := Load(context.Background(), Options{Repo: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestLoadNoChanges(t *testing.T) {
	dir := testRepo(t)
	cmd := exec.Command("git", "checkout", "-q", "main")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())
	require.NoError(t, os.Remove(filepath.Join(dir, "scratch.go")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kept.go"), []byte("package main\n\nfunc a() {}\n"), 0o600))

	diff, err := Load(context.Background(), Options{Repo: dir})
	require.NoError(t, err)
	assert.Empty(t, diff.Files)
}

// noBaseRepo has a single branch that is neither main nor master.
func noBaseRepo(t *testing.T, commit bool) string {
	t.Helper()
	dir := t.TempDir()
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

	git("init", "-b", "trunk", "-q")
	if commit {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n"), 0o600))
		git("add", ".")
		git("commit", "-qm", "first")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\nvar x = 1\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.go"), []byte("package app\n"), 0o600))
	return dir
}

func TestLoadWithNoBaseBranch(t *testing.T) {
	dir := noBaseRepo(t, true)

	diff, err := Load(context.Background(), Options{Repo: dir})
	require.NoError(t, err, "a repo with no base branch must still open")

	assert.Equal(t, ModeWorktree, diff.Mode)
	assert.Contains(t, diff.Note, "no base branch found")
	assert.ElementsMatch(t, []string{"app.go", "new.go"}, paths(diff.Files))
}

func TestLoadWithNoBaseBranchCommitted(t *testing.T) {
	dir := noBaseRepo(t, true)

	diff, err := Load(context.Background(), Options{Repo: dir, Scope: ScopeCommitted})
	require.NoError(t, err)

	assert.Contains(t, diff.Note, "every commit on the branch")
	assert.Contains(t, paths(diff.Files), "app.go", "the branch's own commits show as added")
}

func TestLoadWithNoCommitsAtAll(t *testing.T) {
	dir := noBaseRepo(t, false)

	diff, err := Load(context.Background(), Options{Repo: dir})
	require.NoError(t, err, "a repo with no commits must still open")

	assert.Contains(t, diff.Note, "no commits")
	assert.Contains(t, paths(diff.Files), "new.go")
}

func TestExplicitBaseStillErrorsWhenUnknown(t *testing.T) {
	dir := noBaseRepo(t, true)

	// An explicit --base is a stated intent, so a bad one is an error not a fallback.
	_, err := Load(context.Background(), Options{Repo: dir, Base: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no merge base")
}

func TestParseOrigin(t *testing.T) {
	tests := []struct {
		remote string
		owner  string
		repo   string
	}{
		{"git@github.com:owenrumney/ade.git", "owenrumney", "ade"},
		{"git@github.com:owenrumney/ade", "owenrumney", "ade"},
		{"https://github.com/owenrumney/ade.git", "owenrumney", "ade"},
		{"https://github.com/owenrumney/ade", "owenrumney", "ade"},
		{"ssh://git@github.com/owenrumney/ade.git", "owenrumney", "ade"},
		{"https://user:token@github.com/owenrumney/ade.git", "owenrumney", "ade"},
		// A self-hosted forge can nest an owner under a group; the last two win.
		{"https://gitlab.example.com/group/team/thing.git", "team", "thing"},
		{"git@gitlab.example.com:group/team/thing.git", "team", "thing"},
		{"/srv/git/bare.git", "git", "bare"},
		// Nothing to say is better than something wrong.
		{"", "", ""},
		{"origin", "", ""},
		{"https://github.com", "", ""},
		{"git@github.com:", "", ""},
	}
	for _, tc := range tests {
		owner, repo := parseOrigin(tc.remote)
		assert.Equalf(t, tc.owner, owner, "owner of %q", tc.remote)
		assert.Equalf(t, tc.repo, repo, "repo of %q", tc.remote)
	}
}

// commitRepo is a repository with one commit and a clean tree.
func commitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600))
	git("add", ".")
	git("commit", "-qm", "first")
	return dir
}

func TestAmendKeepsTheMessageOrReplacesIt(t *testing.T) {
	ctx := context.Background()
	dir := commitRepo(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o600))
	stage := exec.Command("git", "add", ".")
	stage.Dir = dir
	require.NoError(t, stage.Run())

	// An empty message keeps what the commit already says.
	_, err := Amend(ctx, dir, "")
	require.NoError(t, err)
	got, err := HeadMessage(ctx, dir)
	require.NoError(t, err)
	assert.Equal(t, "first", got)

	// And it is still one commit, not two.
	out, err := run(ctx, dir, "rev-list", "--count", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(out))

	// A message replaces it.
	_, err = Amend(ctx, dir, "second thoughts\n\nwith a body")
	require.NoError(t, err)
	got, err = HeadMessage(ctx, dir)
	require.NoError(t, err)
	assert.Equal(t, "second thoughts\n\nwith a body", got)
}

func TestCommitStillNeedsAMessage(t *testing.T) {
	dir := commitRepo(t)

	// Only an amend can go without one; a new commit with no message would be
	// git opening an editor that nothing can answer.
	_, err := Commit(context.Background(), dir, "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a message")
}

func TestHasCommits(t *testing.T) {
	ctx := context.Background()
	assert.True(t, HasCommits(ctx, commitRepo(t)))

	empty := t.TempDir()
	init := exec.Command("git", "init", "-b", "main", "-q")
	init.Dir = empty
	require.NoError(t, init.Run())

	// A branch with no commits is where amend has nothing to fold into.
	assert.False(t, HasCommits(ctx, empty))
}

func TestSameBaseComparesResolvedCommits(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		_, err := run(context.Background(), dir, args...)
		require.NoError(t, err)
	}
	git("init", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644))
	git("add", ".")
	git("commit", "-m", "one")
	git("branch", "stack")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644))
	git("add", ".")
	git("commit", "-m", "two")

	ctx := context.Background()
	assert.True(t, SameBase(ctx, dir, "main", ""), "an empty base is the default branch")
	assert.True(t, SameBase(ctx, dir, "main", "main"))
	assert.False(t, SameBase(ctx, dir, "stack", "main"), "different commits are different bases")
	assert.False(t, SameBase(ctx, dir, "nope", "main"), "an unresolvable ref matches nothing")
}

// The panel says which it will be, rather than leaving you to find out.
func TestSigningIsReadFromTheConfig(t *testing.T) {
	dir := t.TempDir()
	// Without this the machine's own global config decides the answer.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "no-such-config"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(dir, "no-such-config"))
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), strings.Join(args, " "))
	}
	run("init", "-q")

	assert.False(t, SigningOf(context.Background(), dir).On, "nothing set is not signing")

	run("config", "commit.gpgsign", "true")
	run("config", "gpg.format", "ssh")
	run("config", "user.signingkey", "~/.ssh/id_ed25519.pub")

	got := SigningOf(context.Background(), dir)
	assert.True(t, got.On)
	assert.Equal(t, "SSH", got.How())
	assert.Equal(t, "~/.ssh/id_ed25519.pub", got.Key)

	// git's booleans are not only "true", and "false" is not signing.
	run("config", "commit.gpgsign", "false")
	assert.False(t, SigningOf(context.Background(), dir).On)
	run("config", "commit.gpgsign", "yes")
	assert.True(t, SigningOf(context.Background(), dir).On)
}

func TestSigningNamesTheFormatInWords(t *testing.T) {
	assert.Equal(t, "GPG", Signing{On: true}.How())
	assert.Equal(t, "GPG", Signing{On: true, Format: "openpgp"}.How())
	assert.Equal(t, "X.509", Signing{On: true, Format: "x509"}.How())
}

func TestBranchesAreTheLocalOnes(t *testing.T) {
	names, err := Branches(context.Background(), testRepo(t))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"main", "feature"}, names)
}

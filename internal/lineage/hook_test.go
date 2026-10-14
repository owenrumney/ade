package lineage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "work"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	return dir
}

func commit(t *testing.T, dir, name, body string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "add " + name}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	sha, err := gitdiff.HeadSHA(context.Background(), dir)
	require.NoError(t, err)
	return sha
}

func TestTheHookQueuesEachTerminalCommit(t *testing.T) {
	dir := repo(t)
	require.NoError(t, InstallHook(context.Background(), dir))

	first := commit(t, dir, "a.txt", "alpha\n")
	second := commit(t, dir, "b.txt", "beta\n")

	gitDir, err := gitdiff.GitDir(context.Background(), dir)
	require.NoError(t, err)
	shas, err := Drain(gitDir)
	require.NoError(t, err)
	assert.Equal(t, []string{first, second}, shas)

	again, err := Drain(gitDir)
	require.NoError(t, err)
	assert.Empty(t, again, "a drained queue is empty")
}

func TestTheHookDoesNotTakeOverSomeoneElses(t *testing.T) {
	dir := repo(t)
	gitDir, err := gitdiff.GitDir(context.Background(), dir)
	require.NoError(t, err)
	theirs := filepath.Join(gitDir, "hooks", "post-commit")
	require.NoError(t, os.MkdirAll(filepath.Dir(theirs), 0o750))
	require.NoError(t, os.WriteFile(theirs, []byte("#!/bin/sh\nexit 0\n"), 0o700))

	err = InstallHook(context.Background(), dir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "post-commit")
	body, readErr := os.ReadFile(theirs)
	require.NoError(t, readErr)
	assert.Equal(t, "#!/bin/sh\nexit 0\n", string(body), "theirs is untouched")
}

func TestInstallingTheHookTwiceIsOneHook(t *testing.T) {
	dir := repo(t)
	require.NoError(t, InstallHook(context.Background(), dir))
	require.NoError(t, InstallHook(context.Background(), dir))

	gitDir, err := gitdiff.GitDir(context.Background(), dir)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(gitDir, "hooks", "post-commit"))
	require.NoError(t, err)
	assert.Equal(t, hookScript, string(body))
}

func TestStampingFollowsACommitMadeInATerminal(t *testing.T) {
	dir := repo(t)
	base := commit(t, dir, "seed.txt", "seed\n")
	require.NoError(t, InstallHook(context.Background(), dir))

	ctx := context.Background()
	gitDir, err := gitdiff.GitDir(ctx, dir)
	require.NoError(t, err)
	store, err := Open(gitDir, "work")
	require.NoError(t, err)

	sha := commit(t, dir, "a.go", "alpha\nbeta\n")
	diff, err := gitdiff.Load(ctx, gitdiff.Options{Repo: dir, Base: base, Scope: gitdiff.ScopeBranch})
	require.NoError(t, err)
	require.NoError(t, store.Add(Entry{Session: "s", Summary: "write alpha",
		Ranges: []Range{rangeAt(t, diff, "a.go", 1, 2)}}))

	shas, err := Drain(gitDir)
	require.NoError(t, err)
	require.Equal(t, []string{sha}, shas)

	added, err := gitdiff.AddedLines(ctx, dir, sha)
	require.NoError(t, err)
	n, err := store.Stamp(diff, sha, added)
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	turns := store.ForCommit(sha, "a.go")
	require.Len(t, turns, 1)
	assert.Equal(t, "write alpha", turns[0].Summary)
}

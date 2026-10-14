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

func TestBlameNamesTheCommitALineCameFrom(t *testing.T) {
	dir := testRepo(t)
	ctx := context.Background()
	head, err := HeadSHA(ctx, dir)
	require.NoError(t, err)

	blame, err := BlameLine(ctx, dir, "added.go", 3)

	require.NoError(t, err)
	assert.True(t, blame.Committed())
	assert.Equal(t, head, blame.SHA)
	assert.Equal(t, head[:7], blame.Short)
	assert.Equal(t, "feature work", blame.Summary)
	assert.Equal(t, "t", blame.Author)
	assert.False(t, blame.At.IsZero())
}

func TestBlameSaysWhenALineIsNotCommittedYet(t *testing.T) {
	blame, err := BlameLine(context.Background(), testRepo(t), "kept.go", 3)

	require.NoError(t, err)
	assert.False(t, blame.Committed(), "the edit is only in the worktree")
}

func TestAddedLinesAreTheCommitsOwnAdditions(t *testing.T) {
	dir := testRepo(t)
	ctx := context.Background()
	head, err := HeadSHA(ctx, dir)
	require.NoError(t, err)

	added, err := AddedLines(ctx, dir, head)

	require.NoError(t, err)
	assert.Equal(t, []string{"var x = 2"}, added["base.go"])
	assert.Equal(t, []string{"package main", "", "func b() {}"}, added["added.go"])
	assert.NotContains(t, added, "kept.go", "the uncommitted edit is not in it")
}

func TestConfigValueIsEmptyWhenUnset(t *testing.T) {
	assert.Empty(t, ConfigValue(context.Background(), testRepo(t), "core.hooksPath"))
}

func TestAGlobalSettingCannotAnswerARepositorysQuestion(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	global := filepath.Join(home, ".gitconfig")
	// Named, not left to HOME: an agent harness pins GIT_CONFIG_GLOBAL, and
	// git reads that in preference to the file this test just wrote.
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	require.NoError(t, os.WriteFile(global,
		[]byte("[ade \"lineage\"]\n\tverbatim = true\n"), 0o600))

	for _, args := range [][]string{{"init", "-q", "-b", "work"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}

	ctx := context.Background()
	assert.Equal(t, "true", ConfigValue(ctx, dir, "ade.lineage.verbatim"), "the chain sees it")
	assert.Empty(t, LocalConfigValue(ctx, dir, "ade.lineage.verbatim"), "this repository never said so")
}

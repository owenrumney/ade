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

// commitAs writes a file and commits it as the given author.
func commitAs(t *testing.T, dir, name, email, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	for _, args := range [][]string{
		{"add", name},
		{"commit", "-qm", "a change", "--no-gpg-sign"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=someone", "GIT_AUTHOR_EMAIL="+email,
			"GIT_COMMITTER_NAME=someone", "GIT_COMMITTER_EMAIL="+email)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
}

func authorRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main", "-q"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	return dir
}

// The handle lookup starts here, so an address that answers for the wrong
// person puts the review on the wrong person.
func TestLastCommitByFindsOnlyThatAddress(t *testing.T) {
	dir := authorRepo(t)
	commitAs(t, dir, "a.txt", "liam@infracost.io", "one\n")
	commitAs(t, dir, "b.txt", "liam.cervante@infracost.io", "two\n")

	ctx := context.Background()

	first, err := LastCommitBy(ctx, dir, "liam@infracost.io")
	require.NoError(t, err)
	require.NotEmpty(t, first)

	second, err := LastCommitBy(ctx, dir, "liam.cervante@infracost.io")
	require.NoError(t, err)
	require.NotEmpty(t, second)

	assert.NotEqual(t, first, second, "two addresses must not answer with one commit")
}

// A dot is a regular expression metacharacter, and a substring match would let
// a longer address answer for a shorter one.
func TestLastCommitByDoesNotMatchALongerAddress(t *testing.T) {
	dir := authorRepo(t)
	commitAs(t, dir, "a.txt", "xliam@infracost.io", "one\n")

	got, err := LastCommitBy(context.Background(), dir, "liam@infracost.io")

	require.NoError(t, err)
	assert.Empty(t, got, "xliam@ is not liam@")
}

func TestLastCommitByIsEmptyForAStranger(t *testing.T) {
	dir := authorRepo(t)
	commitAs(t, dir, "a.txt", "liam@infracost.io", "one\n")

	got, err := LastCommitBy(context.Background(), dir, "newstarter@infracost.io")

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLastCommitByIgnoresAnEmptyAddress(t *testing.T) {
	got, err := LastCommitBy(context.Background(), t.TempDir(), "  ")

	require.NoError(t, err)
	assert.Empty(t, got, "an empty address must not run git at all")
}

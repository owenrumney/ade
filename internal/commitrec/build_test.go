package commitrec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
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

func commit(t *testing.T, dir, name, body, message string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	sha, err := gitdiff.HeadSHA(context.Background(), dir)
	require.NoError(t, err)
	return sha
}

func TestARecordCarriesTheCommitsSubjectAndKey(t *testing.T) {
	dir := repo(t)
	sha := commit(t, dir, "a.go", "package a\n", "feat: add a")

	got, err := Build(context.Background(), Source{
		Repo: "owenrumney/ade", Root: dir, SHA: sha, Branch: "work",
		Turns: []lineage.Turn{{Summary: "add a package", At: time.Now()}},
	})
	require.NoError(t, err)

	assert.Equal(t, "feat: add a", got.Subject)
	assert.NotEmpty(t, got.Key(), "a commit with a diff has a patch-id")
	require.Len(t, got.Asked, 1)
	assert.Equal(t, "add a package", got.Asked[0].Text)
}

func TestTheKeyDoesNotChangeWhenTheCommitIsRebasedCleanly(t *testing.T) {
	dir := repo(t)
	commit(t, dir, "base.go", "package base\n", "base")
	first := commit(t, dir, "a.go", "package a\n", "feat: add a")

	before, err := gitdiff.PatchID(context.Background(), dir, first)
	require.NoError(t, err)

	// A commit on top and a reorder is a rebase that applies cleanly: the same
	// change at a different line, which is exactly what patch-id ignores.
	for _, args := range [][]string{
		{"commit", "-q", "--amend", "--no-edit", "--date", "2020-01-01T00:00:00Z"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		require.NoError(t, cmd.Run(), "git %v", args)
	}
	moved, err := gitdiff.HeadSHA(context.Background(), dir)
	require.NoError(t, err)
	assert.NotEqual(t, first, moved, "the sha changed")

	after, err := gitdiff.PatchID(context.Background(), dir, moved)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the same diff keys the same")
}

func TestVerbatimDecidesWhatLeavesTheLaptopNotWhatWasCaptured(t *testing.T) {
	dir := repo(t)
	sha := commit(t, dir, "a.go", "package a\n", "feat: add a")
	turn := lineage.Turn{Summary: "add a package", Prompt: "add a package, and the api key is hunter2"}

	clipped, err := Build(context.Background(), Source{Repo: "o/r", Root: dir, SHA: sha, Turns: []lineage.Turn{turn}})
	require.NoError(t, err)
	assert.Equal(t, "add a package", clipped.Asked[0].Text, "the default publishes the summary")

	full, err := Build(context.Background(), Source{Repo: "o/r", Root: dir, SHA: sha, Turns: []lineage.Turn{turn}, Verbatim: true})
	require.NoError(t, err)
	assert.Equal(t, turn.Prompt, full.Asked[0].Text)
}

func TestATurnThatWroteTwoRangesInOneCommitIsStillOneAsk(t *testing.T) {
	dir := repo(t)
	sha := commit(t, dir, "a.go", "package a\n", "feat: add a")
	turn := lineage.Turn{Session: "s", MessageID: "m", Summary: "add a"}

	got, err := Build(context.Background(), Source{
		Repo: "o/r", Root: dir, SHA: sha, Turns: []lineage.Turn{turn, turn},
	})
	require.NoError(t, err)
	assert.Len(t, got.Asked, 1)
}

func TestOnlyClaimsAgainstLinesThisCommitAddedAreRecorded(t *testing.T) {
	dir := repo(t)
	sha := commit(t, dir, "a.go", "package a\n", "feat: add a")

	mine := comments.Thread{
		File: "a.go", Line: 1, Side: comments.SideRight,
		Anchor:   comments.Anchor{Content: "package a"},
		Severity: comments.SeverityHigh, Category: "correctness", Resolved: true,
		Comments: []comments.Comment{{Author: comments.AuthorAgent, Body: "wrong package"}},
	}
	elsewhere := mine
	elsewhere.Anchor = comments.Anchor{Content: "package b"}
	whole := comments.Thread{Scope: comments.ScopeRepo, Comments: []comments.Comment{{Body: "no"}}}

	got, err := Build(context.Background(), Source{
		Repo: "o/r", Root: dir, SHA: sha,
		Turns:   []lineage.Turn{{Summary: "add a"}},
		Threads: []comments.Thread{mine, elsewhere, whole},
	})
	require.NoError(t, err)

	require.Len(t, got.Review, 1)
	assert.Equal(t, "a.go", got.Review[0].File)
	assert.Equal(t, comments.FateFixed, got.Review[0].Fate)
	assert.Equal(t, "wrong package", got.Review[0].Text)
}

func TestACommitNoTurnWroteStillBuildsWithNoAsks(t *testing.T) {
	dir := repo(t)
	sha := commit(t, dir, "a.go", "package a\n", "feat: add a")

	got, err := Build(context.Background(), Source{Repo: "o/r", Root: dir, SHA: sha})
	require.NoError(t, err)
	assert.Empty(t, got.Asked)
}

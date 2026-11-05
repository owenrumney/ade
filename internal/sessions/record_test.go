package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/statedir"
)

func record(t *testing.T, gitDir string, held runlog.Record) {
	t.Helper()
	require.NoError(t, runlog.Update(runlog.Path(gitDir), func(r *runlog.Record) { *r = held }))
}

func TestASessionIsListedFromItsRecordAndNothingElse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	main := repo(t, filepath.Join(root, "ade"), "main")
	tree := worktree(t, main, filepath.Join(root, "ade-fix"), "fix-12")
	repo(t, filepath.Join(root, "bare"), "main")

	used := time.Now().Add(-time.Hour).Truncate(time.Second)
	record(t, GitDir(tree), runlog.Record{
		Session: "abc", Provider: "codex", Ticket: "FIX-12", Tags: []string{"auth"}, Used: used,
	})
	require.NoError(t, settings.SetTitle("abc", "the login fix"))
	docs := filepath.Join(statedir.Git(GitDir(tree)), "docs")
	require.NoError(t, os.MkdirAll(docs, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "fix-12.md"), []byte("# brief\n"), 0o600))

	repos := Repos([]string{root}, nil)

	var got []Session
	for _, r := range repos {
		for _, dir := range r.Checkouts {
			got = append(got, dir.Sessions...)
		}
	}
	require.Len(t, got, 1, "a checkout with no record has no session")
	assert.True(t, used.Equal(got[0].LastUsed))
	got[0].LastUsed = used
	assert.Equal(t, Session{
		ID: "abc", Agent: "codex", Dir: tree, Name: "ade-fix", Branch: "fix-12",
		Title: "the login fix", LastUsed: used, Ticket: "FIX-12", Tags: []string{"auth"},
		Brief: filepath.Join(docs, "fix-12.md"), Resumable: true,
	}, got[0])
	assert.Equal(t, main, repos[0].Path, "the repository holding it leads")
	assert.Equal(t, 1, repos[0].Sessions)
}

func TestAnUntaggedSessionAnswersAnEmptyListAndNoBrief(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	main := repo(t, filepath.Join(root, "ade"), "main")
	record(t, GitDir(main), runlog.Record{Session: "abc"})

	dir, ok := FindCheckout(Repos([]string{root}, nil), main)
	require.True(t, ok)
	require.Len(t, dir.Sessions, 1)
	assert.Equal(t, "claude", dir.Sessions[0].Agent)
	assert.NotNil(t, dir.Sessions[0].Tags)
	assert.Empty(t, dir.Sessions[0].Tags)
	assert.Empty(t, dir.Sessions[0].Brief)
	assert.False(t, dir.Sessions[0].Live, "nothing is serving it")
}

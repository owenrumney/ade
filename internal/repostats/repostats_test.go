package repostats

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

// repo is a real git repository, because the whole package is what git says.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Owen", "GIT_AUTHOR_EMAIL=owen@example.com",
			"GIT_COMMITTER_NAME=Owen", "GIT_COMMITTER_EMAIL=owen@example.com",
			"GIT_AUTHOR_DATE=2026-10-13T10:00:00Z", "GIT_COMMITTER_DATE=2026-10-13T10:00:00Z")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "owen@example.com")
	run("config", "user.name", "Owen")
	run("remote", "add", "origin", "git@github.com:owenrumney/ade.git")
	write(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	write(t, filepath.Join(dir, "README.md"), "# ade\n\nnotes\n")
	write(t, filepath.Join(dir, "logo.png"), "\x89PNG\x00\x00binary\x00")
	run("add", ".")
	run("commit", "-qm", "first")
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func TestCollectReadsWhatTheRepositoryIs(t *testing.T) {
	dir := repo(t)
	got, err := Collect(context.Background(), dir, nil, now)
	require.NoError(t, err)

	assert.Equal(t, "owenrumney/ade", got.Repo, "keyed on the remote, as the stats are")
	assert.Equal(t, "main", got.Branch)
	assert.Equal(t, "git@github.com:owenrumney/ade.git", got.Remote)
	assert.Zero(t, got.Dirty)

	t.Run("languages are counted by line, binaries left out", func(t *testing.T) {
		by := map[string]Language{}
		for _, lang := range got.Languages {
			by[lang.Name] = lang
		}
		assert.Equal(t, 3, by["Go"].Lines)
		assert.Equal(t, 1, by["Go"].Files)
		assert.Equal(t, 3, by["Markdown"].Lines)
		assert.NotContains(t, by, "Other", "a png has no line count worth showing")
		assert.Equal(t, 6, got.Lines)
		assert.Equal(t, 2, got.Files)
	})

	t.Run("a year of days, with the quiet ones in it", func(t *testing.T) {
		require.Len(t, got.Days, window)
		assert.Equal(t, "2025-10-15", got.Days[0].Date, "oldest first")
		assert.Equal(t, "2026-10-14", got.Days[window-1].Date)
		assert.Equal(t, 1, got.Commits)
		assert.Equal(t, "2026-10-13", got.BusiestDay)
	})

	t.Run("contributors and the tip", func(t *testing.T) {
		require.Len(t, got.Contributors, 1)
		assert.Equal(t, "Owen", got.Contributors[0].Name)
		assert.Equal(t, 1, got.Contributors[0].Commits)
		require.NotNil(t, got.Last)
		assert.Equal(t, "first", got.Last.Subject)
	})
}

func TestCollectCountsUncommittedFiles(t *testing.T) {
	dir := repo(t)
	write(t, filepath.Join(dir, "new.go"), "package main\n")
	got, err := Collect(context.Background(), dir, nil, now)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Dirty)
}

func TestCollectWithoutARepositoryIsAnError(t *testing.T) {
	_, err := Collect(context.Background(), t.TempDir(), nil, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestABranchWithNoUpstreamIsOrdinary(t *testing.T) {
	got, err := Collect(context.Background(), repo(t), nil, now)
	require.NoError(t, err)
	assert.Empty(t, got.Upstream, "a worktree spends its life this way")
	assert.Zero(t, got.Ahead)
	assert.Zero(t, got.Behind)
}

func TestLanguageOf(t *testing.T) {
	tests := map[string]string{
		"internal/web/web.go": "Go", "vscode/src/chat.ts": "TypeScript",
		"Dockerfile": "Docker", "deploy/Makefile": "Make",
		"notes.md": "Markdown", "a/b/thing.unknown": "Other",
		"SHELL.SH": "Shell",
	}
	for path, want := range tests {
		t.Run(path, func(t *testing.T) {
			assert.Equal(t, want, languageOf(path))
		})
	}
}

package transcripts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHead(t *testing.T, gitDir, head string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(head), 0o600))
}

func TestGitBranch(t *testing.T) {
	t.Run("reads the branch from HEAD", func(t *testing.T) {
		repo := t.TempDir()
		writeHead(t, filepath.Join(repo, ".git"), "ref: refs/heads/feat/fix-448\n")
		assert.Equal(t, "feat/fix-448", GitBranch(repo))
	})

	t.Run("walks up to the repo root", func(t *testing.T) {
		repo := t.TempDir()
		writeHead(t, filepath.Join(repo, ".git"), "ref: refs/heads/main\n")
		nested := filepath.Join(repo, "internal", "ui")
		require.NoError(t, os.MkdirAll(nested, 0o755))
		assert.Equal(t, "main", GitBranch(nested))
	})

	t.Run("shortens a detached head", func(t *testing.T) {
		repo := t.TempDir()
		writeHead(t, filepath.Join(repo, ".git"), "9f2c1ab3d4e5f60718293a4b5c6d7e8f90a1b2c3\n")
		assert.Equal(t, "9f2c1ab", GitBranch(repo))
	})

	t.Run("follows a worktree gitdir pointer", func(t *testing.T) {
		root := t.TempDir()
		real := filepath.Join(root, "store", "worktrees", "wt")
		writeHead(t, real, "ref: refs/heads/wt-branch\n")

		tree := filepath.Join(root, "tree")
		require.NoError(t, os.MkdirAll(tree, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+real+"\n"), 0o600))

		assert.Equal(t, "wt-branch", GitBranch(tree))
	})

	t.Run("returns nothing outside a repo", func(t *testing.T) {
		assert.Empty(t, GitBranch(t.TempDir()))
		assert.Empty(t, GitBranch(""))
	})
}

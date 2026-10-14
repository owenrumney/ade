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

func writeConfig(t *testing.T, gitDir, url string) {
	t.Helper()
	config := "[core]\n\tbare = false\n[remote \"upstream\"]\n\turl = git@github.com:someone/else.git\n"
	if url != "" {
		config += "[remote \"origin\"]\n\turl = " + url + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	}
	require.NoError(t, os.MkdirAll(gitDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "config"), []byte(config), 0o600))
}

func TestGitRemote(t *testing.T) {
	t.Run("reads origin, not another remote", func(t *testing.T) {
		repo := t.TempDir()
		writeConfig(t, filepath.Join(repo, ".git"), "git@github.com:owenrumney/ade.git")
		nested := filepath.Join(repo, "internal")
		require.NoError(t, os.MkdirAll(nested, 0o755))
		assert.Equal(t, "owenrumney/ade", GitRemote(nested))
	})

	t.Run("a worktree reads the main repository's config", func(t *testing.T) {
		root := t.TempDir()
		common := filepath.Join(root, "ade", ".git")
		writeConfig(t, common, "https://github.com/owenrumney/ade")
		admin := filepath.Join(common, "worktrees", "more-stats")
		writeHead(t, admin, "ref: refs/heads/more-stats\n")
		require.NoError(t, os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o600))

		tree := filepath.Join(root, "ade-more-stats")
		require.NoError(t, os.MkdirAll(tree, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+admin+"\n"), 0o600))

		assert.Equal(t, "owenrumney/ade", GitRemote(tree))
		assert.Equal(t, "owenrumney/ade", RepoKey(tree))
	})

	t.Run("with no origin the repository's directory names it", func(t *testing.T) {
		root := t.TempDir()
		common := filepath.Join(root, "scratch", ".git")
		writeConfig(t, common, "")
		admin := filepath.Join(common, "worktrees", "wt")
		require.NoError(t, os.MkdirAll(admin, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(admin, "commondir"), []byte("../..\n"), 0o600))
		tree := filepath.Join(root, "scratch-wt")
		require.NoError(t, os.MkdirAll(tree, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+admin+"\n"), 0o600))

		assert.Empty(t, GitRemote(tree))
		assert.Equal(t, "scratch", RepoKey(tree), "the worktree belongs to scratch")
		assert.Equal(t, "scratch", RepoKey(filepath.Join(root, "scratch")))
	})

	t.Run("outside a repo there is nothing", func(t *testing.T) {
		assert.Empty(t, GitRemote(t.TempDir()))
		assert.Empty(t, RepoKey(""))
	})
}

func TestOwnerName(t *testing.T) {
	tests := []struct{ url, want string }{
		{"git@github.com:owenrumney/ade.git", "owenrumney/ade"},
		{"https://github.com/owenrumney/ade", "owenrumney/ade"},
		{"https://github.com/owenrumney/ade.git/", "owenrumney/ade"},
		{"ssh://git@host:2222/group/sub/name.git", "sub/name"},
		{"name", "name"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			assert.Equal(t, tt.want, ownerName(tt.url))
		})
	}
}

package archive

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The code under test commits, and a machine may have no git identity.
func TestMain(m *testing.M) {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if err := os.Setenv(k, "t"); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

// newRemote is a wiki remote with one page on it, as the store expects to find.
func newRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "wiki.git")
	git(t, "", "init", "--bare", "--initial-branch", "main", bare)
	work := t.TempDir()
	git(t, work, "init", "--initial-branch", "main")
	require.NoError(t, os.MkdirAll(filepath.Join(work, "general"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(work, "general", "first.md"), []byte("first\n"), 0o600))
	git(t, work, "add", ".")
	git(t, work, "commit", "-m", "first")
	git(t, work, "remote", "add", "origin", bare)
	git(t, work, "push", "origin", "main")
	return bare
}

func remotePage(t *testing.T, remote, name string) string {
	t.Helper()
	work := t.TempDir()
	git(t, "", "clone", remote, work)
	body, err := os.ReadFile(filepath.Join(work, filepath.FromSlash(name)))
	require.NoError(t, err)
	return string(body)
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, out)
}

package provision

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchNameIsTheDocumentsSlug(t *testing.T) {
	for doc, want := range map[string]string{
		"fix-1234-cancel-running-process.md": "fix-1234-cancel-running-process",
		"FIX-1234 Cancel.md":                 "fix-1234-cancel",
		"notes/worker ssh user.md":           "worker-ssh-user",
		"--force.md":                         "force",
		"   ":                                "",
		"":                                   "",
	} {
		t.Run(doc, func(t *testing.T) {
			assert.Equal(t, want, BranchName(doc))
		})
	}
}

func TestBranchSwitchesTheCloneToANewBranch(t *testing.T) {
	var name string
	var args []string
	run := func(_ context.Context, n string, a []string, _ io.Reader) error {
		name, args = n, a
		return nil
	}
	require.NoError(t, Branch(context.Background(), "/srv/ade/abc/ade", "fix-1234", run))
	assert.Equal(t, "git", name)
	assert.Equal(t, []string{"-C", "/srv/ade/abc/ade", "switch", "-c", "fix-1234"}, args)
}

func TestBranchRefusesANameItWouldNotHaveMade(t *testing.T) {
	ran := false
	run := func(context.Context, string, []string, io.Reader) error {
		ran = true
		return nil
	}
	for _, name := range []string{"", "-c", "Fix 1234", "a/b"} {
		assert.Errorf(t, Branch(context.Background(), "/srv/ade/abc/ade", name, run), "took %q", name)
	}
	assert.False(t, ran, "git was run on a name that was refused")
}

func TestBranchReportsWhatGitRefused(t *testing.T) {
	run := func(context.Context, string, []string, io.Reader) error {
		return errors.New("exit status 128")
	}
	err := Branch(context.Background(), "/srv/ade/abc/ade", "fix-1234", run)
	assert.ErrorContains(t, err, "branch fix-1234")
}

func TestOwnedDirIsOnlyASessionDirectoryUnderTheRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ADE_SCAN_DIR", root)
	session := filepath.Join(root, "0123456789abcdef")

	owned, ok := OwnedDir(filepath.Join(session, "ade"))
	require.True(t, ok)
	assert.Equal(t, session, owned)

	for _, path := range []string{
		filepath.Join(root, "ade"),
		filepath.Join(root, "owner", "ade"),
		filepath.Join(t.TempDir(), "0123456789abcdef", "ade"),
		session,
	} {
		_, ok := OwnedDir(path)
		assert.Falsef(t, ok, "%s is not a clone's checkout", path)
	}
}

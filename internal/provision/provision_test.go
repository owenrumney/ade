package provision

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneUsesASessionDirectoryAndStdinForTheToken(t *testing.T) {
	var gotName string
	var gotArgs []string
	var gotInput string
	run := func(_ context.Context, name string, args []string, input io.Reader) error {
		gotName, gotArgs = name, append([]string(nil), args...)
		data, err := io.ReadAll(input)
		require.NoError(t, err)
		gotInput = string(data)
		return nil
	}

	dir, err := Clone(context.Background(), "infracost/ade", "github-token", run)
	require.NoError(t, err)
	assert.Equal(t, "docker", gotName)
	assert.Contains(t, gotArgs, "https://github.com/infracost/ade.git")
	assert.Contains(t, gotArgs, dir)
	assert.NotContains(t, strings.Join(gotArgs, " "), "github-token")
	assert.Equal(t, "github-token\n", gotInput)
	assert.Equal(t, "ade", filepathBase(dir))
}

func TestAnotherHostKeepsItsURL(t *testing.T) {
	remote, name, err := repository("https://gitlab.com/infracost/cli.git")
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/infracost/cli.git", remote)
	assert.Equal(t, "cli", name)
}

// A provisioner published an hour ago is not one this machine has, and the
// clone it makes is the one that was fixed.
func TestCloneRefreshesTheProvisionerImageFirst(t *testing.T) {
	var calls [][]string
	run := func(_ context.Context, _ string, args []string, _ io.Reader) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	_, err := Clone(context.Background(), "infracost/ade", "token", run)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"pull", "-q", Image}, calls[0])
	assert.Equal(t, "run", calls[1][0])
}

// The worker and the provisioner are published separately and a machine can
// hold one of each age, so what passes between them has to suit both.
func TestTheRemoteSuitsAProvisionerOfEitherAge(t *testing.T) {
	remote, _, err := repository("git@github.com:infracost/dashboard.git")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(remote, "https://"), "git clone and gh repo clone both take a URL")
}

func TestCloneRejectsSomethingThatIsNotARepository(t *testing.T) {
	_, err := Clone(context.Background(), "../../etc", "token", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org/repo")
}

func TestCloneNeverEscapesTheSessionDirectory(t *testing.T) {
	for _, ref := range []string{"../..", "https://github.com/..", "git@github.com:.."} {
		t.Run(ref, func(t *testing.T) {
			_, err := Clone(context.Background(), ref, "token", func(context.Context, string, []string, io.Reader) error { return nil })
			require.Error(t, err)
		})
	}
}

// The laptop's origins are ssh, and the provisioner has a token and no key, so
// an ssh remote is cloned over https or not at all.
func TestRepositoryClonesAnSSHOriginOverHTTPS(t *testing.T) {
	for _, ref := range []string{
		"git@github.com:infracost/infracost.git",
		"git@github.com:infracost/infracost",
		"ssh://git@github.com/infracost/infracost.git",
	} {
		remote, name, err := repository(ref)
		require.NoError(t, err, ref)
		assert.Equal(t, "https://github.com/infracost/infracost.git", remote)
		assert.Equal(t, "infracost", name)
	}
}

func filepathBase(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// The provisioner runs as root by default, so its checkouts are root-owned and
// the session's own user cannot purge them.
func TestCloneRunsAsWhoeverOwnsTheWorkRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ADE_SCAN_DIR", root)

	var gotArgs []string
	run := func(_ context.Context, _ string, args []string, _ io.Reader) error {
		gotArgs = args
		return nil
	}
	_, err := Clone(context.Background(), "infracost/ade", "token", run)
	require.NoError(t, err)

	joined := strings.Join(gotArgs, " ")
	assert.Contains(t, joined, "--user", "the clone is made as the root's owner")
	assert.Contains(t, joined, rootOwner(root))
	// gh has nowhere to write its config as a uid with no passwd entry.
	assert.Contains(t, joined, "HOME=/tmp")
}

// A failed clone leaves a partial checkout under a random session id, and the
// id is never reused, so nothing else would ever remove it.
func TestAFailedCloneLeavesNothingBehind(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ADE_SCAN_DIR", root)

	run := func(_ context.Context, _ string, args []string, _ io.Reader) error {
		dir := args[len(args)-1]
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
		return errors.New("exit status 128")
	}
	_, err := Clone(context.Background(), "infracost/ade", "token", run)
	require.Error(t, err)

	left, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, left, "a partial checkout no session owns is disk nothing can purge")
}

func TestCloneRunsAsTheDefaultWhenTheRootCannotBeRead(t *testing.T) {
	t.Setenv("ADE_SCAN_DIR", "/nowhere/that/exists")

	var gotArgs []string
	run := func(_ context.Context, _ string, args []string, _ io.Reader) error {
		gotArgs = args
		return nil
	}
	_, err := Clone(context.Background(), "infracost/ade", "token", run)
	require.NoError(t, err)
	assert.NotContains(t, strings.Join(gotArgs, " "), "--user",
		"no owner to copy is no worse than before")
}

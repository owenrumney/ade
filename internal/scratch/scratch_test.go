package scratch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func TestANewScratchIsACheckoutAdeCanServe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := New(context.Background(), "Rate limit the webhook handler")
	require.NoError(t, err)

	// Under ade's own state, named after the work, and a repository.
	root, err := settings.ScratchDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "rate-limit-the-webhook-handler"), dir)
	assert.DirExists(t, filepath.Join(dir, ".git"))
	assert.True(t, settings.IsScratch(dir))

	// A branch above an empty main: the diff has a base, and the work comes
	// out again as a range of commits.
	assert.Equal(t, "rate-limit-the-webhook-handler", gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD"))
	assert.Equal(t, "1", gitOut(t, dir, "rev-list", "--count", "main"))
	assert.Empty(t, gitOut(t, dir, "diff", "--name-only", "main"))
}

func TestTwoScratchesOfTheSameNameGetTheirOwnDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	first, err := New(context.Background(), "same thing")
	require.NoError(t, err)
	second, err := New(context.Background(), "same thing")
	require.NoError(t, err)

	assert.Equal(t, "same-thing", filepath.Base(first))
	assert.Equal(t, "same-thing-2", filepath.Base(second))
	assert.Equal(t, "same-thing-2", gitOut(t, second, "rev-parse", "--abbrev-ref", "HEAD"))
}

func TestAScratchWithNothingSaidAboutItIsDated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := New(context.Background(), "   ")
	require.NoError(t, err)
	assert.Regexp(t, `^scratch-\d{8}-\d{6}$`, filepath.Base(dir))
}

func TestTheCommitterIsAdeWhenTheMachineHasNoIdentity(t *testing.T) {
	// Nothing to fall back on: the empty base commit still has to be made, or
	// a scratch cannot start at all.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(home, "no-such-gitconfig"))

	dir, err := New(context.Background(), "no identity here")
	require.NoError(t, err)
	assert.Equal(t, "ade", gitOut(t, dir, "log", "-1", "--format=%an", "main"))
}

func TestSlugKeepsWhatCanBeABranchName(t *testing.T) {
	cases := map[string]string{
		"Add rate limiting to the webhook handler now": "add-rate-limiting-to-the",
		"  spaced  out  ":      "spaced-out",
		"Fix #123: the thing!": "fix-123-the-thing",
		"----":                 "",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := Slug(in)
			if want == "" {
				assert.Regexp(t, `^scratch-\d{8}-\d{6}$`, got)
				return
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestAScratchThatCouldNotBeMadeLeavesNoDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// git cannot run under a cancelled context, so the claimed name has to go
	// back: leaving it would burn the name and look like a made scratch.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(ctx, "never got going")
	require.Error(t, err)

	root, err := settings.ScratchDir()
	require.NoError(t, err)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

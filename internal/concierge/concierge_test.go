package concierge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/settings"
)

func TestTheDeskIsACheckoutAdeCanServe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := Ensure(context.Background())

	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(dir, ".git"))
	assert.FileExists(t, filepath.Join(dir, "CLAUDE.md"))
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	require.NoError(t, err)
	assert.Equal(t, "1", strings.TrimSpace(string(out)), "a base commit, so a diff has an end")
}

func TestTheDeskSitsOutsideEveryScanRoot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := Ensure(context.Background())
	require.NoError(t, err)

	// Nothing hides the desk from the session list: it is simply not anywhere
	// the scan looks. A flag would rot the day somebody adds a root.
	config, err := settings.Load()
	require.NoError(t, err)
	for _, root := range config.Roots() {
		assert.NotEqual(t, root, dir)
		assert.False(t, strings.HasPrefix(dir, root+string(filepath.Separator)),
			"the desk is under %s, which is scanned", root)
	}
	assert.False(t, settings.IsScratch(dir))
}

func TestMakingTheDeskTwiceKeepsTheConversationsCheckout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first, err := Ensure(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(first, "kept.txt"), []byte("still here"), 0o600))

	again, err := Ensure(context.Background())

	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.FileExists(t, filepath.Join(again, "kept.txt"))
}

func TestTheRoleIsRewrittenSoAnOldOneCannotLinger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := Ensure(context.Background())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("be a coding agent"), 0o600))

	_, err = Ensure(context.Background())

	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, Role(), string(body))
}

func TestTheRoleForbidsTheThingsTheDeskMustNotDo(t *testing.T) {
	role := Role()

	assert.Contains(t, role, "never on your own initiative")
	assert.Contains(t, role, "the press is theirs")
}

func TestOnlyTheDesksOwnCheckoutIsTheDesk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := Ensure(context.Background())
	require.NoError(t, err)

	assert.True(t, Is(dir))
	assert.False(t, Is(t.TempDir()))
	assert.False(t, Is(""))
}

func TestTheDeskIsNeverBoxed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := Ensure(context.Background())
	require.NoError(t, err)

	// A container gets no MCP config at all, so a boxed desk is a desk with
	// no tools: it could not file a todo or say what is running.
	on, known := runlog.Open(runlog.Path(filepath.Join(dir, ".git"))).Sandbox()
	require.True(t, known, "the choice never reached the memory")
	assert.False(t, on)
}

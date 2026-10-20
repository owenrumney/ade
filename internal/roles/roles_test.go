package roles

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestRolesAreSavedSelectedAndDeleted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, Create("solo", "keep the change small"))
	require.NoError(t, Create("paired", "use explicit profiles"))
	require.NoError(t, SetDefault("paired"))

	view, err := ViewOf()
	require.NoError(t, err)
	assert.Equal(t, "paired", view.Default)
	assert.Equal(t, []Role{{Name: "paired", Prompt: "use explicit profiles"}, {Name: "solo", Prompt: "keep the change small"}}, view.Roles)
	assert.Equal(t, "use explicit profiles", Current())

	require.NoError(t, Delete("paired"))
	assert.Empty(t, Current())
	view, err = ViewOf()
	require.NoError(t, err)
	assert.Empty(t, view.Default)
	assert.Equal(t, []Role{{Name: "solo", Prompt: "keep the change small"}}, view.Roles)
}

func TestRolesRejectAnUnsafeNameAndOversizedPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Error(t, Save("../outside", "no"))
	assert.Error(t, Save("empty", ""))
	assert.Error(t, Save("large", string(make([]byte, MaxPrompt+1))))
	assert.Error(t, SetDefault("missing"))
}

func TestInstallKeepsLocalEditsUnlessForced(t *testing.T) {
	stands(t, shippedRoles())
	home := t.TempDir()
	defaults, err := Defaults()
	require.NoError(t, err)
	done, err := Install(home, false)
	require.NoError(t, err)
	require.Len(t, done, len(defaults))
	for _, item := range done {
		assert.Equal(t, "written", item.State)
	}

	path := filepath.Join(home, ".ade", "roles", "solo.md")
	require.NoError(t, os.WriteFile(path, []byte("mine\n"), 0o600))
	done, err = Install(home, false)
	require.NoError(t, err)
	assert.Equal(t, "kept", stateFor(done, "solo"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data))

	done, err = Install(home, true)
	require.NoError(t, err)
	assert.Equal(t, "replaced", stateFor(done, "solo"))
}

func TestCurrentIgnoresAStaleDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{DefaultRole: "gone"}))
	assert.Empty(t, Current())
}

func TestLoadSkipsAHandWrittenOversizedRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir, err := Dir()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "large.md"), make([]byte, MaxPrompt+1), 0o600))
	list, err := Load()
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestASessionsOwnRoleIsReadByNameNotBySelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, Create("solo", "keep the change small"))
	require.NoError(t, Create("paired", "use explicit profiles"))
	require.NoError(t, SetDefault("paired"))

	assert.Equal(t, "keep the change small", Prompt("solo"), "the session's own role, not the selected one")
	assert.Empty(t, Prompt("gone"), "a deleted role is no role, not a failure to start")
	assert.Empty(t, Prompt(""))
}

func stateFor(done []Installed, name string) string {
	for _, item := range done {
		if item.Name == name {
			return item.State
		}
	}
	return ""
}

package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWorkspaceIsCheckedBeforeItIsKept(t *testing.T) {
	home := t.TempDir()
	other := filepath.Join(home, "code", "other")
	require.NoError(t, os.MkdirAll(other, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	file := WorkspacePath(filepath.Join(home, "code", "x", ".git"))

	empty, err := LoadWorkspace(file)
	require.NoError(t, err)
	assert.Empty(t, empty.Paths, "no file is an empty workspace")
	assert.NotNil(t, empty.Env)

	tests := []struct {
		name string
		ws   Workspace
		want string
	}{
		{"relative", Workspace{Paths: []Path{{Path: "code/other"}}}, "absolute"},
		{"missing", Workspace{Paths: []Path{{Path: filepath.Join(home, "nope")}}}, "no such file"},
		{"claude", Workspace{Paths: []Path{{Path: filepath.Join(home, ".claude", "history.jsonl")}}}, "~/.claude"},
		{"bad key", Workspace{Env: map[string]string{"1x": "y"}}, "not a variable name"},
		{"home", Workspace{Env: map[string]string{"HOME": "/elsewhere"}}, "HOME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SaveWorkspace(file, home, tt.ws)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NoFileExists(t, file, "nothing is written when a line is wrong")
		})
	}

	saved, err := SaveWorkspace(file, home, Workspace{
		Paths: []Path{{Path: "~/code/other", RW: true}, {Path: " " + other + " "}, {Path: ""}},
		Env:   map[string]string{"GOFLAGS": "-mod=mod", " ": "x"},
	})
	require.NoError(t, err)
	assert.Equal(t, []Path{{Path: other, RW: true}}, saved.Paths, "~ expanded, trimmed, deduplicated on the first")
	assert.Equal(t, map[string]string{"GOFLAGS": "-mod=mod"}, saved.Env)

	again, err := LoadWorkspace(file)
	require.NoError(t, err)
	assert.Equal(t, saved, again)
	assert.Equal(t, []Mount{{Host: other, Guest: other, RW: true}}, again.Mounts())

	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the variables may be secrets")
}

func TestARepositorySandboxIsLaidOverTheGlobalOne(t *testing.T) {
	global := Workspace{
		Paths: []Path{{Path: "/opt/shared"}, {Path: "/opt/both"}},
		Env:   map[string]string{"CI": "1", "GOFLAGS": "-mod=mod"},
	}
	repo := Workspace{
		Paths: []Path{{Path: "/opt/both", RW: true}, {Path: "/opt/mine"}},
		Env:   map[string]string{"GOFLAGS": "-mod=readonly"},
	}

	got := repo.Under(global)

	assert.Equal(t, []Path{{Path: "/opt/both", RW: true}, {Path: "/opt/mine"}, {Path: "/opt/shared"}}, got.Paths,
		"both lists, and the repository's word on a path it also names")
	assert.Equal(t, map[string]string{"CI": "1", "GOFLAGS": "-mod=readonly"}, got.Env,
		"the global variable applies unless the repository sets its own")
}

func TestAnEmptyGlobalSandboxLeavesTheRepositoryAlone(t *testing.T) {
	repo := Workspace{Paths: []Path{{Path: "/opt/mine", RW: true}}, Env: map[string]string{"CI": "1"}}

	got := repo.Under(Workspace{})

	assert.Equal(t, repo.Paths, got.Paths)
	assert.Equal(t, repo.Env, got.Env)
}

func TestASecretVariableNeverLeavesTheMachine(t *testing.T) {
	t.Parallel()
	env := map[string]string{
		"GITHUB_TOKEN": "ghp_EXAMPLE_NOT_A_REAL_TOKEN",
		"db_password":  "hunter2",
		"GOFLAGS":      "-mod=mod",
		"TOKEN_PATH":   "",
	}
	view := RedactEnv(env)
	assert.Equal(t, Redacted, view["GITHUB_TOKEN"])
	assert.Equal(t, Redacted, view["db_password"], "the name is matched however it is cased")
	assert.Equal(t, "-mod=mod", view["GOFLAGS"])
	assert.Empty(t, view["TOKEN_PATH"], "an empty value has nothing to hide")
	assert.Equal(t, "ghp_EXAMPLE_NOT_A_REAL_TOKEN", env["GITHUB_TOKEN"], "the stored map is not touched")
}

func TestASecretIsNamedMoreWaysThanPasswordAndToken(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "AWS_SECRET_ACCESS_KEY", "SSH_PRIVATE_KEY",
		"CLIENT_SECRET", "PASSWD", "GH_PAT", "NPM_CONFIG__AUTH", "KEY", "GCP_CREDENTIALS",
		"SSH_PASSPHRASE", "SENTRY_DSN", "SLACK_WEBHOOK",
	} {
		assert.True(t, Secret(key), key)
	}
	for _, key := range []string{"GOFLAGS", "PATH", "HOME", "KEYBOARD_LAYOUT", "MONKEY"} {
		assert.False(t, Secret(key), key)
	}
}

func TestTheMarkerComingBackMeansKeepWhatIsStored(t *testing.T) {
	t.Parallel()
	was := map[string]string{"GITHUB_TOKEN": "ghp_EXAMPLE_NOT_A_REAL_TOKEN", "API_PASSWORD": "old", "GOFLAGS": "-mod=mod"}
	sent := map[string]string{
		"GITHUB_TOKEN": Redacted,
		"API_PASSWORD": "typed over",
		"GOFLAGS":      "-mod=vendor",
	}
	got, err := RestoreEnv(sent, was)
	require.NoError(t, err)
	assert.Equal(t, "ghp_EXAMPLE_NOT_A_REAL_TOKEN", got["GITHUB_TOKEN"])
	assert.Equal(t, "typed over", got["API_PASSWORD"])
	assert.Equal(t, "-mod=vendor", got["GOFLAGS"])
	_, dropped := got["DROPPED"]
	assert.False(t, dropped, "a line deleted from the page is gone")
}

func TestRenamingAHiddenValueIsRefusedRatherThanErasingIt(t *testing.T) {
	t.Parallel()
	was := map[string]string{"GITHUB_TOKEN": "ghp_EXAMPLE_NOT_A_REAL_TOKEN"}
	sent := map[string]string{"GH_TOKEN": Redacted}
	_, err := RestoreEnv(sent, was)
	require.Error(t, err, "the marker names no stored value, so the save loses the secret")
	assert.Contains(t, err.Error(), "GH_TOKEN")
}

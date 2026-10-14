package sandbox

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheCodexSpecCopiesTheConfigAndMountsTheSessions(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "x")
	codexDir := filepath.Join(home, ".codex")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.MkdirAll(codexDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "config.toml"), []byte("[otel]\nexporter = \"otlp-grpc\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(`{"tokens":{}}`), 0o600))
	skillsDir := filepath.Join(home, ".claude", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "triage"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "triage", "SKILL.md"), []byte("---\nname: triage\n---\n\nbody\n"), 0o600))

	spec, err := Codex{Home: home, Root: root}.Spec()
	require.NoError(t, err)

	guests := map[string]Mount{}
	for _, m := range spec.Mounts {
		guests[m.Guest] = m
	}
	assert.True(t, guests[root].RW, "the checkout is written")
	assert.Equal(t, CodexBox(home), guests[codexDir].Host, "the guest's ~/.codex is ade's box")
	assert.True(t, guests[codexDir].RW)
	sessions := filepath.Join(codexDir, "sessions")
	assert.Equal(t, sessions, guests[sessions].Host, "threads land on the host so the launcher can list them")
	assert.True(t, guests[sessions].RW)
	assert.Equal(t, skillsDir, guests[skillsDir].Host, "claude's skills are mounted so codex's links resolve")
	assert.False(t, guests[skillsDir].RW)
	link, err := os.Readlink(filepath.Join(CodexBox(home), "skills", "triage"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(skillsDir, "triage"), link, "codex reads $CODEX_HOME/skills")

	// The [otel] section is why the driver runs codex rather than talking to
	// the backend itself, so it has to reach the process.
	config, err := os.ReadFile(CodexConfig(home))
	require.NoError(t, err)
	assert.Contains(t, string(config), "[otel]")
	assert.FileExists(t, filepath.Join(CodexBox(home), "auth.json"))
}

// codexLogin writes a login stamped at the given time.
func codexLogin(t *testing.T, path, access string, at time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	body := `{"tokens":{"access_token":"` + access + `","refresh_token":"r-` + access +
		`"},"last_refresh":"` + at.UTC().Format(time.RFC3339) + `"}`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func TestTheContainersCodexRefreshBecomesTheHosts(t *testing.T) {
	// The CLI refreshes inside the box and rotates the host's pair away. Left
	// one-way, the host's own `codex` is logged out by its next call.
	home := t.TempDir()
	now := time.Now()
	codexLogin(t, CodexHostAuth(home), "old", now.Add(-time.Hour))
	codexLogin(t, CodexAuth(home), "refreshed", now)

	require.NoError(t, CodexCredentials(home))

	host, err := os.ReadFile(CodexHostAuth(home))
	require.NoError(t, err)
	assert.Contains(t, string(host), "refreshed")
}

func TestTheHostsCodexLoginWinsWhenItIsTheLaterOne(t *testing.T) {
	home := t.TempDir()
	now := time.Now()
	codexLogin(t, CodexHostAuth(home), "fresh", now)
	codexLogin(t, CodexAuth(home), "stale", now.Add(-time.Hour))

	require.NoError(t, CodexCredentials(home))

	box, err := os.ReadFile(CodexAuth(home))
	require.NoError(t, err)
	assert.Contains(t, string(box), "fresh")
}

func TestAHalfWrittenCodexLoginIsNotWrittenBack(t *testing.T) {
	// A failed refresh in the container must not log the host out.
	home := t.TempDir()
	now := time.Now()
	codexLogin(t, CodexHostAuth(home), "good", now.Add(-time.Hour))
	require.NoError(t, os.MkdirAll(CodexBox(home), 0o700))
	require.NoError(t, os.WriteFile(CodexAuth(home),
		[]byte(`{"tokens":{"access_token":"half"},"last_refresh":"`+
			now.UTC().Format(time.RFC3339)+`"}`), 0o600))

	require.NoError(t, CodexCredentials(home))

	host, err := os.ReadFile(CodexHostAuth(home))
	require.NoError(t, err)
	assert.Contains(t, string(host), "good")
}

func TestACodexLoginWithNoStampLeavesTheHostAlone(t *testing.T) {
	// An unstamped file is not evidence of a later refresh; mtime used to be
	// read as one, and a copy gets a new mtime for free.
	home := t.TempDir()
	codexLogin(t, CodexHostAuth(home), "host", time.Now().Add(-time.Hour))
	require.NoError(t, os.MkdirAll(CodexBox(home), 0o700))
	require.NoError(t, os.WriteFile(CodexAuth(home),
		[]byte(`{"tokens":{"access_token":"box","refresh_token":"r"}}`), 0o600))

	require.NoError(t, CodexCredentials(home))

	host, err := os.ReadFile(CodexHostAuth(home))
	require.NoError(t, err)
	assert.Contains(t, string(host), "host")
	box, err := os.ReadFile(CodexAuth(home))
	require.NoError(t, err)
	assert.Contains(t, string(box), "host", "brought up to the host's")
}

func TestSyncingCodexWithNoHostLoginIsNotAnError(t *testing.T) {
	home := t.TempDir()
	codexLogin(t, CodexAuth(home), "boxonly", time.Now())

	require.NoError(t, CodexCredentials(home))
	SyncCodexCredentials(home)

	assert.NoFileExists(t, CodexHostAuth(home), "ade does not invent a login the user never made")
	box, err := os.ReadFile(CodexAuth(home))
	require.NoError(t, err)
	assert.Contains(t, string(box), "boxonly", "the copy is all there is")
}

func TestTheCodexSpecNeedsAHomeAndARoot(t *testing.T) {
	_, err := Codex{Root: t.TempDir()}.Spec()
	require.Error(t, err)
	_, err = Codex{Home: t.TempDir()}.Spec()
	require.Error(t, err)
}

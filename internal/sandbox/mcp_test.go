package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpecAddsGitHubPackagesAuthForAToken(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))

	spec, err := Claude{Home: home, Root: root, Env: map[string]string{"NODE_AUTH_TOKEN": "secret"}}.Spec(Box(home))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".claude", npmrcFile), spec.Env["NPM_CONFIG_USERCONFIG"])
	written, err := os.ReadFile(filepath.Join(Box(home), npmrcFile))
	require.NoError(t, err)
	assert.Equal(t, "//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}\n", string(written))
	assert.NotContains(t, string(written), "secret")
}

func TestSpecRewritesLoopbackMCPURLsForDocker(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"hub":{"type":"http","url":"http://127.0.0.1:4867/mcp"},"remote":{"type":"http","url":"https://example.com/mcp"}}}`), 0o600))

	_, err := Claude{Home: home, Root: root, MCPHost: "host.docker.internal"}.Spec(Box(home))
	require.NoError(t, err)
	written, err := os.ReadFile(filepath.Join(Box(home), mcpFile))
	require.NoError(t, err)
	assert.JSONEq(t, `{"mcpServers":{"hub":{"type":"http","url":"http://host.docker.internal:4867/mcp"},"remote":{"type":"http","url":"https://example.com/mcp"}}}`, string(written))
}

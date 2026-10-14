package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unpacked is the tar's entries by name, so a test says what was sent rather
// than how many bytes it took.
func unpacked(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	out := map[string]string{}
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return out
		}
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		out[header.Name] = string(body)
	}
}

func nvimHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "nvim", "lua", "plugins")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "nvim", "init.lua"), []byte("vim.o.number = true\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lsp.lua"), []byte("return {}\n"), 0o644))
	return home
}

func TestEditorConfigCarriesTheWholeTree(t *testing.T) {
	data, err := EditorConfig(nvimHome(t))
	require.NoError(t, err)
	require.NotEmpty(t, data)

	files := unpacked(t, data)
	assert.Equal(t, "vim.o.number = true\n", files["init.lua"])
	assert.Equal(t, "return {}\n", files["lua/plugins/lsp.lua"])
	// Paths are relative to the config root, so the far side untars into
	// $HOME/.config/nvim rather than recreating the laptop's home.
	for name := range files {
		assert.False(t, strings.HasPrefix(name, "/"), name)
		assert.False(t, strings.Contains(name, ".."), name)
	}
}

func TestNoEditorConfigIsNotAnError(t *testing.T) {
	data, err := EditorConfig(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestASymlinkOutOfTheTreeIsNotSent(t *testing.T) {
	home := nvimHome(t)
	secret := filepath.Join(home, "secret")
	require.NoError(t, os.WriteFile(secret, []byte("token"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(home, ".config", "nvim", "link.lua")))

	data, err := EditorConfig(home)
	require.NoError(t, err)
	files := unpacked(t, data)
	assert.NotContains(t, files, "link.lua")
	for _, body := range files {
		assert.NotContains(t, body, "token")
	}
}

func TestAConfigOverTheCapIsRefusedRatherThanSent(t *testing.T) {
	home := nvimHome(t)
	big := filepath.Join(home, ".config", "nvim", "big.bin")
	require.NoError(t, os.WriteFile(big, make([]byte, EditorLimit+1), 0o644))

	_, err := EditorConfig(home)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cap")
}

func TestPrepareEditorConfigUnpacksItAsTheWorker(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) { return []byte(sentinel), nil, nil }}
	host := testHost(t, rec)

	require.NoError(t, host.PrepareEditorConfig(context.Background(), []byte("tarball")))
	require.Len(t, rec.calls, 1)
	command := rec.calls[0][len(rec.calls[0])-1]
	assert.True(t, hasArgs(command, "docker", "exec", "-i", workerName))
	assert.Contains(t, command, "$HOME/.config/nvim")
	assert.Equal(t, [][]byte{[]byte("tarball")}, rec.input)
}

func TestNothingToSendRunsNothing(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) { return []byte(sentinel), nil, nil }}
	host := testHost(t, rec)

	require.NoError(t, host.PrepareEditorConfig(context.Background(), nil))
	assert.Empty(t, rec.calls)
}

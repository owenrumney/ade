package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHub is an mcp-hub that writes its record when told to serve, pointing at
// a stand-in that answers as this process.
func fakeHub(t *testing.T) string {
	t.Helper()
	srv := hubAt(t, os.Getpid(), true)
	home := t.TempDir()
	t.Setenv(envHome, home)
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-hub")
	record := fmt.Sprintf(`{"pid":%d,"url":%q}`, os.Getpid(), srv.URL)
	script := "#!/bin/sh\n[ \"$1\" = serve ] || exit 2\nprintf '%s' '" + record + "' > \"$MCP_HUB_HOME/server.json\"\n"
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700))
	was := lookPath
	lookPath = func(string) (string, error) { return bin, nil }
	t.Cleanup(func() { lookPath = was })
	return bin
}

func TestEnsureStartsTheHubWhenNoneIsRunning(t *testing.T) {
	fakeHub(t)
	got, err := Ensure(context.Background())
	require.NoError(t, err)
	assert.True(t, got.Running)
	assert.NotEmpty(t, got.URL)
}

func TestEnsureLeavesARunningHubAlone(t *testing.T) {
	srv := hubAt(t, os.Getpid(), true)
	write(t, fmt.Sprintf(`{"pid":%d,"url":%q}`, os.Getpid(), srv.URL))
	was := lookPath
	lookPath = func(string) (string, error) { t.Fatal("nothing should be started"); return "", nil }
	t.Cleanup(func() { lookPath = was })
	got, err := Ensure(context.Background())
	require.NoError(t, err)
	assert.Equal(t, srv.URL, got.URL)
}

func TestEnsureSaysWhenThereIsNoHubToStart(t *testing.T) {
	t.Setenv(envHome, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	was := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = was })
	_, err := Ensure(context.Background())
	assert.ErrorIs(t, err, ErrNotInstalled)
}

func TestEnsureGivesUpOnAHubThatNeverAnswers(t *testing.T) {
	was := startWait
	startWait = 150 * time.Millisecond
	t.Cleanup(func() { startWait = was })
	t.Setenv(envHome, t.TempDir())
	dir := t.TempDir()
	bin := filepath.Join(dir, "mcp-hub")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	wasLook := lookPath
	lookPath = func(string) (string, error) { return bin, nil }
	t.Cleanup(func() { lookPath = wasLook })
	_, err := Ensure(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not answer")
}

package session

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func asRootOnLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("ownership is mapped by the VM off Linux")
	}
	if os.Getuid() != 0 {
		t.Skip("chowning to another user needs root")
	}
}

func owner(t *testing.T, path string) (int, int) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return int(stat.Uid), int(stat.Gid)
}

func TestTheStateDirectoryBecomesTheHostUsersOwn(t *testing.T) {
	// A Remote-SSH window on a worker reads these as the ssh user. Left to
	// root they are a Design pane with nothing in it and no log to read.
	asRootOnLinux(t)
	t.Setenv(HostUserEnv, "1000")
	t.Setenv(HostGroupEnv, "1001")
	dir := t.TempDir()
	docs := filepath.Join(dir, "docs", "enhancements")
	require.NoError(t, os.MkdirAll(docs, 0o700))
	deep := filepath.Join(docs, "08.md")
	for _, path := range []string{filepath.Join(dir, "chat.db"), filepath.Join(dir, "server.log"), deep} {
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	}

	HandToHost(dir)

	for _, path := range []string{dir, filepath.Join(dir, "chat.db"),
		filepath.Join(dir, "server.log"), filepath.Join(dir, "docs"), deep} {
		uid, gid := owner(t, path)
		assert.Equal(t, 1000, uid, path)
		assert.Equal(t, 1001, gid, path)
	}
}

func TestAHandoverRunsAgainForWhatTheTurnAdded(t *testing.T) {
	// Unlike the build cache, this directory keeps gaining root-owned files.
	// Skipping an already-handed-over directory would miss every one of them.
	asRootOnLinux(t)
	t.Setenv(HostUserEnv, "1000")
	t.Setenv(HostGroupEnv, "1001")
	dir := t.TempDir()
	HandToHost(dir)
	later := filepath.Join(dir, "outbox")
	require.NoError(t, os.MkdirAll(later, 0o700))

	HandToHost(dir)

	uid, _ := owner(t, later)
	assert.Equal(t, 1000, uid)
}

func TestNothingMovesWhenAdeIsTheHostUserItself(t *testing.T) {
	asRootOnLinux(t)
	t.Setenv(HostUserEnv, "1000")
	t.Setenv(HostGroupEnv, "")
	dir := t.TempDir()

	HandToHost(dir)

	uid, _ := owner(t, dir)
	assert.Equal(t, os.Getuid(), uid, "half an answer is no answer")
}

func TestARecordWriteHandsOverWhatItMade(t *testing.T) {
	asRootOnLinux(t)
	t.Setenv(HostUserEnv, "1000")
	t.Setenv(HostGroupEnv, "1001")
	path := filepath.Join(t.TempDir(), "ade", "server.json")

	require.NoError(t, Write(path, Record{PID: 1, URL: "http://x", Root: "/r"}))

	for _, p := range []string{filepath.Dir(path), path} {
		uid, gid := owner(t, p)
		assert.Equal(t, 1000, uid, p)
		assert.Equal(t, 1001, gid, p)
	}
}

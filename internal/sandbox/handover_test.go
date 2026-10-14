package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/owenrumney/ade/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func onLinuxAsRoot(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("ownership is mapped by the VM off Linux")
	}
	if os.Getuid() != 0 {
		t.Skip("chowning to another user needs root")
	}
}

func ownerOf(t *testing.T, path string) (int, int) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)
	return int(stat.Uid), int(stat.Gid)
}

func TestWhatAdeMadeForTheSandboxBecomesTheGuestsOwn(t *testing.T) {
	// `codex` reported this as "failed to initialize sqlite state runtime":
	// root made ~/.codex 0700 in the worker and the agent runs as the ssh user.
	onLinuxAsRoot(t)
	t.Setenv(session.HostUserEnv, "1000")
	t.Setenv(session.HostGroupEnv, "1001")
	box := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.MkdirAll(filepath.Join(box, "sessions"), 0o700))
	inside := filepath.Join(box, "auth.json")
	require.NoError(t, os.WriteFile(inside, []byte("{}"), 0o600))

	handToGuest(box)

	for _, path := range []string{box, filepath.Join(box, "sessions"), inside} {
		uid, gid := ownerOf(t, path)
		assert.Equal(t, 1000, uid, path)
		assert.Equal(t, 1001, gid, path)
	}
}

func TestNothingIsHandedOverWhenAdeIsTheUserItself(t *testing.T) {
	// Locally ade is you. Chowning to your own ids is work for nothing, and
	// half an answer from the environment is no answer.
	onLinuxAsRoot(t)
	t.Setenv(session.HostUserEnv, "1000")
	t.Setenv(session.HostGroupEnv, "")
	dir := t.TempDir()

	handToGuest(dir)

	uid, _ := ownerOf(t, dir)
	assert.Equal(t, os.Getuid(), uid)
}

func TestAnAlreadyHandedOverCacheIsNotWalkedAgain(t *testing.T) {
	// The build cache is gigabytes. Walking it on every run would be the
	// slowest thing a session does before it has answered anything.
	onLinuxAsRoot(t)
	t.Setenv(session.HostUserEnv, "1000")
	t.Setenv(session.HostGroupEnv, "1001")
	cache := t.TempDir()
	require.NoError(t, os.Chown(cache, 1000, 1001))
	deep := filepath.Join(cache, "go-build", "ab")
	require.NoError(t, os.MkdirAll(deep, 0o750))

	handToGuest(cache)

	uid, _ := ownerOf(t, deep)
	assert.Equal(t, 0, uid, "the top was already theirs, so the walk was skipped")
}

func TestAMissingPathIsNotAFailure(t *testing.T) {
	onLinuxAsRoot(t)
	t.Setenv(session.HostUserEnv, "1000")
	t.Setenv(session.HostGroupEnv, "1001")

	handToGuest(filepath.Join(t.TempDir(), "never-made"))
}

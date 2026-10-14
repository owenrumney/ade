//go:build unix

package agents

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPipeInTheAgentsDirectoryDoesNotHangTheView(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "a.md"), 0o600))

	done := make(chan UserView, 1)
	go func() {
		view, _ := User(home)
		done <- view
	}()
	select {
	case view := <-done:
		assert.Equal(t, SourceUser, sources(view)["a"])
	case <-time.After(3 * time.Second):
		t.Fatal("User blocked reading a pipe")
	}
}

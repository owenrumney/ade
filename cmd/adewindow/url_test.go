package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheWindowTakesALoginShellsPath is the Dock's thin PATH: without this the
// agent's PTY cannot exec claude, and the pane is a blinking cursor.
func TestTheWindowTakesALoginShellsPath(t *testing.T) {
	if _, err := os.Stat("/bin/zsh"); err != nil {
		t.Skip("no zsh")
	}
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("PATH", "/usr/bin:/bin")

	got := shellPath()
	require.NotEmpty(t, got, "a login shell always prints a PATH")
	assert.NotEqual(t, "/usr/bin:/bin", got)
	assert.NotContains(t, got, "__ADE_PATH__")
	assert.Greater(t, strings.Count(got, ":"), 1, got)
}

func TestAShellThatPrintsNothingLeavesThePathAlone(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/true")
	assert.Empty(t, shellPath())
}

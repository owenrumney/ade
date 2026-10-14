package web

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/runlog"
)

// restarted is a second runner over the same checkout, told what the flags say.
func restarted(t *testing.T, dir string, box sandboxConfig) *agentRunner {
	t.Helper()
	return newAgentRunner(dir, sessionNone, "", "", "", box, runlog.Open(runlog.Path(filepath.Join(dir, ".git"))))
}

func TestTheSandboxChoiceOutlivesTheServerThatMadeIt(t *testing.T) {
	for _, want := range []bool{false, true} {
		dir := t.TempDir()
		t.Setenv("HOME", t.TempDir())

		was := restarted(t, dir, sandboxConfig{On: !want})
		was.setSandbox(want)

		assert.Equal(t, want, restarted(t, dir, sandboxConfig{On: !want}).State().Sandbox,
			"the flag default put the agent back where the user took it from")
	}
}

func TestATypedSandboxFlagOutranksTheRememberedChoice(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	restarted(t, dir, sandboxConfig{On: true}).setSandbox(false)

	on := restarted(t, dir, sandboxConfig{On: true, Explicit: true}).State().Sandbox
	assert.True(t, on, "--sandbox asked for isolation and did not get it")
}

func TestSetSandboxWritesThroughToTheMemory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())

	restarted(t, dir, sandboxConfig{On: true}).setSandbox(false)

	on, known := runlog.Open(runlog.Path(filepath.Join(dir, ".git"))).Sandbox()
	require.True(t, known, "the choice never reached the memory")
	assert.False(t, on)
}

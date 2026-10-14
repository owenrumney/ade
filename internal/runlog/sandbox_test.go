package runlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The choice is kept off the checkout, so every test here needs a home of its own.
func ownHome(t *testing.T) { t.Helper(); t.Setenv("HOME", t.TempDir()) }

func TestACheckoutThatNeverChoseHasNoSandboxOpinion(t *testing.T) {
	ownHome(t)
	on, known := Open(filepath.Join(t.TempDir(), "agent.json")).Sandbox()
	assert.False(t, known, "an unset memory must not outvote the flag default")
	assert.False(t, on)
}

func TestTheSandboxChoiceSurvivesAReopen(t *testing.T) {
	ownHome(t)
	for _, want := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "agent.json")
		require.NoError(t, Open(path).SetSandbox(want))

		on, known := Open(path).Sandbox()
		require.True(t, known, "the choice was written but does not read back")
		assert.Equal(t, want, on)
	}
}

func TestRememberingTheSandboxKeepsTheRestOfTheMemory(t *testing.T) {
	ownHome(t)
	path := filepath.Join(t.TempDir(), "agent.json")
	held := Open(path)
	require.NoError(t, held.SetAgentSession("abc", "codex"))
	require.NoError(t, held.SetSandbox(false))

	again := Open(path)
	assert.Equal(t, "abc", again.Session())
	assert.Equal(t, "codex", again.Provider())
	on, known := again.Sandbox()
	assert.True(t, known)
	assert.False(t, on)
}

// An agent in the box can write agent.json; it must not be able to let itself out.
func TestTheSandboxChoiceIsNotWrittenWhereTheAgentCanReachIt(t *testing.T) {
	ownHome(t)
	path := filepath.Join(t.TempDir(), "agent.json")
	require.NoError(t, Open(path).SetSandbox(false))

	data, err := os.ReadFile(path)
	if err == nil {
		assert.NotContains(t, string(data), "sandbox")
	}
}

func TestANilMemoryHasNoOpinionAndTakesNoWrite(t *testing.T) {
	ownHome(t)
	var none *Log
	on, known := none.Sandbox()
	assert.False(t, known)
	assert.False(t, on)
	assert.NoError(t, none.SetSandbox(true))
}

func TestAMemoryWithNoPathRemembersNoSandboxChoice(t *testing.T) {
	ownHome(t)
	held := Open("")
	require.NoError(t, held.SetSandbox(false))
	_, known := held.Sandbox()
	assert.False(t, known)
}

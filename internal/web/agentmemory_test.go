package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/runlog"
)

// transcript writes the file Claude Code would have left for a session in dir,
// which is what makes it resumable.
func transcript(t *testing.T, home, dir, id string) {
	t.Helper()
	slug := strings.ReplaceAll(strings.TrimSuffix(dir, "/"), "/", "-")
	at := filepath.Join(home, ".claude", "projects", slug)
	require.NoError(t, os.MkdirAll(at, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(at, id+".jsonl"), []byte(`{"type":"user"}`+"\n"), 0o600))
}

// remembering opens a memory over a fresh git dir and seeds it.
func remembering(t *testing.T, gitDir, session string) *runlog.Log {
	t.Helper()
	mem := runlog.Open(runlog.Path(gitDir))
	require.NoError(t, mem.SetSession(session))
	return mem
}

func TestARestartComesBackToTheSameSession(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(dir, ".git")
	transcript(t, home, dir, "sess-abc")

	remembering(t, gitDir, "sess-abc")

	// A new process, told nothing, in the same checkout.
	runner := newAgentRunner(dir, "", "", "", "", sandboxConfig{}, runlog.Open(runlog.Path(gitDir)))

	state := runner.State()
	assert.Equal(t, "sess-abc", state.Session, "the conversation was not picked up")
	assert.Equal(t, "sess-abc", runner.Current(), "and it is the one the conversation is stored under")
	assert.Positive(t, state.SessionBytes, "the transcript it will replay was not measured")
}

func TestARememberedSessionIsDroppedOnceItsTranscriptIsGone(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(dir, ".git")

	// No transcript written: resuming this would fail every run from then on.
	remembering(t, gitDir, "sess-gone")

	runner := newAgentRunner(dir, "", "", "", "", sandboxConfig{}, runlog.Open(runlog.Path(gitDir)))
	assert.Empty(t, runner.State().Session)
	assert.Empty(t, runner.Current())
}

func TestAnExplicitSessionOverridesTheRememberedOne(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(dir, ".git")
	transcript(t, home, dir, "sess-abc")
	remembering(t, gitDir, "sess-abc")

	t.Run("a named session wins", func(t *testing.T) {
		runner := newAgentRunner(dir, "sess-named", "", "", "", sandboxConfig{}, runlog.Open(runlog.Path(gitDir)))
		assert.Equal(t, "sess-named", runner.State().Session)
	})

	t.Run("none starts fresh", func(t *testing.T) {
		// The way out of a session that has gone bad, without deleting a file
		// by hand.
		runner := newAgentRunner(dir, sessionNone, "", "", "", sandboxConfig{}, runlog.Open(runlog.Path(gitDir)))
		assert.Empty(t, runner.State().Session)
		assert.Zero(t, runner.State().SessionBytes)
	})
}

func TestTheSessionIsRecordedOnceTheAgentNamesIt(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(dir, ".git")
	path := runlog.Path(gitDir)

	runner := newAgentRunner(dir, "", "", "", "", sandboxConfig{}, runlog.Open(path))
	require.Empty(t, runner.State().Session, "a fresh agent has no session to resume")

	// A fresh session only names itself once it is running, which is how the
	// next process learns what to resume.
	var moved [][2]string
	runner.onSession = func(from, to string) { moved = append(moved, [2]string{from, to}) }
	runner.note("sess-fresh")
	assert.Equal(t, "sess-fresh", runlog.Open(path).Session())
	assert.Empty(t, runner.State().Session, "this run still resumed nothing")
	assert.Equal(t, "sess-fresh", runner.Current(), "but the conversation is now stored under it")
	assert.Equal(t, [][2]string{{"", "sess-fresh"}}, moved, "whoever holds the conversation is told to move it")

	// Said again on every event, which is not a move.
	runner.note("sess-fresh")
	assert.Len(t, moved, 1)

	runner.note("")
	assert.Equal(t, "sess-fresh", runlog.Open(path).Session(),
		"an event with no id says nothing")

	// And the next process in this checkout picks it up.
	transcript(t, home, dir, "sess-fresh")
	assert.Equal(t, "sess-fresh",
		newAgentRunner(dir, "", "", "", "", sandboxConfig{}, runlog.Open(path)).State().Session)
}

func TestRelocatingTakesOnTheNewCheckoutsMemory(t *testing.T) {
	from, to, home := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	transcript(t, home, to, "sess-there")
	remembering(t, filepath.Join(to, ".git"), "sess-there")

	runner := newAgentRunner(from, "sess-here", "", "", "", sandboxConfig{}, nil)
	runner.relocate(to, runlog.Open(runlog.Path(filepath.Join(to, ".git"))))

	// The old session cannot follow, because Claude Code keys transcripts by
	// working directory. The destination's own can.
	assert.Equal(t, "sess-there", runner.State().Session)
	assert.Equal(t, "sess-there", runner.Current())
}

func TestRelocatingSomewhereNewLeavesTheAgentFresh(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	t.Setenv("HOME", t.TempDir())

	runner := newAgentRunner(from, "sess-here", "", "", "", sandboxConfig{}, nil)
	runner.relocate(to, runlog.Open(runlog.Path(filepath.Join(to, ".git"))))

	// A worktree just created has no memory, and a fresh agent that says so is
	// better than one silently resuming the wrong tree.
	assert.Empty(t, runner.State().Session)
	assert.Empty(t, runner.Current())
}

func TestANewSessionForgetsTheOldOneAndTakesTheModel(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(dir, ".git")
	transcript(t, home, dir, "sess-abc")
	remembering(t, gitDir, "sess-abc")

	runner := newAgentRunner(dir, "", "", "sonnet", "", sandboxConfig{}, runlog.Open(runlog.Path(gitDir)))
	require.Equal(t, "sess-abc", runner.State().Session)

	runner.reset("opus")
	state := runner.State()
	assert.Empty(t, state.Session, "a new session resumes nothing")
	assert.Empty(t, runner.Current())
	assert.Zero(t, state.SessionBytes)
	assert.Equal(t, "opus", state.Model)

	// Naming no model keeps the one in use rather than forgetting it.
	runner.reset("")
	assert.Equal(t, "opus", runner.State().Model)

	// The memory is not touched until the new agent names itself, so a restart
	// before then lands back in the old conversation rather than nowhere.
	assert.Equal(t, "sess-abc", runlog.Open(runlog.Path(gitDir)).Session())
}

func TestResumingAnotherSessionOfTheSameCheckout(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	gitDir := filepath.Join(dir, ".git")
	transcript(t, home, dir, "sess-one")
	transcript(t, home, dir, "sess-two")
	remembering(t, gitDir, "sess-one")

	runner := newAgentRunner(dir, "", "", "", "", sandboxConfig{}, runlog.Open(runlog.Path(gitDir)))
	require.Equal(t, "sess-one", runner.State().Session)

	// The launcher hands a session to the server already running here, which
	// is the alternative to a rival server on the same checkout.
	runner.resume("sess-two")
	assert.Equal(t, "sess-two", runner.State().Session)
	assert.Equal(t, "sess-two", runner.Current())
	assert.Positive(t, runner.State().SessionBytes)
	assert.Equal(t, "sess-two", runlog.Open(runlog.Path(gitDir)).Session(), "and it is what the next restart resumes")
}

func TestTheReplayIsAnnouncedOncePerProcessNotPerTurn(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	transcript(t, home, dir, "sess-abc")

	// cat stands in for claude: it starts, reads stdin and says nothing, which
	// is all this needs — a process that stays up across two ensure calls.
	runner := newAgentRunner(dir, "sess-abc", "/bin/cat", "", "", sandboxConfig{}, nil)
	require.Positive(t, runner.State().SessionBytes)
	var heard []string
	runner.onStep = func(text string) { heard = append(heard, text) }
	defer runner.Close()

	for range 3 {
		_, err := runner.ensure(context.Background(), func(bridge.Event) {})
		require.NoError(t, err)
	}

	// The transcript is replayed when the process starts, not on every message
	// sent to it, so the conversation says so once rather than every turn.
	require.Len(t, heard, 1)
	assert.Contains(t, heard[0], "resuming a")
}

package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/verify"
)

// listed writes a check list for the test server's repository.
func listed(t *testing.T, s *Server, checks ...verify.Check) {
	t.Helper()
	require.NoError(t, verify.Save(s.verifyPath(), verify.Config{Checks: checks, Asked: true}))
}

// notes is the conversation's notes, in order, which is where a verdict goes.
func notes(s *Server) []chat.Message {
	var out []chat.Message
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.Kind == chat.KindNote {
			out = append(out, msg)
		}
	}
	return out
}

// touched makes the diff move, which is what tells a turn it changed files.
func touched(t *testing.T, s *Server) string {
	t.Helper()
	was := s.currentHash()
	require.NoError(t, os.WriteFile(filepath.Join(s.space().homeRoot, "a.go"),
		[]byte("package main\n\nvar x = 99\n"), 0o600))
	require.NoError(t, s.refresh(t.Context()))
	require.NotEqual(t, was, s.currentHash(), "the diff should have moved")
	return was
}

func TestChecksRunAfterATurnThatChangedSomething(t *testing.T) {
	s := newTestServer(t)
	listed(t, s, verify.Check{Name: "build", Run: "true"}, verify.Check{Name: "vet", Run: "true"})

	s.verifyAfterTurn(touched(t, s))

	require.Len(t, notes(s), 1)
	assert.Equal(t, "checks passed (2)", notes(s)[0].Body)
	assert.False(t, notes(s)[0].Failed)
	assert.Empty(t, s.heardSoFar(), "a pass is not worth the agent's window")

	state := s.verifyState()
	require.NotNil(t, state)
	assert.True(t, state.Passed)
	assert.Equal(t, 2, state.Ran)
	assert.Equal(t, 2, state.Total)
}

func TestATurnThatChangedNothingRunsNoChecks(t *testing.T) {
	s := newTestServer(t)
	listed(t, s, verify.Check{Name: "build", Run: "touch should-not-run"})

	s.verifyAfterTurn(s.currentHash())

	assert.Empty(t, notes(s))
	assert.Nil(t, s.verifyState())
	assert.NoFileExists(t, filepath.Join(s.space().homeRoot, "should-not-run"))
}

func TestAFailingCheckStopsTheListAndTellsTheAgent(t *testing.T) {
	s := newTestServer(t)
	listed(t, s,
		verify.Check{Name: "build", Run: "true"},
		verify.Check{Name: "vet", Run: "echo 'a.go:3: undefined: x' >&2; exit 1"},
		verify.Check{Name: "format", Run: "touch should-not-run"})

	s.verifyAfterTurn(touched(t, s))

	require.Len(t, notes(s), 1)
	note := notes(s)[0]
	assert.Equal(t, "vet failed", note.Body)
	assert.True(t, note.Failed)
	assert.Contains(t, note.Output, "undefined: x")
	assert.NoFileExists(t, filepath.Join(s.space().homeRoot, "should-not-run"))

	heard := s.heardSoFar()
	require.Len(t, heard, 1)
	assert.Equal(t, "ade", heard[0].Who)
	assert.Contains(t, heard[0].Text, "vet failed")
	assert.Contains(t, heard[0].Text, "undefined: x")
	assert.Contains(t, heard[0].Text, "Fix the cause")

	state := s.verifyState()
	require.NotNil(t, state)
	assert.False(t, state.Passed)
	assert.Equal(t, "vet", state.Failed)
	assert.Equal(t, 2, state.Ran)
	assert.Equal(t, 3, state.Total)
}

func TestStopEndsARunningCheckList(t *testing.T) {
	s := newTestServer(t)
	listed(t, s, verify.Check{Name: "slow", Run: "sleep 30"})
	was := touched(t, s)

	done := make(chan struct{})
	go func() {
		s.verifyAfterTurn(was)
		close(done)
	}()
	require.Eventually(t, func() bool {
		state := s.verifyState()
		return state != nil && state.Running
	}, 3*time.Second, 10*time.Millisecond)

	// The Stop button, which takes the turn and what ade started after it.
	require.Equal(t, http.StatusNoContent, do(t, s, "POST", "/api/cancel", "").Code)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the checks were never stopped")
	}

	require.Len(t, notes(s), 1)
	assert.Equal(t, "checks stopped", notes(s)[0].Body)
	assert.Empty(t, s.heardSoFar(), "you stopping it is not a failure to fix")
}

func TestARepositoryWithNoListIsOfferedOneOnce(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o600))

	s.verifyAfterTurn(touched(t, s))

	state := s.verifyState()
	require.NotNil(t, state)
	assert.Equal(t, []verify.Check{
		{Name: "build", Run: "go build ./..."},
		{Name: "vet", Run: "go vet ./..."},
	}, state.Offer)
	assert.Empty(t, notes(s), "an offer is not a verdict")

	t.Run("declining is remembered", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/verify", `{"checks":[]}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Nil(t, s.verifyState())

		s.verifyAfterTurn("something-else")
		assert.Nil(t, s.verifyState(), "the offer is made once")
	})
}

func TestANodeOnlyRepositoryWithNothingToDetectIsNotAskedTwice(t *testing.T) {
	s := newTestServer(t)
	s.verifyAfterTurn(touched(t, s))
	assert.Nil(t, s.verifyState())

	config, err := verify.Load(s.verifyPath())
	require.NoError(t, err)
	assert.True(t, config.Asked, "nothing to detect is still an answer")
}

func TestTheListIsServedSavedAndRunOnDemand(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/verify", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"checks":[],"asked":false,"offer":null,"detected":null}`, rec.Body.String())

	rec = do(t, s, "POST", "/api/verify/run", "")
	assert.Equal(t, http.StatusConflict, rec.Code, "nothing to run")

	rec = do(t, s, "POST", "/api/verify", `{"checks":[{"name":" build ","run":" true "}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	config, err := verify.Load(s.verifyPath())
	require.NoError(t, err)
	assert.Equal(t, []verify.Check{{Name: "build", Run: "true"}}, config.Checks, "trimmed on the way in")

	rec = do(t, s, "POST", "/api/verify/run", "")
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Eventually(t, func() bool {
		state := s.verifyState()
		return state != nil && !state.Running && state.Passed
	}, 5*time.Second, 10*time.Millisecond)
}

func TestAHalfWrittenCheckIsRefused(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/verify", `{"checks":[{"name":"build","run":"  "}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing to run")
}

// The list is ade's own state. A repository that ships one cannot make ade
// run it: that would be a command from a stranger, with your credentials.
func TestACheckListCommittedInTheRepositoryIsIgnored(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	hostile := `{"checks":[{"name":"pwn","run":"touch pwned"}]}`
	for _, name := range []string{"checks.json", ".ade-checks.json"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(hostile), 0o600))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".ade"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".ade", "checks.json"), []byte(hostile), 0o600))

	s.verifyAfterTurn(touched(t, s))

	assert.NoFileExists(t, filepath.Join(root, "pwned"))
	assert.NotContains(t, s.verifyPath(), root+string(filepath.Separator)+".ade")
}

func TestANarrowPassReportsThenTheFullListDecides(t *testing.T) {
	s := newTestServer(t)
	listed(t, s,
		verify.Check{Name: "test", Run: "touch full-ran", Narrow: "echo {{.Files}} > narrow-ran"},
		verify.Check{Name: "vet", Run: "true"})

	s.verifyAfterTurn(touched(t, s))

	require.Len(t, notes(s), 2)
	assert.Equal(t, "narrow checks passed (1), running the full list", notes(s)[0].Body)
	assert.Equal(t, "checks passed (2)", notes(s)[1].Body)
	root := s.space().homeRoot
	seen, err := os.ReadFile(filepath.Join(root, "narrow-ran"))
	require.NoError(t, err)
	assert.Equal(t, "a.go\n", string(seen))
	assert.FileExists(t, filepath.Join(root, "full-ran"))

	state := s.verifyState()
	require.NotNil(t, state)
	assert.True(t, state.Passed)
	assert.False(t, state.Narrow, "the full outcome replaces the narrow one")
}

func TestANarrowFailureStopsBeforeTheFullList(t *testing.T) {
	s := newTestServer(t)
	listed(t, s, verify.Check{Name: "test", Run: "touch should-not-run",
		Narrow: "echo 'a.go:3: boom' >&2; false {{.Files}}"})

	s.verifyAfterTurn(touched(t, s))

	require.Len(t, notes(s), 1)
	assert.Equal(t, "test failed", notes(s)[0].Body)
	assert.NoFileExists(t, filepath.Join(s.space().homeRoot, "should-not-run"))
	require.Len(t, s.heardSoFar(), 1)
	assert.Contains(t, s.heardSoFar()[0].Text, "false a.go", "the agent is shown the command that ran")

	state := s.verifyState()
	require.NotNil(t, state)
	assert.True(t, state.Narrow)
	assert.False(t, state.Passed)
	assert.False(t, state.Running)
	assert.Equal(t, "test", state.Failed)
}

func TestRunningTheChecksByHandSkipsTheNarrowPass(t *testing.T) {
	s := newTestServer(t)
	listed(t, s, verify.Check{Name: "test", Run: "true", Narrow: "touch should-not-run {{.Files}}"})
	touched(t, s)

	s.runChecks([]verify.Check{{Name: "test", Run: "true", Narrow: "touch should-not-run {{.Files}}"}}, nil)

	require.Len(t, notes(s), 1)
	assert.Equal(t, "checks passed (1)", notes(s)[0].Body)
	assert.NoFileExists(t, filepath.Join(s.space().homeRoot, "should-not-run"))
}

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/quiz"
)

// idle waits for the accepted run to let the agent go.
func idle(t *testing.T, s *Server) {
	t.Helper()
	require.Eventually(t, func() bool { return !s.agent.State().Running },
		5*time.Second, 10*time.Millisecond)
}

// answering puts a reply in the agent's mouth and counts how often it is asked.
func answering(t *testing.T, text string) *int {
	t.Helper()
	asked := 0
	old := runOneShot
	runOneShot = func(_ context.Context, _ bridge.OneShotOpts) (*bridge.Turn, error) {
		asked++
		return &bridge.Turn{Text: text}, nil
	}
	t.Cleanup(func() { runOneShot = old })
	return &asked
}

// threeAbout is a reply with the given number of usable questions, all
// anchored to the one file newTestServer's diff carries.
func threeAbout(n int) string {
	refs := []string{"Q1", "Q2", "Q3", "Q4", "Q5", "Q6"}
	out := ""
	for i := 0; i < n; i++ {
		out += "```" + quiz.Fence + "\n" +
			`{"ref":"` + refs[i] + `","file":"a.go","line":3,` +
			`"question":"What does the caller get now?",` +
			`"options":["A zero","An error","Nothing at all"],` +
			`"answer":"B","why":"The early return skips the assignment."}` +
			"\n```\n"
	}
	return out
}

func TestQuizRefusesAnEmptyDiff(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.diff = nil
	s.mu.Unlock()

	rec := do(t, s, "POST", "/api/quiz", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing to quiz")
	assert.False(t, s.agent.State().Running, "a rejected request must not claim the agent")
}

// You quiz while the agent is still working, the same as you stage and
// describe while it works.
func TestQuizQueuesBehindARunningTurn(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/quiz", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	waiting := s.agentState().Waiting
	require.Len(t, waiting, 1)
	assert.Equal(t, "Quiz me", waiting[0].Kind)
	assert.Equal(t, "write a quiz about the diff", waiting[0].What)
}

func TestQuizRunsOnTheDiffAndLandsInState(t *testing.T) {
	s := newTestServer(t)
	asked := answering(t, threeAbout(4))

	require.NoError(t, s.quizOnce(context.Background(), s.diff))
	assert.Equal(t, 1, *asked)

	var got stateResponse
	require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/state", "").Body.Bytes(), &got))
	require.NotNil(t, got.Quiz)
	assert.Len(t, got.Quiz.Questions, 4)
	assert.Equal(t, 1, got.Quiz.Files)
	assert.False(t, got.Quiz.Stale)
	assert.NotEmpty(t, got.Quiz.Questions[0].Why, "the answer is shown with its reason")
}

// A second call on the same diff is the cache, not another twenty seconds.
func TestASecondQuizOnTheSameDiffDoesNotRunTheAgent(t *testing.T) {
	s := newTestServer(t)
	asked := answering(t, threeAbout(3))

	require.NoError(t, s.quizOnce(context.Background(), s.diff))
	require.Equal(t, 1, *asked)

	rec := do(t, s, "POST", "/api/quiz", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"questions"`)
	assert.Equal(t, 1, *asked, "the held quiz is handed back")
	assert.False(t, s.agent.State().Running)
}

// Reopening a quiz you have already taken would test your memory.
func TestRegenerateBypassesTheCache(t *testing.T) {
	s := newTestServer(t)
	asked := answering(t, threeAbout(3))
	require.NoError(t, s.quizOnce(context.Background(), s.diff))

	rec := do(t, s, "POST", "/api/quiz", `{"regenerate":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	idle(t, s)
	assert.Equal(t, 2, *asked)
}

// The quiz is for the minutes before a pull request, but the panel still has
// to say when it is describing a diff that has moved.
func TestAQuizIsMarkedStaleWhenTheDiffMoves(t *testing.T) {
	s := newTestServer(t)
	answering(t, threeAbout(3))
	require.NoError(t, s.quizOnce(context.Background(), s.diff))
	require.False(t, s.currentQuiz().Stale)

	s.mu.Lock()
	s.diffHash = "something else"
	s.mu.Unlock()

	assert.True(t, s.currentQuiz().Stale)
	// And a stale quiz is not what a plain ask gets back.
	rec := do(t, s, "POST", "/api/quiz", `{}`)
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	idle(t, s)
}

// Under three questions the server asks once more and keeps the larger set.
func TestTooFewQuestionsAreAskedForAgain(t *testing.T) {
	s := newTestServer(t)
	asked := 0
	old := runOneShot
	runOneShot = func(_ context.Context, _ bridge.OneShotOpts) (*bridge.Turn, error) {
		asked++
		if asked == 1 {
			return &bridge.Turn{Text: threeAbout(1)}, nil
		}
		return &bridge.Turn{Text: threeAbout(3)}, nil
	}
	t.Cleanup(func() { runOneShot = old })

	require.NoError(t, s.quizOnce(context.Background(), s.diff))
	assert.Equal(t, 2, asked)
	assert.Len(t, s.currentQuiz().Questions, 3)
}

// A quiz of one is not a confidence check of anything, so it says so rather
// than draw one.
func TestStillTooFewQuestionsIsSaidPlainly(t *testing.T) {
	s := newTestServer(t)
	answering(t, threeAbout(1))

	err := s.quizOnce(context.Background(), s.diff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a confidence check")
	assert.Nil(t, s.currentQuiz())
}

// The reading is for the context a hunk does not carry, so no Bash.
func TestTheQuizRunReadsAndRunsNothing(t *testing.T) {
	s := newTestServer(t)
	var opts bridge.OneShotOpts
	old := runOneShot
	runOneShot = func(_ context.Context, got bridge.OneShotOpts) (*bridge.Turn, error) {
		opts = got
		return &bridge.Turn{Text: threeAbout(3)}, nil
	}
	t.Cleanup(func() { runOneShot = old })

	require.NoError(t, s.quizOnce(context.Background(), s.diff))

	assert.Equal(t, outgoing.FastModel, opts.Model)
	assert.True(t, opts.NoMCP)
	assert.NotContains(t, opts.AllowedTools, "Bash")
	assert.Contains(t, opts.Prompt, "var x = 2", "the diff is handed over rendered")
}

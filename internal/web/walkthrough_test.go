package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/walkthrough"
)

// stepsAbout is a reply with the given number of usable steps, all anchored to
// the one file newTestServer's diff carries.
func stepsAbout(n int) string {
	refs := []string{"S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8"}
	out := ""
	for i := 0; i < n; i++ {
		out += "```" + walkthrough.StepFence + "\n" +
			`{"ref":"` + refs[i] + `","title":"The value is read once",` +
			`"file":"a.go","line":3,"endLine":3,"node":"pkg",` +
			`"body":"Every caller now gets 2, so nothing downstream reads 1 again."}` +
			"\n```\n"
	}
	return out + "```" + walkthrough.FlowFence + "\n" +
		`{"nodes":[{"id":"pkg","label":"package main","file":"a.go","line":3},` +
		`{"id":"caller","label":"Whatever reads x"}],` +
		`"edges":[{"from":"pkg","to":"caller","label":"x"}]}` + "\n```\n"
}

func TestWalkthroughRefusesAnEmptyDiff(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.diff = nil
	s.mu.Unlock()

	rec := do(t, s, "POST", "/api/walkthrough", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing to explain")
	assert.False(t, s.agent.State().Running, "a rejected request must not claim the agent")
}

func TestWalkthroughQueuesBehindARunningTurn(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/walkthrough", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	waiting := s.agentState().Waiting
	require.Len(t, waiting, 1)
	assert.Equal(t, "Explain this change", waiting[0].Kind)
	assert.Equal(t, "read the diff back as steps", waiting[0].What)
}

func TestWalkthroughRunsOnTheDiffAndLandsInState(t *testing.T) {
	s := newTestServer(t)
	asked := answering(t, stepsAbout(4))

	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))
	assert.Equal(t, 1, *asked)

	var got stateResponse
	require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/state", "").Body.Bytes(), &got))
	require.NotNil(t, got.Walkthrough)
	assert.Len(t, got.Walkthrough.Steps, 4)
	assert.Equal(t, 1, got.Walkthrough.Files)
	assert.False(t, got.Walkthrough.Stale)
	require.NotNil(t, got.Walkthrough.Flow)
	assert.Len(t, got.Walkthrough.Flow.Nodes, 2)
}

// The code beside the words comes with them: the panel does not fetch a file
// per step, and the lines cannot drift from the hash this is keyed on.
func TestEachStepCarriesTheLinesItIsAbout(t *testing.T) {
	s := newTestServer(t)
	answering(t, stepsAbout(3))

	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))

	excerpt := s.currentWalkthrough().Steps[0].Excerpt
	require.NotEmpty(t, excerpt)
	assert.Equal(t, 1, excerpt[0].N, "the context starts at the top of a short file")
	marked := []string{}
	for _, line := range excerpt {
		if line.In {
			marked = append(marked, line.Text)
		}
	}
	assert.Equal(t, []string{"var x = 2"}, marked)
}

// A step pointed at a file that has since gone still reads; it just has no
// code beside it.
func TestAStepWhoseFileWillNotReadKeepsItsWords(t *testing.T) {
	s := newTestServer(t)
	step := walkthrough.Step{File: "gone.go", Line: 1, EndLine: 1}
	assert.Nil(t, excerptOf(context.Background(), s.space(), step))
}

// A committed scope's line numbers are HEAD's: the excerpt comes from the
// commit, not from whatever the working tree has become since.
func TestAnExcerptComesFromTheSideTheDiffShows(t *testing.T) {
	s := newTestServer(t)
	ws := *s.space()
	ws.opts.Scope = gitdiff.ScopeCommitted
	require.NoError(t, os.WriteFile(filepath.Join(ws.homeRoot, "a.go"),
		[]byte("package main\n\n// added since\n\nvar x = 2\n"), 0o600))

	excerpt := excerptOf(context.Background(), &ws, walkthrough.Step{File: "a.go", Line: 3, EndLine: 3})

	require.Len(t, excerpt, 3)
	assert.Equal(t, "var x = 2", excerpt[2].Text, "HEAD's line 3, not the tree's")
}

func TestASecondWalkthroughOnTheSameDiffDoesNotRunTheAgent(t *testing.T) {
	s := newTestServer(t)
	asked := answering(t, stepsAbout(3))

	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))
	require.Equal(t, 1, *asked)

	rec := do(t, s, "POST", "/api/walkthrough", `{}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"steps"`)
	assert.Equal(t, 1, *asked, "the held walkthrough is handed back")
	assert.False(t, s.agent.State().Running)
}

func TestRegenerateBypassesTheHeldWalkthrough(t *testing.T) {
	s := newTestServer(t)
	asked := answering(t, stepsAbout(3))
	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))

	rec := do(t, s, "POST", "/api/walkthrough", `{"regenerate":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	idle(t, s)
	assert.Equal(t, 2, *asked)
}

// A walk through code that has changed underneath is worse than none, so the
// panel is told, and a plain ask starts again.
func TestAWalkthroughIsMarkedStaleWhenTheDiffMoves(t *testing.T) {
	s := newTestServer(t)
	answering(t, stepsAbout(3))
	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))
	require.False(t, s.currentWalkthrough().Stale)

	s.mu.Lock()
	s.diffHash = "something else"
	s.mu.Unlock()

	assert.True(t, s.currentWalkthrough().Stale)
	rec := do(t, s, "POST", "/api/walkthrough", `{}`)
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	idle(t, s)
}

// A walkthrough that has to queue must not leave the old steps in state: the
// panel waits on it and would take them for the new ones.
func TestAQueuedWalkthroughDropsTheHeldOne(t *testing.T) {
	s := newTestServer(t)
	answering(t, stepsAbout(3))
	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))
	require.NotNil(t, s.currentWalkthrough())

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)
	defer s.dropStack()

	rec := do(t, s, "POST", "/api/walkthrough", `{"regenerate":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Nil(t, s.currentWalkthrough())
}

func TestTooFewStepsAreAskedForAgain(t *testing.T) {
	s := newTestServer(t)
	asked := 0
	old := runOneShot
	runOneShot = func(_ context.Context, _ bridge.OneShotOpts) (*bridge.Turn, error) {
		asked++
		if asked == 1 {
			return &bridge.Turn{Text: stepsAbout(1)}, nil
		}
		return &bridge.Turn{Text: stepsAbout(3)}, nil
	}
	t.Cleanup(func() { runOneShot = old })

	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))
	assert.Equal(t, 2, asked)
	assert.Len(t, s.currentWalkthrough().Steps, 3)
}

func TestStillTooFewStepsIsSaidPlainly(t *testing.T) {
	s := newTestServer(t)
	answering(t, stepsAbout(1))

	err := s.walkthroughOnce(context.Background(), s.diff)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a walk through anything")
	assert.Nil(t, s.currentWalkthrough())
}

// Ordering the steps is the whole job, which is the part a fast model does
// worst: this one runs on the session's model.
func TestTheWalkthroughRunReadsAndRunsNothing(t *testing.T) {
	s := newTestServer(t)
	var opts bridge.OneShotOpts
	old := runOneShot
	runOneShot = func(_ context.Context, got bridge.OneShotOpts) (*bridge.Turn, error) {
		opts = got
		return &bridge.Turn{Text: stepsAbout(3)}, nil
	}
	t.Cleanup(func() { runOneShot = old })

	require.NoError(t, s.walkthroughOnce(context.Background(), s.diff))

	assert.NotEqual(t, outgoing.FastModel, opts.Model)
	assert.True(t, opts.NoMCP)
	assert.NotContains(t, opts.AllowedTools, "Bash")
	assert.Contains(t, opts.Prompt, "var x = 2", "the diff is handed over rendered")
}

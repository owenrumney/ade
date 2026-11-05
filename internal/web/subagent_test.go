package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/review"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/settings"
)

func defineAgent(t *testing.T, s *Server, name, body string) {
	t.Helper()
	dir := filepath.Join(s.space().homeRoot, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600))
}

// defineUserAgent writes an agent into the user's own scope, which is where a
// review resolves its reviewers from. HOME must already be redirected.
func defineUserAgent(t *testing.T, name, body string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600))
}

func TestAgentsAreListedFromTheCheckout(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	rec := do(t, s, "GET", "/api/agents", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"agents":[],"skipped":[]}`, rec.Body.String())

	defineAgent(t, s, "staff-reviewer", "---\nname: staff-reviewer\ndescription: critiques\nmodel: opus\ntools: Read, Grep\n---\nbe hard on it\n")
	defineAgent(t, s, "broken", "---\nname: broken\nmodel: gemini/pro\n---\n")
	rec = do(t, s, "GET", "/api/agents", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"staff-reviewer"`)
	assert.NotContains(t, rec.Body.String(), `"name":"broken"`)
	assert.Contains(t, rec.Body.String(), `unknown provider \"gemini\"`, "a file that is not an agent is reported, not hidden")
	assert.Contains(t, rec.Body.String(), `"provider":"claude"`)
	assert.Contains(t, rec.Body.String(), `"scope":"repo"`)
	// The prompt is the agent's own business, not the page's.
	assert.NotContains(t, rec.Body.String(), "be hard on it")
}

func TestAFindingOutsideTheDiffIsSaidRatherThanFiled(t *testing.T) {
	// The test repo's only change is on new-side line 3 of a.go.
	s := newTestServer(t)
	f := s.newFiler(reviewingAgent("second-opinion"), "", "")
	f.push("```ade-finding\n" +
		`{"file": ".git/ade/docs/main.md", "line": 12, "category": "design", "severity": "high", "summary": "the cache has no eviction"}` +
		"\n```\n")
	f.push("```ade-finding\n" +
		`{"file": "a.go", "line": 3, "category": "correctness", "severity": "high", "summary": "x is magic"}` +
		"\n```\n")
	f.done()

	threads := s.ws.store.List()
	require.Len(t, threads, 1, "only the finding on the diff becomes a comment")
	assert.Equal(t, "a.go", threads[0].File)

	// The brief is in no diff, so its finding is said rather than filed.
	require.Len(t, f.aside, 1)
	assert.Contains(t, lastSaid(s), "outside the diff")
	assert.Contains(t, lastSaid(s), "the cache has no eviction")
}

func TestReviewWithANamedAgentIsASideRunInTheChat(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")

	rec := do(t, s, "POST", "/api/review", `{"agent":"nobody"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no agent called @nobody")

	defineUserAgent(t, "staff-reviewer", "---\nname: staff-reviewer\n---\nbe hard on it\n")
	rec = do(t, s, "POST", "/api/review", `{"agent":"staff-reviewer"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"agents":["staff-reviewer"]`)
	// The main agent was not claimed.
	assert.False(t, s.agent.State().Running)
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)

	// Without an agent it is the cold review, as before.
	rec = do(t, s, "POST", "/api/review", `{"level":"medium"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.True(t, s.agent.State().Running)
	s.agent.Cancel()
}

func TestSeveralAgentsCanReviewTheDiffAtOnce(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineUserAgent(t, "staff-reviewer", "---\nname: staff-reviewer\n---\nbe hard on it\n")
	defineUserAgent(t, "security", "---\nname: security\n---\ntrust nothing\n")

	// One unknown name and none of them run: the ask is all or nothing.
	rec := do(t, s, "POST", "/api/review", `{"agents":["staff-reviewer","nobody"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, "POST", "/api/review", `{"agents":["staff-reviewer","security"," staff-reviewer "]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"agents":["staff-reviewer","security"]`, "repeats collapse")
	assert.False(t, s.agent.State().Running)
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)
}

func TestReviewersAreTrimmedAndUnique(t *testing.T) {
	assert.Nil(t, reviewers(nil, ""))
	assert.Equal(t, []string{"a"}, reviewers(nil, " a "))
	assert.Equal(t, []string{"a", "b"}, reviewers([]string{"a", "", "b", "a"}, "b"))
}

func TestARepositoryAgentGetsReadOnlyToolsWhateverItAsksFor(t *testing.T) {
	readOnly := []string{"Read", "Grep", "Glob"}
	for _, tc := range []struct {
		name  string
		agent agents.Agent
		want  []string
	}{
		{"nothing named is read-only", agents.Agent{Scope: agents.ScopeRepo}, readOnly},
		{"nothing named, even for the user", agents.Agent{Scope: agents.ScopeUser}, readOnly},
		{"a repo agent cannot grant itself the shell", agents.Agent{Scope: agents.ScopeRepo, Tools: []string{"Bash", "Edit", "Read"}}, []string{"Read"}},
		{"a repo agent naming only write tools gets the read set", agents.Agent{Scope: agents.ScopeRepo, Tools: []string{"Bash"}}, readOnly},
		{"the user's own agent is trusted as written", agents.Agent{Scope: agents.ScopeUser, Tools: []string{"Bash", "Edit"}}, []string{"Bash", "Edit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, agentTools(tc.agent))
		})
	}
}

func TestTooManySideRunsIsANoteNotARun(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defineAgent(t, s, "poet", "---\nname: poet\n---\nverse\n")
	defineUserAgent(t, "poet", "---\nname: poet\n---\nverse\n")
	s.mu.Lock()
	s.sides = map[string]sideRun{}
	for i := 0; i < maxSides; i++ {
		s.sides[itoa(i)] = sideRun{name: "busy", cancel: func() {}}
	}
	s.mu.Unlock()

	rec := do(t, s, "POST", "/api/review", `{"agent":"poet"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	// The numbers, not just "no": which of the four it was is the whole point.
	assert.Contains(t, lastSaid(s), "4 agent(s) running")
	// Three for agents, of four: the fourth is the rewrite's.
	assert.Contains(t, lastSaid(s), "3 at most for agents, of 4")
}

func TestASessionsTitleFollowsItOntoTheRunner(t *testing.T) {
	// HOME before the server: cleanups run last-first, so this way the
	// server is closed before the directory its goroutines write under goes.
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	require.NoError(t, settings.SetTitle("sess-1", "Rate limit the webhook"))

	// A fresh agent naming itself as a known session picks the title up.
	s.agent.note("sess-1")
	assert.Equal(t, "Rate limit the webhook", s.agent.State().Title)
}

func TestStopEndsNamedAgentsToo(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.sides = map[string]sideRun{"1": {name: "poet", cancel: cancel}}
	s.mu.Unlock()
	require.Len(t, s.sideRuns(), 1)

	rec := do(t, s, "POST", "/api/cancel", "")
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Error(t, ctx.Err(), "the side run was cancelled")
}

func TestANamedAgentsFindingBlocksAreFiledAsComments(t *testing.T) {
	// The test repo's only change is "var x = 2" on new-side line 3.
	s := newTestServer(t)
	// The shipped definition: @commenter reviews nothing and still files.
	f := s.newFiler(agents.Agent{Name: commenterAgent}, "security", "")

	// A block is a comment from the agent that asked, not the one that
	// filed it; the prose around it stays prose.
	f.push("Filing.\n\n```ade-finding\n{\"file\": \"a.go\", \"line\": 3, \"category\": \"security\", \"summary\": \"x is magic\", \"failure_scenario\": \"always\"}\n```\n")
	f.push("```ade-finding\n{\"file\": \"a.go\", \"line\": 3, \"category\": \"security\", \"summary\": \"x is magic\"}\n```\n")
	f.done()

	threads := s.space().store.List()
	require.Len(t, threads, 1, "the same finding twice is filed once")
	assert.Equal(t, "a.go", threads[0].File)
	assert.Equal(t, 3, threads[0].Line)
	assert.Equal(t, "security", threads[0].Category)
	require.Len(t, threads[0].Comments, 1)
	assert.Equal(t, "security", threads[0].Comments[0].Author)
	assert.Equal(t, "x is magic\n\nFails when: always", threads[0].Comments[0].Body)

	assert.Equal(t, "@commenter filed 1 comment(s) on the diff as @security", lastSaid(s))

	// A second reviewer landing on the same line joins the thread that is
	// there rather than opening another beside it.
	own := s.newFiler(reviewingAgent("commenter"), "", "")
	own.push("```ade-finding\n{\"file\": \"a.go\", \"line\": 3, \"summary\": \"also\"}\n```")
	threads = s.space().store.List()
	require.Len(t, threads, 1)
	require.Len(t, threads[0].Comments, 2)
	assert.Equal(t, "commenter", threads[0].Comments[1].Author)
	assert.Equal(t, "also", threads[0].Comments[1].Body)

	// Nothing filed is nothing said.
	before := lastSaid(s)
	s.newFiler(reviewingAgent("poet"), "", "").done()
	assert.Equal(t, before, lastSaid(s))
}

func TestDiffReviewersAreToldAboutTheCommenterWhenThereIsOne(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defineAgent(t, s, "security", "---\nname: security\n---\ntrust nothing\n")
	assert.Equal(t, "", commenterFor(s.agentsList()))

	defineAgent(t, s, "commenter", "---\nname: commenter\n---\nfile it\n")
	assert.Equal(t, "commenter", commenterFor(s.agentsList()))
}

func TestOneSideRunStopsWithoutTheRest(t *testing.T) {
	s := newTestServer(t)

	started, release := make(chan string, 2), make(chan struct{})
	job := func(name string) sideJob {
		return sideJob{name: name, run: func(ctx context.Context) {
			started <- name
			select {
			case <-ctx.Done():
			case <-release:
			}
		}}
	}
	require.True(t, s.startJobs([]sideJob{job("reviewer"), job("tester")}, 0))
	<-started
	<-started

	runs := s.sideRuns()
	require.Len(t, runs, 2)
	// Oldest first and stable, or the rows shuffle under the pointer between
	// polls and Stop hits whichever one moved into place.
	assert.NotEmpty(t, runs[0].ID)
	assert.NotEqual(t, runs[0].ID, runs[1].ID)

	victim := runs[0]
	assert.True(t, s.stopSide(victim.ID))
	assert.False(t, s.stopSide("no-such-run"), "an id that names nothing stops nothing")

	require.Eventually(t, func() bool {
		left := s.sideRuns()
		return len(left) == 1 && left[0].ID != victim.ID
	}, 2*time.Second, 10*time.Millisecond, "the other agent went too")

	close(release)
}

// reviewingAgent is a bare definition that reviews, for the filer's tests.
func reviewingAgent(name string) agents.Agent {
	return agents.Agent{Name: name, Reviews: true}
}

func TestMCPIsOnlyStartedForAnAgentThatNamedOne(t *testing.T) {
	assert.False(t, wantsMCP(review.ReadOnlyTools()))
	assert.False(t, wantsMCP(nil))
	assert.True(t, wantsMCP([]string{"Read", "mcp__linear__list_issues"}))
}

func TestAReviewLeavesRoomForARewrite(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	named := []sideJob{{name: "reviewer", run: func(context.Context) {}}}
	cold := []sideJob{{name: kindConvert, cold: true, run: func(context.Context) {}}}

	assert.Equal(t, maxSides-coldReserve, slotsFor(named), "a named agent leaves the reserve")
	assert.Equal(t, maxSides, slotsFor(cold), "a one-shot may use it")
	assert.Equal(t, maxSides-coldReserve, slotsFor(append(append([]sideJob{}, cold...), named...)))

	s.mu.Lock()
	s.sides = map[string]sideRun{}
	for i := 0; i < maxSides-coldReserve; i++ {
		s.sides[itoa(i)] = sideRun{name: "reviewer", cancel: func() {}}
	}
	s.mu.Unlock()

	assert.False(t, s.startJobs(named, 0), "no room for another agent")
	assert.True(t, s.startJobs(cold, 0), "but the rewrite still goes")
}

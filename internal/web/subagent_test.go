package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/review"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/said"
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

func TestPiDelegationRequiresItsOwnCapabilityToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "delegate-token"

	rec := do(t, s, "POST", "/api/internal/delegate", `{"name":"reviewer","prompt":"check this"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = do(t, s, "GET", "/api/internal/delegate/agents", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)

	req := httptest.NewRequest("POST", "/api/internal/delegate", strings.NewReader(`{"name":"reviewer","prompt":"check this"}`))
	req.Header.Set("X-Ade-Delegate-Token", s.delegateToken)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `unknown agent \"reviewer\"`)

	defineAgent(t, s, "reviewer", "---\nname: reviewer\ndescription: checks changes\n---\nreview\n")
	req = httptest.NewRequest("GET", "/api/internal/delegate/agents", nil)
	req.Header.Set("X-Ade-Delegate-Token", s.delegateToken)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.JSONEq(t, `{"agents":[{"name":"reviewer","description":"checks changes"}]}`, rec.Body.String())
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

func TestAMentionOfNobodyGoesToTheMainAgent(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/chat", `{"message":"@nobody are you there"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"queued":`)
	assert.NotContains(t, rec.Body.String(), `"agents":`)
	assert.Equal(t, 1, s.stacked())
	assert.Empty(t, s.sideRuns())
}

func TestAMentionRunsBesideTheMainAgent(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	// No claude to run, so the side run fails fast; what matters here is that
	// it was a side run and not a turn.
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineAgent(t, s, "poet", "---\nname: poet\n---\nverse\n")
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/chat", `{"message":"@poet a haiku"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"agents":["poet"]`)
	// Not queued behind the review: it has its own process.
	assert.Equal(t, 0, s.stacked())

	// What you said is in the conversation as said, mention and all.
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	assert.Equal(t, "@poet a haiku", yours[0].Body)

	// The failure lands in the conversation under the agent's name.
	assert.Eventually(t, func() bool {
		for _, msg := range messagesOfKind(s.conversation(), chat.KindNote) {
			if msg.Author == "poet" && msg.Failed {
				return true
			}
		}
		return false
	}, 5e9, 1e7)
	// The note is said before the run's slot is released, so wait for that too.
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)
}

func TestWhatANamedAgentSaidReachesTheMainAgentsNextTurn(t *testing.T) {
	s := newTestServer(t)

	// Nothing running, so it waits for the next turn.
	s.relay("staff-reviewer", "the cache never evicts")
	prompt, carried := s.turnPrompt(nil, "fix it then", nil)
	assert.Contains(t, prompt, "@staff-reviewer said")
	assert.Contains(t, prompt, "the cache never evicts")

	// Building a prompt is not sending one: until the turn goes, it stands.
	again, _ := s.turnPrompt(nil, "and?", nil)
	assert.Contains(t, again, "the cache never evicts")

	// Once it has gone, the turn after does not hear it again.
	s.dropHeard(carried)
	after, _ := s.turnPrompt(nil, "and?", nil)
	assert.NotContains(t, after, "staff-reviewer")
}

func TestASendThatFindsNothingRunningKeepsWhatANamedAgentSaid(t *testing.T) {
	s := newTestServer(t)
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")

	s.relay("staff-reviewer", "the cache never evicts")
	rec := do(t, s, "POST", "/api/chat", `{"message":"put that in the doc"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	// The handler builds a prompt to inject, finds nothing running to take
	// it, and drops it. What it quoted must not go with it: that is a named
	// agent's findings, on screen for the user and nowhere for the agent.
	prompt, _ := s.turnPrompt(nil, "and?", nil)
	assert.Contains(t, prompt, "the cache never evicts")
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

	// The design document is not in any diff, so its findings travel as text
	// — into the conversation, and into what the main agent is told.
	assert.Contains(t, f.asideText(), "the cache has no eviction")
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Contains(t, notes[len(notes)-1].Body, "outside the diff")
	assert.Contains(t, notes[len(notes)-1].Body, "the cache has no eviction")
}

func TestSwitchingModelKeepsTheSession(t *testing.T) {
	s := newTestServer(t)
	s.agent.mu.Lock()
	s.agent.session, s.agent.current = "abc", "abc"
	s.agent.mu.Unlock()

	rec := do(t, s, "POST", "/api/chat/model", `{"model":"sonnet"}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, "sonnet", s.agent.State().Model)
	assert.Equal(t, "abc", s.agent.Current())
	// Said in the conversation, so the change of voice is explained.
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Equal(t, "switched to sonnet", notes[len(notes)-1].Body)

	// The same model again is nothing to do, and nothing to say.
	before := len(s.conversation())
	rec = do(t, s, "POST", "/api/chat/model", `{"model":"sonnet"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Len(t, s.conversation(), before)
}

func TestSwitchingModelMidRunWaitsForTheRun(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)

	rec := do(t, s, "POST", "/api/chat/model", `{"model":"opus"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Equal(t, 1, s.stacked())
	assert.Empty(t, s.agent.State().Model)
	waiting := s.agentState().Waiting
	require.Len(t, waiting, 1)
	assert.Equal(t, "Switch model", waiting[0].Kind)
	assert.Equal(t, "switch to opus", waiting[0].What)

	// The run ending drains the queue, which does the switch and moves on.
	s.agent.finish(nil)
	assert.Eventually(t, func() bool { return s.agent.State().Model == "opus" }, 2e9, 1e7)
	assert.Equal(t, 0, s.stacked())
	assert.False(t, s.agent.State().Running)
}

func TestTheMainAgentsReplyCarriesItsModel(t *testing.T) {
	s := newTestServer(t)
	s.agent.switchModel("haiku")
	reply := s.say(ChatMessage{Kind: chat.KindAgent, Body: "hi"})
	assert.Equal(t, "haiku", reply.Model)
	// A named agent's model is its own business.
	named := s.say(ChatMessage{Kind: chat.KindAgent, Author: "poet", Body: "verse"})
	assert.Empty(t, named.Model)
}

func TestANamedAgentRemembersItsOwnThread(t *testing.T) {
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "hello main agent"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "hello back"})
	s.say(ChatMessage{Kind: chat.KindYou, Body: "@security review this"})
	s.say(ChatMessage{Kind: chat.KindTool, Author: "security", Tool: "Read", Run: runSubagent})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Body: "1. symlink escape", Run: runSubagent})
	s.say(ChatMessage{Kind: chat.KindYou, Body: "@poet a haiku"})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "poet", Body: "verse", Run: runSubagent})

	thread := s.thread("security")
	require.Len(t, thread, 2)
	assert.Equal(t, "you", thread[0].Who)
	assert.Equal(t, "@security review this", thread[0].Text)
	assert.Equal(t, "security", thread[1].Who)
	assert.Equal(t, "1. symlink escape", thread[1].Text)

	// Only its own: the main conversation and the poet are not in it.
	assert.Empty(t, s.thread("nobody"))
	poet := s.thread("poet")
	require.Len(t, poet, 2)
	assert.Equal(t, "verse", poet[1].Text)
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
	// The ask is in the conversation, and the main agent was not claimed.
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	assert.Equal(t, "@staff-reviewer review the diff (1 files)", yours[0].Body)
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
	assert.Empty(t, messagesOfKind(s.conversation(), chat.KindYou))

	rec = do(t, s, "POST", "/api/review", `{"agents":["staff-reviewer","security"," staff-reviewer "]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"agents":["staff-reviewer","security"]`, "repeats collapse")
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1, "one ask in the conversation, not one per reviewer")
	assert.Equal(t, "@staff-reviewer @security review the diff (1 files)", yours[0].Body)
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

	rec := do(t, s, "POST", "/api/chat", `{"message":"@poet a haiku"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"running":false`, "it did not run, and says so")
	assert.Len(t, s.sideRuns(), maxSides, "nothing more was started")
	assert.Equal(t, 0, s.stacked(), "and it did not go to the main agent either")

	rec = do(t, s, "POST", "/api/review", `{"agent":"poet"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	// The numbers, not just "no": which of the four it was is the whole point.
	assert.Contains(t, notes[len(notes)-1].Body, "4 agent(s) running")
	// Three for agents, of four: the fourth is the rewrite's.
	assert.Contains(t, notes[len(notes)-1].Body, "3 at most for agents, of 4")
	assert.True(t, notes[len(notes)-1].Failed)
}

func TestAReplyThatMentionsAnAgentHandsOffToIt(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineAgent(t, s, "critic", "---\nname: critic\n---\nharsh\n")
	defineAgent(t, s, "poet", "---\nname: poet\n---\nverse\n")
	critic, _ := agents.Find(s.agentsList(), "critic")

	// The critic's reply asks the poet: the poet runs, for the critic.
	s.handoff(critic, []string{"poet", "critic", "nobody"}, "a haiku about caches", 0, "")
	runs := s.sideRuns()
	require.Len(t, runs, 1, "itself and the unknown are skipped")
	assert.Equal(t, "poet", runs[0].Name)
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Equal(t, "poet", notes[len(notes)-1].Author)
	assert.Equal(t, "critic", notes[len(notes)-1].For, "the failure is marked for the asker too")

	// Too deep is a note, not a run.
	before := len(s.conversation())
	s.handoff(critic, []string{"poet"}, "again", maxHops, "")
	assert.Empty(t, s.sideRuns())
	assert.Len(t, s.conversation(), before+1)
	assert.Contains(t, s.conversation()[before].Body, "hand-offs deep")
}

func TestAnAgentsThreadIncludesWhatOtherAgentsAskedAndAnswered(t *testing.T) {
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "@critic what do you think?"})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "critic", Body: "@poet give me a haiku first", Run: runSubagent})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "poet", For: "critic", Body: "cache warm, then cold", Run: runSubagent})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "poet", Body: "unrelated verse", Run: runSubagent})

	critic := s.thread("critic")
	require.Len(t, critic, 3)
	assert.Equal(t, "you", critic[0].Who)
	assert.Equal(t, "critic", critic[1].Who)
	assert.Equal(t, "poet", critic[2].Who)
	assert.Equal(t, "cache warm, then cold", critic[2].Text)

	// The poet's thread has the critic's ask and its own answers; not the
	// user's message to the critic.
	poet := s.thread("poet")
	require.Len(t, poet, 3)
	assert.Equal(t, "critic", poet[0].Who)
	assert.Equal(t, "@poet give me a haiku first", poet[0].Text)
}

func TestTheQueueSurvivesARestart(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	for _, m := range []string{"first", "second"} {
		rec := do(t, s, "POST", "/api/chat", `{"message":"`+m+`"}`)
		require.Equal(t, http.StatusAccepted, rec.Code)
	}
	rec := do(t, s, "POST", "/api/chat/model", `{"model":"opus"}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Equal(t, 3, s.stacked())

	// A new server over the same store reads it back, ids and all, so the
	// messages still on screen are the ones that will run.
	s.mu.Lock()
	saved := s.queue
	s.queue, s.nextQueued = nil, 0
	s.mu.Unlock()
	s.loadQueue()
	require.Equal(t, 3, s.stacked())
	assert.Equal(t, saved[0].id, s.queue[0].id)
	assert.Equal(t, "first", s.queue[0].message)
	assert.Equal(t, saved[0].chatID, s.queue[0].chatID)
	assert.Equal(t, kindSwitchModel, s.queue[2].kind)
	assert.Equal(t, "opus", s.queue[2].model)
	assert.Equal(t, 3, s.nextQueued, "ids carry on from the highest saved")

	// Taking one off is saved too.
	_, ok = s.unstack()
	require.True(t, ok)
	s.mu.Lock()
	s.queue = nil
	s.mu.Unlock()
	s.loadQueue()
	assert.Equal(t, 2, s.stacked())
	s.agent.finish(nil)
}

func TestTheRunnerReportsHowFullTheWindowIs(t *testing.T) {
	s := newTestServer(t)
	assert.Equal(t, 200_000, s.agent.State().ContextWindow)
	assert.Zero(t, s.agent.State().ContextTokens)

	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{Input: 100, CacheRead: 60_000, CacheCreation: 2_000, Output: 50}})
	assert.Equal(t, 62_100, s.agent.State().ContextTokens)
	// A result repeats the turn; not counted twice, and never from a tool.
	s.agent.noteUsage(bridge.Event{Type: "result", Usage: &bridge.Usage{Input: 1}})
	s.agent.noteUsage(bridge.Event{Type: "user"})
	assert.Equal(t, 62_100, s.agent.State().ContextTokens)

	// Halving from a real size is a compaction, and says so.
	notes := len(messagesOfKind(s.conversation(), chat.KindNote))
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 150_000}})
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 40_000}})
	all := messagesOfKind(s.conversation(), chat.KindNote)
	require.Len(t, all, notes+1)
	assert.Equal(t, "compacted the conversation: 150k → 40k tokens", all[len(all)-1].Body)
	// Growth, and the small swings of a session's first messages, are not.
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 90_000}})
	s.agent.reset("")
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 15_000}})
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 3_000}})
	assert.Len(t, messagesOfKind(s.conversation(), chat.KindNote), notes+1)

	// Usage past the window says the window was the big one all along.
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 619_000}})
	assert.Equal(t, 1_000_000, s.agent.State().ContextWindow)
	s.agent.noteUsage(bridge.Event{Type: "assistant", Usage: &bridge.Usage{CacheRead: 3_000}})

	s.agent.switchModel("opus[1m]")
	assert.Equal(t, 1_000_000, s.agent.State().ContextWindow)
	assert.Equal(t, 3_000, s.agent.State().ContextTokens, "the same session, so the same window in use")
	s.agent.reset("")
	assert.Zero(t, s.agent.State().ContextTokens, "a new session starts empty")
}

func TestTheGraphWillNotStopTheAdeYouAreLookingAt(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/graph/stop", `{"dir":"/nowhere"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "only a checkout the scan offered")
	assert.Contains(t, rec.Body.String(), "not one of the checkouts")
}

func TestThePlanLimitsRideOnTheStateOnceRead(t *testing.T) {
	// A Codex reading is kept in ~/.ade so the launcher can see it, so this
	// needs a home of its own or it reads — and writes — the real one.
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	asked := 0
	s.limits.Ask = func(context.Context) (limits.Limits, error) {
		asked++
		return limits.Limits{Provider: "claude", CheckedAt: time.Now(),
			Windows: []limits.Window{{Name: "5h", Percent: 39}, {Name: "7d", Percent: 23}}}, nil
	}
	// Nothing known yet; the first look starts a reading in the background.
	assert.Nil(t, s.currentLimits())
	assert.Eventually(t, func() bool { return s.currentLimits() != nil }, 5e9, 1e7)
	got := s.currentLimits()
	require.NotNil(t, got.Claude)
	assert.Equal(t, 39.0, got.Claude.Windows[0].Percent)
	// Fresh for a while: the next looks do not ask again.
	s.currentLimits()
	s.currentLimits()
	assert.Equal(t, 1, asked)

	// What Codex says on a response is kept beside it.
	s.limits.NoteCodex(limits.Limits{Provider: "codex", Windows: []limits.Window{{Name: "5h", Percent: 12}}})
	assert.Equal(t, 12.0, s.currentLimits().Codex.Windows[0].Percent)

	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"limits":{"claude":{"provider":"claude"`)
}

func TestASessionsTitleFollowsItOntoTheRunner(t *testing.T) {
	// HOME before the server: cleanups run last-first, so this way the
	// server is closed before the directory its goroutines write under goes.
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	require.NoError(t, settings.SetTitle("sess-1", "Rate limit the webhook"))

	s.agent.resume("sess-1")
	assert.Equal(t, "Rate limit the webhook", s.agent.State().Title)
	s.agent.reset("")
	assert.Empty(t, s.agent.State().Title)
	// A fresh agent naming itself as a known session picks the title up.
	s.agent.note("sess-1")
	assert.Equal(t, "Rate limit the webhook", s.agent.State().Title)

	assert.Equal(t, "Rate limit the webhook", cleanTitle("\"Rate limit the webhook.\"\nmore"))
	assert.Empty(t, cleanTitle("  "))
}

func TestStopEndsNamedAgentsToo(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.sides = map[string]sideRun{"1": {name: "poet", cancel: cancel}}
	s.mu.Unlock()
	require.Len(t, s.agentState().SideRuns, 1)

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

	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.Len(t, notes, 1)
	assert.Equal(t, "commenter", notes[0].Author)
	assert.Contains(t, notes[0].Body, "filed 1 comment(s) on the diff as @security")
	assert.Contains(t, notes[0].Body, "a.go:3 — medium security")

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
	before := len(s.conversation())
	s.newFiler(reviewingAgent("poet"), "", "").done()
	assert.Len(t, s.conversation(), before)
}

func TestFindingBlocksAreNotShownAsProse(t *testing.T) {
	s := newTestServer(t)
	s.onSubagentEvent("commenter", "security", bridge.Event{Text: "Filed.\n\n```ade-finding\n{\"file\": \"a.go\", \"line\": 3, \"summary\": \"x\"}\n```"})
	s.onSubagentEvent("commenter", "security", bridge.Event{Text: "```ade-finding\n{\"file\": \"a.go\", \"line\": 3, \"summary\": \"x\"}\n```"})
	got := messagesOfKind(s.conversation(), chat.KindAgent)
	require.Len(t, got, 1, "a message that is only blocks is not a message")
	assert.Equal(t, "Filed.", got[0].Body)
	assert.Equal(t, "security", got[0].For)
}

func TestDiffReviewersAreToldAboutTheCommenterWhenThereIsOne(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defineAgent(t, s, "security", "---\nname: security\n---\ntrust nothing\n")
	assert.Equal(t, "", commenterFor(s.agentsList()))

	defineAgent(t, s, "commenter", "---\nname: commenter\n---\nfile it\n")
	assert.Equal(t, "commenter", commenterFor(s.agentsList()))
}

func TestAReviewIsHandedOnWhetherOrNotItAsks(t *testing.T) {
	// A reply that names an agent goes where it says, with the name stripped.
	names, rest, ok := onward("@commenter\n\ncart.go:24 is wrong", "commenter")
	require.True(t, ok)
	assert.Equal(t, []string{"commenter"}, names)
	assert.Equal(t, "cart.go:24 is wrong", rest)

	// A reply that names nobody still reaches the filer: the review is what
	// matters, not whether the reviewer opened with the hand-off.
	names, rest, ok = onward("## Findings\n\ncart.go:24 is wrong", "commenter")
	require.True(t, ok)
	assert.Equal(t, []string{"commenter"}, names)
	assert.Equal(t, "## Findings\n\ncart.go:24 is wrong", rest, "the whole reply, since none of it was a mention")

	// A reply that names someone else is respected; the standing hand-off
	// does not also fire.
	names, _, ok = onward("@tester check this", "commenter")
	require.True(t, ok)
	assert.Equal(t, []string{"tester"}, names)

	// No standing hand-off and no mention is the end of it.
	_, _, ok = onward("## Findings\n\nall fine", "")
	assert.False(t, ok)
}

func TestAnInterruptedSideIsSaidSoInTheConversation(t *testing.T) {
	s := newTestServer(t)

	// Two agents mid-run when the process is about to go away.
	s.sides = map[string]sideRun{
		"1": {name: "security", started: time.Now()},
		"2": {name: "second-opinion", started: time.Now()},
	}
	s.noteInterruptedSides()

	var notes []ChatMessage
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.Kind == chat.KindNote {
			notes = append(notes, msg)
		}
	}
	require.Len(t, notes, 2, "one per agent still running")

	// Named, and marked failed: the conversation held a question that was
	// never going to be answered, and said nothing about it.
	assert.Equal(t, "second-opinion", notes[0].Author, "in a stable order")
	assert.Equal(t, "security", notes[1].Author)
	for _, note := range notes {
		assert.True(t, note.Failed)
		assert.Contains(t, note.Body, "restarted")
	}

	// Nothing running is nothing said.
	s.sides = nil
	s.noteInterruptedSides()
	assert.Len(t, s.chatStore().List(s.agent.Current()), 2)
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

// The main agent's own session holds none of a subagent's work, so this is the
// only route back to it.
func TestSaidIsWhatANamedAgentPutInTheConversation(t *testing.T) {
	s := newTestServer(t)
	s.delegateToken = "secret"

	s.say(ChatMessage{Kind: chat.KindYou, Body: "@security have a look"})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Run: runSubagent, Body: "Six findings, filed."})
	s.say(ChatMessage{Kind: chat.KindTool, Author: "security", Run: runSubagent, Tool: "Grep", Body: ""})
	s.say(ChatMessage{Kind: chat.KindNote, Author: "security", Run: runSubagent, Body: "@security filed 6 comment(s)"})
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "architect", Run: runSubagent, Body: "a different agent"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "the main agent's own reply"})

	got := s.saidBy("security")
	require.Len(t, got, 2, "its replies and its notes, not its tool calls")
	assert.Equal(t, "Six findings, filed.", got[0].Text)
	assert.Equal(t, chat.KindNote, got[1].Kind)
	assert.Equal(t, "security", got[1].Who)
}

func TestSaidNeedsTheCapabilityToken(t *testing.T) {
	s := newTestServer(t)
	s.delegateToken = "secret"
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Run: runSubagent, Body: "findings"})

	// The page's own token is not this one: the conversation is the browser's
	// to read over its own route, and this is the agent's capability.
	rec := do(t, s, http.MethodGet, "/api/internal/delegate/said?agent=security", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/internal/delegate/said?agent=security", nil)
	req.Header.Set("X-Ade-Delegate-Token", "secret")
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var answer said.Answer
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	require.Len(t, answer.Said, 1)
	assert.Equal(t, "findings", answer.Said[0].Text)
}

func TestSaidTellsAnUnknownAgentApart(t *testing.T) {
	s := newTestServer(t)
	s.delegateToken = "secret"

	ask := func(agent string) said.Answer {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/internal/delegate/said?agent="+agent, nil)
		req.Header.Set("X-Ade-Delegate-Token", "secret")
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var answer said.Answer
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
		return answer
	}

	assert.False(t, ask("invented-agent").Known)
	assert.Empty(t, ask("invented-agent").Said)

	req := httptest.NewRequest(http.MethodGet, "/api/internal/delegate/said", nil)
	req.Header.Set("X-Ade-Delegate-Token", "secret")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
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

// "Update doc" folds a reply into the design document. A reviewer of the diff
// files comments instead, so there is nothing of its reply to fold.
func TestAReviewersReplyIsMarkedAsOneAndAConversationsIsNot(t *testing.T) {
	s := newTestServer(t)

	s.onSubagentEventModel("security", "", "", "batch-1", bridge.Event{Text: "a symlink escape"})
	s.onSubagentEventModel("architect", "", "", "", bridge.Event{Text: "cache it at the edge"})

	msgs := s.conversation()
	byAuthor := map[string]chat.Message{}
	for _, m := range msgs {
		if m.Kind == chat.KindAgent {
			byAuthor[m.Author] = m
		}
	}
	require.Contains(t, byAuthor, "security")
	require.Contains(t, byAuthor, "architect")
	assert.True(t, byAuthor["security"].Review, "a reviewer of the diff ran in a batch")
	assert.False(t, byAuthor["architect"].Review, "asked in the conversation, not at a diff")
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

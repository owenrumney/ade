package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/worktrees"
)

// conversation is what the page would show right now.
func (s *Server) conversation() []ChatMessage {
	return s.chatStore().List(s.agent.Current())
}

// messagesOfKind filters the conversation to one kind of message.
func messagesOfKind(messages []ChatMessage, kind string) []ChatMessage {
	var out []ChatMessage
	for _, msg := range messages {
		if msg.Kind == kind {
			out = append(out, msg)
		}
	}
	return out
}

func getChat(t *testing.T, s *Server) chatResponse {
	t.Helper()
	rec := do(t, s, "GET", "/api/chat", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body chatResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestChatNeedsAMessageButNotADocument(t *testing.T) {
	s := newTestServer(t)
	require.Empty(t, s.currentDoc())

	rec := do(t, s, "POST", "/api/chat", `{"message":"   "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "say something")
	// The conversation is the session. A design document is a tool you reach
	// for, not a precondition.
	assert.NotContains(t, rec.Body.String(), "design document")
}

func TestChatTurnsStackBehindARunAndShowAtOnce(t *testing.T) {
	s := newTestServer(t)

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// A cold review has nowhere to take a message mid-run, so the turn waits
	// its turn rather than being refused: you have the next thing to say while
	// it is still working on the last.
	for _, message := range []string{"first", "second", "third"} {
		rec := do(t, s, "POST", "/api/chat", `{"message":"`+message+`"}`)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"queued":`)
	}
	assert.Equal(t, 3, s.stacked())

	// On screen straight away, in the order sent, and marked as waiting so the
	// page can say so beside each one.
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 3)
	assert.Equal(t, "first", yours[0].Body)
	waiting := s.agentState().Waiting
	require.Len(t, waiting, 3)
	assert.Equal(t, yours[0].ID, waiting[0].ChatID)

	for _, want := range []string{"first", "second", "third"} {
		next, ok := s.unstack()
		require.True(t, ok)
		assert.Equal(t, want, next.message)
		assert.Equal(t, kindChat, next.kind)
	}
}

func TestChatCarriesWhatWasHighlighted(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/chat",
		`{"message":"why?","context":{"from":"a.go","text":"  var x = 2  "}}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	// Quoting text is attaching it to the message, not a side conversation
	// scoped to the file it came from.
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	require.NotNil(t, yours[0].Context)
	assert.Equal(t, "a.go", yours[0].Context.From)
	assert.Equal(t, "var x = 2", yours[0].Context.Text)

	next, ok := s.unstack()
	require.True(t, ok)
	require.NotNil(t, next.context)
	queued, _ := s.turnPrompt(next.context, next.message, nil)
	assert.Contains(t, queued, "<selection>\nvar x = 2\n</selection>")

	// An empty highlight is no attachment.
	rec = do(t, s, "POST", "/api/chat", `{"message":"and?","context":{"from":"a.go","text":"  "}}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Nil(t, messagesOfKind(s.conversation(), chat.KindYou)[1].Context)
}

func TestTheTurnPromptNamesTheOpenDocument(t *testing.T) {
	s := newTestServer(t)

	// Nothing open: the agent is told where one would go, because otherwise it
	// has never heard of the Design tab and "start the design doc" landed in
	// whatever scratch file looked plausible.
	first, _ := s.turnPrompt(nil, "hello", nil)
	assert.Contains(t, first, "no design document open")
	assert.Contains(t, first, "docs/feature.md", "named for the branch, so docs.Find will match it")

	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")
	open, _ := s.turnPrompt(nil, "hello", nil)
	assert.Contains(t, open, ".git/ade/docs/plan.md")
	assert.NotContains(t, open, "no design document open", "one instruction about the document, not two")
}

func TestASlashCommandIsNotWrapped(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")

	// The turn preamble names the document and sets the rules; a command has
	// to reach Claude Code first, as typed, or it is not a command.
	compact, _ := s.turnPrompt(nil, "/compact", nil)
	assert.Equal(t, "/compact", compact)
	clear, _ := s.turnPrompt(nil, "/clear", nil)
	assert.True(t, strings.HasPrefix(clear, "/clear"))
	wrapped, _ := s.turnPrompt(nil, "compact this", nil)
	assert.Contains(t, wrapped, "They said:")
}

func TestASnapshotCanStartASessionInANewWorktree(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.ws.snapshot = true
	s.mu.Unlock()

	// In place is refused, and says what to do instead.
	rec := do(t, s, "POST", "/api/chat/new", `{"message":"fix the thing"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "new worktree")

	// Beside it is the way out: the checkout behind the snapshot is real.
	opened := ""
	s.openIn = func(_ context.Context, dir, _ string) (string, error) {
		opened = dir
		return "http://127.0.0.1:1/", nil
	}
	s.ask = func(_ context.Context, _, _, path, _ string) error {
		assert.Equal(t, "/api/chat/new", path)
		return nil
	}
	rec = do(t, s, "POST", "/api/chat/new", `{"message":"fix the thing","worktree":true,"branch":"fix-thing"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, opened, "fix-thing")
}

func TestASessionCanStartInAScratch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	opened := ""
	s.openIn = func(_ context.Context, dir, _ string) (string, error) {
		opened = dir
		return "http://127.0.0.1:1/", nil
	}
	s.ask = func(_ context.Context, _, _, path, _ string) error {
		assert.Equal(t, "/api/chat/new", path)
		return nil
	}

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"try a thing","scratch":true,"branch":"try a thing"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Under ~/.ade, not beside this checkout, and on a branch above an empty
	// base commit.
	root, err := settings.ScratchDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "try-a-thing"), opened)
	assert.DirExists(t, filepath.Join(opened, ".git"))
	assert.NotContains(t, opened, s.space().homeRoot)
}

func TestChatIsServedApartFromTheState(t *testing.T) {
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "hello"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "hi", HTML: "<p>hi</p>"})

	// A session can run to thousands of entries, and the state is fetched on
	// every poll, so the conversation has its own endpoint.
	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), `"messages"`)
	// But it says how long the conversation is, so a page that missed a
	// message can tell.
	var st struct {
		Chat ChatInfo `json:"chat"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
	assert.Equal(t, ChatInfo{Session: s.agent.Current(), Count: 2}, st.Chat)

	body := getChat(t, s)
	require.Len(t, body.Messages, 2)
	assert.Equal(t, "hello", body.Messages[0].Body)
	assert.Equal(t, "<p>hi</p>", body.Messages[1].HTML)
	assert.Equal(t, s.agent.Current(), body.Session)
}

func TestAnEmptyConversationIsAListNotNull(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "GET", "/api/chat", "")
	require.Equal(t, http.StatusOK, rec.Code)
	// A nil slice reaches the browser as null and breaks iterating it.
	assert.Contains(t, rec.Body.String(), `"messages":[]`)
}

func TestWhatTheAgentSaysAndDoesLandsInTheConversation(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "resolve")
	require.True(t, ok)
	defer s.agent.finish(nil)

	s.onAgentEvent(bridge.Event{Type: "assistant", Text: "Looking at *a.go*", ToolUses: []bridge.ToolUse{
		{ID: "toolu_1", Name: "Read", Input: []byte(`{"file_path":"a.go"}`)},
	}})
	s.onAgentEvent(bridge.Event{Type: "user", ToolResults: []bridge.ToolResult{
		{ID: "toolu_1", Content: "package main"},
	}})
	s.onAgentEvent(bridge.Event{Type: "assistant", ToolUses: []bridge.ToolUse{
		{ID: "toolu_2", Name: "Bash", Input: []byte(`{"command":"go test ./..."}`)},
	}})
	s.onAgentEvent(bridge.Event{Type: "user", ToolResults: []bridge.ToolResult{
		{ID: "toolu_2", Content: "FAIL", IsError: true},
	}})

	// Every word and every tool call, whole, not the first 400 characters of
	// the prose and a one-line summary of the rest.
	messages := s.conversation()
	require.Len(t, messages, 3)

	assert.Equal(t, chat.KindTool, messages[0].Kind)
	assert.Equal(t, "Read", messages[0].Tool)
	assert.Equal(t, "a.go", messages[0].Target)
	assert.JSONEq(t, `{"file_path":"a.go"}`, messages[0].Input)
	assert.Equal(t, "package main", messages[0].Output, "the result lands on the call that made it")
	assert.False(t, messages[0].Failed)

	assert.Equal(t, chat.KindAgent, messages[1].Kind)
	assert.Equal(t, "Looking at *a.go*", messages[1].Body)
	assert.Contains(t, messages[1].HTML, "<em>a.go</em>")

	assert.Equal(t, "Bash", messages[2].Tool)
	assert.Equal(t, "FAIL", messages[2].Output)
	assert.True(t, messages[2].Failed)

	// Each carries the run it belongs to, so a review's chatter can be told
	// apart from the conversation proper.
	for _, msg := range messages {
		assert.Equal(t, "resolve", msg.Run)
	}

	// A result for a call nobody recorded is nothing to worry about.
	assert.NotPanics(t, func() {
		s.onAgentEvent(bridge.Event{Type: "user", ToolResults: []bridge.ToolResult{{ID: "toolu_9"}}})
	})
}

// A reply is markdown, and the page renders whatever goldmark makes of it. A
// diagram, a screenshot and a table all have to survive the trip.
func TestAnAgentReplyCarriesItsMarkdownToThePage(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "resolve")
	require.True(t, ok)
	defer s.agent.finish(nil)

	s.onAgentEvent(bridge.Event{Type: "assistant", Text: strings.Join([]string{
		"## What changed",
		"",
		"```mermaid",
		"graph TD; launcher-->ade;",
		"```",
		"",
		"![the launcher](/design-assets/shot.png)",
		"",
		"| tab | shows |",
		"| --- | --- |",
		"| Home | what is on you |",
	}, "\n")})

	messages := s.conversation()
	require.Len(t, messages, 1)
	html := messages[0].HTML

	// pre.mermaid is the hook MarkdownBody watches for to pull the bundle in.
	assert.Contains(t, html, `<pre class="mermaid">`)
	assert.Contains(t, html, "graph TD; launcher--&gt;ade;")
	assert.Contains(t, html, `<img src="/design-assets/shot.png" alt="the launcher">`)
	assert.Contains(t, html, "<table>")
	assert.Contains(t, html, "<h2 id=\"what-changed\">What changed</h2>")
}

func TestAPictureInAReplyIsRoutedAtTheDocsDirectory(t *testing.T) {
	s := newTestServer(t)

	// Chat has nowhere of its own to serve a file from, so a plain name in a
	// reply means the docs directory, token and all.
	html := s.replyHTML("![the badge](resolver-verdict.png)")
	assert.Contains(t, html, `src="design-images/resolver-verdict.png?t=`+s.token+`"`)

	// An address that is already absolute is left alone.
	assert.Contains(t, s.replyHTML("![x](https://example.com/x.png)"), `src="https://example.com/x.png"`)
}

func TestProgressLinesBecomeNotes(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	s.agent.step("filed 3 finding(s)")
	s.agent.finish(nil)

	// The step detail pane folded into the conversation: what a run did is
	// read between the messages it came between.
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.Len(t, notes, 1)
	assert.Equal(t, "filed 3 finding(s)", notes[0].Body)
	assert.Equal(t, "review", notes[0].Run)
	assert.False(t, notes[0].Failed)
}

func TestAFreshAgentsConversationFollowsItsSessionId(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	require.Empty(t, s.agent.Current(), "the test checkout has no remembered session")

	s.say(ChatMessage{Kind: chat.KindYou, Body: "start here"})
	require.Equal(t, 1, s.chatStore().Len(chat.Pending))

	// The first event names the session, and what was said before it moves
	// under that name, so the next restart finds it.
	s.onAgentEvent(bridge.Event{Type: "system", Subtype: "init", SessionID: "sess-new"})
	s.agent.note("sess-new")

	assert.Equal(t, "sess-new", s.agent.Current())
	assert.Zero(t, s.chatStore().Len(chat.Pending))
	got := getChat(t, s)
	assert.Equal(t, "sess-new", got.Session)
	require.Len(t, got.Messages, 1)
	assert.Equal(t, "start here", got.Messages[0].Body)
}

func TestTheConversationSurvivesARestart(t *testing.T) {
	s := newTestServer(t)
	s.agent.resume("sess-abc")
	s.say(ChatMessage{Kind: chat.KindYou, Body: "add LSP support"})
	s.say(ChatMessage{Kind: chat.KindTool, Tool: "Read", Target: "a.go"})

	// A second server on the same checkout is what a restart is. The store is
	// locked to one process, so the first has to let go before the second can
	// open it.
	s.Close()

	store, err := comments.Open(s.startGitDir, "feature")
	require.NoError(t, err)
	again, err := New(Config{
		Root:         s.startRoot,
		Branch:       "feature",
		GitDir:       s.startGitDir,
		CommonGitDir: s.commonGitDir,
		Diff:         gitdiff.Options{Repo: s.startRoot},
	}, store)
	require.NoError(t, err)
	defer again.Close()

	got := again.chatStore().List("sess-abc")
	require.Len(t, got, 2)
	assert.Equal(t, "add LSP support", got[0].Body)
	assert.Equal(t, "Read", got[1].Tool)
	assert.Equal(t, filepath.Join(s.startGitDir, "ade", "chat.db"), again.chatStore().Path())
}

func TestANewSessionStartsFromAPromptAndTakesAModel(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.say(ChatMessage{Kind: chat.KindYou, Body: "left over from nowhere"})
	require.Equal(t, 1, s.chatStore().Len(chat.Pending))

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"  ","model":"opus"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, "POST", "/api/chat/new", `{"message":"build me a thing","model":"opus"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	s.agent.Cancel()

	// A session starts from a prompt, not a document, on the model you chose,
	// and what a fresh agent left before it ever named itself does not come
	// along.
	state := s.agent.State()
	assert.Equal(t, "opus", state.Model)
	assert.Empty(t, state.Session)
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	assert.Equal(t, "build me a thing", yours[0].Body)
}

func TestANewSessionCanUseCodex(t *testing.T) {
	s := newTestServer(t)
	s.agent.codexBin = t.TempDir() + "/no-such-codex"

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"build it","provider":"codex","model":"gpt-5.6-terra","sandbox":false}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	state := s.agent.State()
	assert.Equal(t, "codex", state.Provider)
	assert.Equal(t, "gpt-5.6-terra", state.Model)
	s.agent.Cancel()
}

func TestANewSessionTakesAnAttachment(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	// A screenshot or a spec is often what the session is about, so it goes
	// with the prompt rather than being dropped on the way in.
	rec := do(t, s, "POST", "/api/chat/new",
		`{"message":"what is broken here","files":[{"name":"shot.png","mediaType":"image/png","data":"aGk="}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	s.agent.Cancel()

	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	require.Len(t, yours[0].Files, 1)
	assert.Equal(t, "shot.png", yours[0].Files[0].Name)
	assert.FileExists(t, yours[0].Files[0].Path)

	// And an attachment over the caps is refused before the session is thrown
	// away for it.
	before := s.agent.Current()
	huge := strings.Repeat("A", chat.MaxFileBytes*4/3+4096)
	rec = do(t, s, "POST", "/api/chat/new",
		`{"message":"x","files":[{"name":"big.bin","data":"`+huge+`"}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, before, s.agent.Current(), "a refused prompt must not start a session")
}

func TestANewSessionIsRefusedMidRun(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// A new session is a decision, not the next thing to say, so it is refused
	// rather than queued behind whatever is running.
	rec := do(t, s, "POST", "/api/chat/new", `{"message":"start over"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Zero(t, s.stacked())
}

func TestResumingAnotherSession(t *testing.T) {
	s := newTestServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	transcript(t, home, s.space().homeRoot, "sess-here")

	// Only a transcript that is actually in this checkout: --resume on a
	// missing id fails every run from then on.
	rec := do(t, s, "POST", "/api/chat/resume", `{"id":"sess-elsewhere"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no transcript")

	rec = do(t, s, "POST", "/api/chat/resume", `{"id":""}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, "POST", "/api/chat/resume", `{"id":"sess-here","provider":"claude","model":"opus"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "sess-here", s.agent.Current())
	assert.Equal(t, "sess-here", s.agent.State().Session)
	assert.Equal(t, "opus", s.agent.State().Model)

	// Already there is not an error, and not a restart of the subprocess.
	rec = do(t, s, "POST", "/api/chat/resume", `{"id":"sess-here"}`)
	assert.Equal(t, http.StatusOK, rec.Code)

	// But not out from under a run.
	transcript(t, home, s.space().homeRoot, "sess-other")
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)
	rec = do(t, s, "POST", "/api/chat/resume", `{"id":"sess-other"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestAMessageMidRunIsQueuedWhenItCannotBeInjected(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "commit-message")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// A cold run has no session to inject into, so the message waits whatever
	// was asked, and the page is told it is not injectable.
	assert.False(t, s.agent.State().Injectable)
	rec := do(t, s, "POST", "/api/chat", `{"message":"and also","queue":false}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)
	assert.NotContains(t, rec.Body.String(), "injected")
}

func TestArchivingASessionFromTheServer(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	rec := do(t, s, "POST", "/api/sessions/archive", `{"id":"","archived":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, "POST", "/api/sessions/archive", `{"id":"sess-1","archived":true}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = do(t, s, "POST", "/api/sessions/archive", `{"id":"sess-1","archived":false}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestMovingCheckoutTakesTheConversationStoreAlong(t *testing.T) {
	s := newTestServer(t)
	before := s.chatStore()

	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.space().homeRoot, path, "cart"))
	require.NoError(t, s.relocate(context.Background(), path))

	// A worktree has its own git dir, so its conversations are its own.
	after := s.chatStore()
	assert.NotSame(t, before, after)
	assert.Contains(t, after.Path(), s.space().gitDir)
	_, err := before.Append("x", ChatMessage{})
	assert.Error(t, err, "the old store was closed, not left open under a directory nothing looks in")
}

func TestAnAttachmentGoesWithTheMessage(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// A picture on its own is a message: "what is this?" goes without saying.
	rec := do(t, s, "POST", "/api/chat", `{"message":"","files":[{"name":"shot.png","mediaType":"image/png","data":"aGk="}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	require.Len(t, yours[0].Files, 1)
	assert.True(t, yours[0].Files[0].Image)
	assert.Equal(t, chat.AttachDir(s.space().gitDir), filepath.Dir(yours[0].Files[0].Path))
	assert.Empty(t, yours[0].Images, "the bytes are the file, not the transcript")

	// And it reaches the turn when it runs, not just the transcript.
	next, ok := s.unstack()
	require.True(t, ok)
	require.Len(t, next.files, 1)
	assert.Equal(t, yours[0].Files[0].Path, next.files[0].Path)
}

// The queue is written to disk, so what it holds is a path rather than the
// bytes: a queued screenshot used to grow the state file by its own size.
func TestTheQueueHoldsPathsRatherThanBytes(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/chat", `{"message":"look","files":[{"name":"a.png","mediaType":"image/png","data":"aGk="}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	raw := s.chatStore().State(queueState)
	assert.NotContains(t, string(raw), "aGk=", "the bytes must not be in the queue on disk")
	assert.Contains(t, string(raw), "a.png")
}

func TestAttachmentsAreCheckedBeforeAnythingIsSaid(t *testing.T) {
	s := newTestServer(t)

	huge := strings.Repeat("A", chat.MaxFileBytes*4/3+4096)
	var many []string
	for range chat.MaxFiles + 1 {
		many = append(many, `{"name":"a.txt","data":"aGk="}`)
	}

	for name, body := range map[string]string{
		"too many":       `{"message":"x","files":[` + strings.Join(many, ",") + `]}`,
		"one too big":    `{"message":"x","files":[{"name":"big.bin","data":"` + huge + `"}]}`,
		"nothing at all": `{"message":"","files":[{"name":"empty.txt","data":"  "}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, s, "POST", "/api/chat", body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
	assert.Empty(t, s.conversation(), "a refused message must not be shown as sent")
}

// An attachment reaches the agent as a path it can read, whatever it is.
// Claude Code removes document blocks, so a document has no other way in.
func TestTheTurnNamesEachAttachmentAsAPath(t *testing.T) {
	s := newTestServer(t)

	files, err := chat.Save(s.space().gitDir, []chat.Incoming{
		{Name: "spec.pdf", MediaType: "application/pdf", Data: "aGk="},
		{Name: "shot.png", MediaType: "image/png", Data: "aGk="},
	})
	require.NoError(t, err)

	turn, _ := s.turnPrompt(nil, "have a look", files)
	assert.Contains(t, turn, "Attached files, read them with Read:")
	assert.Contains(t, turn, files[0].Path+" (spec.pdf)")
	assert.Contains(t, turn, files[1].Path+" (shot.png)")

	// Only the picture gets a block; the document would be stripped.
	blocks := fileBlocks(files)
	require.Len(t, blocks, 1)
	assert.Equal(t, "image/png", blocks[0].MediaType)
}

func TestANewSessionCanStartInAWorktree(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// The worktree gets its own ade, started the way the launcher starts one
	// and handed the prompt. Both are stood in for: the real ones run this
	// binary, which under go test is the test.
	var opened, asked string
	s.openIn = func(_ context.Context, dir, id string) (string, error) {
		opened = dir
		assert.Empty(t, id, "a new session resumes nothing")
		return "http://127.0.0.1:1/?t=tok", nil
	}
	s.ask = func(_ context.Context, url, method, path, body string) error {
		asked = url + " " + method + " " + path + " " + body
		return nil
	}

	// Under a run, and still 200: this server is left alone.
	rec := do(t, s, "POST", "/api/chat/new", `{"message":"add rate limiting","provider":"codex","model":"gpt-5.6-terra","worktree":true,"branch":"Rate Limiting!","sandbox":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"branch":"rate-limiting"`)
	assert.Equal(t, gitdiff.WorktreePath(s.space().homeRoot, "rate-limiting"), opened)
	assert.DirExists(t, opened)
	assert.Contains(t, asked, "http://127.0.0.1:1/?t=tok POST /api/chat/new")
	assert.Contains(t, asked, `"message":"add rate limiting"`)
	assert.Contains(t, asked, `"provider":"codex"`)
	assert.Contains(t, asked, `"model":"gpt-5.6-terra"`)
	assert.Equal(t, "rate-limiting", gitdiff.Branch(context.Background(), opened))
	// Off main, not off feature: new work does not inherit this checkout's
	// branch, whatever it happens to be on.
	main, err := gitdiff.HeadSHA(context.Background(), opened)
	require.NoError(t, err)
	base, err := exec.Command("git", "-C", s.space().homeRoot, "rev-parse", "main").Output()
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(string(base)), main)

	// This server's own conversation and run are untouched.
	assert.True(t, s.agent.State().Running)
	assert.Empty(t, messagesOfKind(s.conversation(), chat.KindYou))

	// A branch that slugs to nothing is refused before anything is made.
	rec = do(t, s, "POST", "/api/chat/new", `{"message":"x","worktree":true,"branch":"---"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// And the same name twice is git's refusal, not a second checkout.
	rec = do(t, s, "POST", "/api/chat/new", `{"message":"x","worktree":true,"branch":"rate-limiting"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "already exists")
}

func TestWorktreesAreListedAndPrunedByTheServer(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	idle := gitdiff.WorktreePath(root, "idle")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, idle, "idle"))
	busy := gitdiff.WorktreePath(root, "busy")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), root, busy, "busy"))
	require.NoError(t, os.WriteFile(filepath.Join(busy, "new.go"), []byte("package main\n"), 0o600))

	rec := do(t, s, "GET", "/api/worktrees", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed struct {
		Worktrees []worktrees.Worktree `json:"worktrees"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Worktrees, 2)

	// Asked to remove both: the idle one goes, the one with uncommitted work
	// stays and says why. Nothing outside the scan is touched.
	rec = do(t, s, "POST", "/api/worktrees/prune", `{"paths":["`+idle+`","`+busy+`","/etc"]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Removed []string `json:"removed"`
		Kept    []struct {
			Path string `json:"path"`
			Why  string `json:"why"`
		} `json:"kept"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, []string{idle}, out.Removed)
	require.Len(t, out.Kept, 2)
	assert.Contains(t, out.Kept[0].Why, "uncommitted")
	assert.Contains(t, out.Kept[1].Why, "not one of")
	assert.NoDirExists(t, idle)
	assert.DirExists(t, busy)
}

func TestAReviewRequestNotOnTheListIsNotFound(t *testing.T) {
	s := newTestServer(t)
	s.board.SetReviews([]ghpr.Request{{Repo: "acme/other", Number: 9}})
	rec := do(t, s, "POST", "/api/reviews/open", `{"repo":"acme/other","number":10}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestANewSessionCanCarryASummaryOfThisOne(t *testing.T) {
	if !carryOverEnabled {
		t.Skip("the hand-over is off; see carryOverEnabled")
	}
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	s.say(ChatMessage{Kind: chat.KindYou, Body: "add rate limiting"})
	s.say(ChatMessage{Kind: chat.KindTool, Tool: "Edit", Target: "x.go"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "Done: limiter in x.go, tests pass."})
	s.say(ChatMessage{Kind: chat.KindNote, Body: "compacted"})

	var asked string
	old := runOneShot
	runOneShot = func(_ context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
		asked = opts.Prompt
		assert.Empty(t, opts.Tools, "a summary reads nothing")
		return &bridge.Turn{Text: "## Work\nRate limiting in x.go.\n## Left\nDocs."}, nil
	}
	t.Cleanup(func() { runOneShot = old })

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"write the docs","carry":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"carrying":true`)
	require.Eventually(t, func() bool { return !s.agent.State().Running }, 10*time.Second, 10*time.Millisecond)

	// The summary was made from what was said, not the tool rows or notes.
	assert.Contains(t, asked, "add rate limiting")
	assert.Contains(t, asked, "Done: limiter in x.go")
	assert.NotContains(t, asked, "Edit")
	assert.NotContains(t, asked, "compacted")

	// The new conversation opens with your prompt, the summary as its context.
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	assert.Equal(t, "write the docs", yours[0].Body)
	require.NotNil(t, yours[0].Context)
	assert.Contains(t, yours[0].Context.From, "carried over")
	assert.Contains(t, yours[0].Context.Text, "Rate limiting in x.go")
	assert.Empty(t, s.agent.State().Session, "a fresh session, not a resume")
}

func TestAFailedSummaryLeavesTheSessionAlone(t *testing.T) {
	if !carryOverEnabled {
		t.Skip("the hand-over is off; see carryOverEnabled")
	}
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.say(ChatMessage{Kind: chat.KindYou, Body: "hello"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "hi"})
	old := runOneShot
	runOneShot = func(context.Context, bridge.OneShotOpts) (*bridge.Turn, error) {
		return nil, errors.New("claude fell over")
	}
	t.Cleanup(func() { runOneShot = old })

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"carry on","carry":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Eventually(t, func() bool { return !s.agent.State().Running }, 10*time.Second, 10*time.Millisecond)

	msgs := s.conversation()
	yours := messagesOfKind(msgs, chat.KindYou)
	require.Len(t, yours, 1, "the new prompt was not said into the old session")
	assert.Equal(t, "hello", yours[0].Body)
	notes := messagesOfKind(msgs, chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Contains(t, notes[len(notes)-1].Body, "could not carry the conversation over")
	assert.True(t, notes[len(notes)-1].Failed)
}

// While the hand-over is off, asking for one is not an error and not a
// half-measure: the new session simply starts fresh, as it would have without
// the tick, and nothing is spent summarising the old one.
func TestANewSessionIgnoresACarryWhileTheHandOverIsOff(t *testing.T) {
	if carryOverEnabled {
		t.Skip("the hand-over is on")
	}
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	s.say(ChatMessage{Kind: chat.KindYou, Body: "add rate limiting"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "Done: limiter in x.go."})

	asked := false
	old := runOneShot
	runOneShot = func(context.Context, bridge.OneShotOpts) (*bridge.Turn, error) {
		asked = true
		return &bridge.Turn{Text: "a summary nobody asked for"}, nil
	}
	t.Cleanup(func() { runOneShot = old })

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"write the docs","carry":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `"carrying"`)
	require.Eventually(t, func() bool { return !s.agent.State().Running }, 10*time.Second, 10*time.Millisecond)

	assert.False(t, asked, "no summary was written")
	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1, "the old conversation was dropped for a fresh one")
	assert.Equal(t, "write the docs", yours[0].Body)
	assert.Nil(t, yours[0].Context, "nothing was carried in")
}

// noRuntime is a container runtime whose binary is not there, so a
// sandboxed start fails at once rather than running anything.
type noRuntime struct{}

func (noRuntime) Name() string                    { return "none" }
func (noRuntime) Bin() string                     { return "/no/such/runtime" }
func (noRuntime) Args(spec sandbox.Spec) []string { return spec.Argv }

func TestANewSessionCanTurnTheSandboxOnOrOff(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	oldDetect, oldLogin := detectRuntime, sandboxLogin
	detectRuntime = func(context.Context) (sandbox.Runtime, error) { return noRuntime{}, nil }
	sandboxLogin = func(_ context.Context, home string) (string, error) { return filepath.Join(home, "creds.json"), nil }
	t.Cleanup(func() { detectRuntime, sandboxLogin = oldDetect, oldLogin })
	assert.False(t, s.agent.State().Sandbox)

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"hello","sandbox":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	// The failure is written into the conversation after the run is closed
	// out, so the note is what to wait for, not the run.
	require.Eventually(t, func() bool { return len(messagesOfKind(s.conversation(), chat.KindNote)) > 0 }, 10*time.Second, 10*time.Millisecond)
	assert.True(t, s.agent.State().Sandbox, "the page asked for a container")
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	assert.Contains(t, notes[len(notes)-1].Body, "no/such/runtime", "the run went to the runtime, not to claude on the host")

	// Left out, the next session runs as this one did.
	rec = do(t, s, "POST", "/api/chat/new", `{"message":"again"}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Eventually(t, func() bool { return !s.agent.State().Running }, 10*time.Second, 10*time.Millisecond)
	assert.True(t, s.agent.State().Sandbox)

	rec = do(t, s, "POST", "/api/chat/new", `{"message":"host","sandbox":false}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Eventually(t, func() bool { return !s.agent.State().Running }, 10*time.Second, 10*time.Millisecond)
	assert.False(t, s.agent.State().Sandbox)
}

// Auto-compaction is Claude Code's, and with it off a full window ends the
// turn with a message about compacting rather than shrinking the conversation.
// Read loosely: the wording is theirs and has moved before.
func TestAFullWindowIsRecognisedHoweverItIsWorded(t *testing.T) {
	for _, word := range []string{
		"Prompt is too long",
		"Context low · Run /compact to compact & continue",
		"the conversation is too long for the context window",
		"5% context left until auto-compact",
	} {
		assert.True(t, fullWindow(errors.New(word)), word)
	}

	assert.False(t, fullWindow(nil))
	assert.False(t, fullWindow(errors.New("context canceled")), "a cancelled run is not a full one")
	assert.False(t, fullWindow(errors.New("claude not found in PATH")))
}

func TestANewSessionStartsWithNoDesignDocument(t *testing.T) {
	s := newTestServer(t)
	// Named for the branch, so the poll's match would find it: this has to be
	// declined as well as closed, or the next poll opens it straight back up.
	name := designName(s.space().branch) + ".md"
	writeDoc(t, s, name, "# Design\n")
	require.Equal(t, name, s.currentDesign().File, "open to begin with")

	rec := do(t, s, "POST", "/api/chat/new", `{"message":"something else"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Empty(t, s.currentDoc())
	assert.Empty(t, s.currentDesign().File, "and it stays closed")
	assert.Empty(t, s.currentDesign().File, "on the poll after that too")

	// Still on disk, and the agent is told there is no design rather than
	// handed the last session's.
	assert.Len(t, s.currentDesign().Files, 1)
	fresh, _ := s.turnPrompt(nil, "hi", nil)
	assert.Contains(t, fresh, "no design document open")
}

// One server per checkout means New session in a checkout that is already
// served arrives as `--session none` on the running ade. It used to resolve
// to a missing transcript and do nothing at all.
func TestResumingNoneStartsAConversation(t *testing.T) {
	s := newTestServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	transcript(t, home, s.space().homeRoot, "sess-here")

	rec := do(t, s, "POST", "/api/chat/resume", `{"id":"sess-here"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "sess-here", s.agent.Current(), "the transcript was resumed")

	// The model goes with it: New session in the launcher picks one, and the
	// fresh conversation has to start on that rather than on the default.
	rec = do(t, s, "POST", "/api/chat/resume", `{"id":"none","model":"opus"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, s.agent.Current())
	assert.Empty(t, s.agent.State().Session)
	assert.Equal(t, "opus", s.agent.State().Model)

	// Not out from under a run: the turn in flight belongs to the old one.
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)
	rec = do(t, s, "POST", "/api/chat/resume", `{"id":"none"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestARetitleIsDueEverySoOften(t *testing.T) {
	said := func(n int) []chat.Message {
		out := make([]chat.Message, 0, n*2)
		for i := 0; i < n; i++ {
			out = append(out,
				chat.Message{Kind: chat.KindYou, Body: "x"},
				chat.Message{Kind: chat.KindAgent, Body: "y"})
		}
		return out
	}

	assert.False(t, dueARetitle(nil), "nothing said is not a drift")
	assert.False(t, dueARetitle(said(1)))
	assert.True(t, dueARetitle(said(retitleEvery)))
	assert.False(t, dueARetitle(said(retitleEvery+1)))
	assert.True(t, dueARetitle(said(retitleEvery*2)))

	// The agent's own messages are a turn, not a change of subject.
	busy := append(said(1), chat.Message{Kind: chat.KindAgent, Body: "z"})
	assert.False(t, dueARetitle(busy))
}

func TestARetitleReadsTheLatestExchange(t *testing.T) {
	said := []chat.Message{
		{Kind: chat.KindYou, Body: "brew install lazytrivy"},
		{Kind: chat.KindAgent, Body: "already installed"},
		{Kind: chat.KindYou, Body: "why no dashboard data"},
		{Kind: chat.KindAgent, Body: "the worker image predates the reporter"},
	}

	first, reply := lately(said)
	assert.Equal(t, "why no dashboard data", first)
	assert.Equal(t, "the worker image predates the reporter", reply)
}

// A conversation ade named on an earlier run has its title on disk and
// nothing in memory, so Rename opened with an empty box.
func TestTheRunStateCarriesASavedTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	s.agent.mu.Lock()
	s.agent.session, s.agent.current, s.agent.title = "s1", "s1", ""
	s.agent.mu.Unlock()

	require.NoError(t, settings.SetTitle("s1", "the worker dashboard"))
	assert.Equal(t, "the worker dashboard", s.agent.State().Title)

	// What the runner was told beats the file: a rename takes effect at once.
	s.agent.setTitle("renamed just now")
	assert.Equal(t, "renamed just now", s.agent.State().Title)
}

func TestAMessageSentDuringARunIsQueuedWhateverThePageBelieves(t *testing.T) {
	for _, said := range []string{`{"message":"I just reloaded","queue":false}`, `{"message":"I just reloaded"}`} {
		t.Run(said, func(t *testing.T) {
			s := newTestServer(t)
			_, ok := s.agent.begin(context.Background(), kindChat)
			require.True(t, ok)
			t.Cleanup(func() {
				s.agent.Cancel()
				s.agent.finish(nil)
			})

			rec := do(t, s, "POST", "/api/chat", said)
			require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

			var got map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			assert.Nil(t, got["injected"], "a live turn is never steered from the composer")
			assert.Equal(t, float64(1), got["queued"], "it waits its turn instead")

			waiting := s.waiting()
			require.Len(t, waiting, 1)
			assert.Equal(t, "I just reloaded", waiting[0].What)
		})
	}
}

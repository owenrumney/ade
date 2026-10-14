package web

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/permissions"
	"github.com/owenrumney/ade/internal/scratch"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/skills"
	"github.com/owenrumney/ade/internal/statedir"
	"github.com/owenrumney/ade/internal/worktrees"
)

// ChatMessage is one entry of the conversation. The chat package owns the type
// because it owns the store the conversation is written to.
type ChatMessage = chat.Message

// chatResponse is the whole conversation, as /api/chat serves it. Session is
// what it is stored under, so the page can tell a new conversation from more
// of the same one.
type chatResponse struct {
	Session  string        `json:"session"`
	Messages []ChatMessage `json:"messages"`
}

// chatEvent is what goes over SSE when the conversation moves: a message that
// was added or changed, or Reset when the whole thing should be read again
// because the session changed under it.
type chatEvent struct {
	Message *ChatMessage `json:"message,omitempty"`
	Reset   bool         `json:"reset,omitempty"`
}

// handleChatList serves the conversation on its own. It is not part of
// /api/state: a session can run to thousands of entries, and the state is
// fetched on every poll.
func (s *Server) handleChatList(w http.ResponseWriter, _ *http.Request) {
	session := s.agent.Current()
	messages := s.chatStore().List(session)
	if messages == nil {
		messages = []ChatMessage{}
	}
	writeJSON(w, http.StatusOK, chatResponse{Session: session, Messages: messages})
}

// handleChat sends one turn to the agent. It returns 202 like every other run,
// so the reply arrives over SSE rather than blocking. A snapshot is the one
// exception: there the turn becomes a checkout, and the 200 says where it is.
//
// While the agent is on a turn of the shared session the message is injected
// into it, so what you typed reaches it without waiting. Queue asks for it to
// wait behind the run instead; a run that cannot take one — a cold review, say
// — waits regardless.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string          `json:"message"`
		Context *chat.Context   `json:"context"`
		Files   []chat.Incoming `json:"files"`
		Queue   bool            `json:"queue"`
	}
	// Attachments make this the one request that can be bigger than a megabyte.
	if !decodeUpTo(w, r, &req, maxChatBody) {
		return
	}
	message := strings.TrimSpace(req.Message)
	// A picture on its own is a message: "what is this?" goes without saying.
	if message == "" && len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "say something to the agent first")
		return
	}
	// Talking to a snapshot is wanting to work on it: checked out, not refused.
	if s.space().snapshot {
		s.checkOutAndHandOff(w, r, message, req.Context, req.Files)
		return
	}
	if s.agent.TakenOver() {
		writeError(w, http.StatusConflict, "the agent's own interface has this conversation; close the terminal to chat here again")
		return
	}
	files, err := s.attach(req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Attachments that were all empty leave nothing to send.
	if message == "" && len(files) == 0 {
		writeError(w, http.StatusBadRequest, "say something to the agent first")
		return
	}
	attached := attachment(req.Context)
	if ws := s.space(); ws.viewRoot != "" && attached != nil {
		attached.From = qualifyView(ws.viewRoot, attached.From)
	}
	if question, ok := btwQuestion(message); ok {
		s.handleAside(w, question, attached)
		return
	}

	// Shown straight away either way, so what you sent is on screen whether it
	// runs now, joins the run, or waits its turn.
	shown := s.say(ChatMessage{Kind: chat.KindYou, Body: message, Context: attached, Files: files})

	// "@name …" calls a named agent, which runs beside the main one rather
	// than in its queue; "@a @b …" calls both at once. Names nobody is
	// defined as are just a message that starts with an @, and go where the
	// rest do.
	if names, rest, ok := agents.Mentions(message); ok {
		if called := s.callSubagents(names, rest, attached, files); called != nil {
			s.hub.Broadcast("agent", nil)
			s.hub.Broadcast("state", nil)
			writeJSON(w, http.StatusAccepted, map[string]any{"running": len(called) > 0, "agents": called})
			return
		}
	}

	// A page that has not refreshed says idle; steering a live turn loses it.
	runCtx, ok := s.agent.begin(s.ctx, kindChat)
	if !ok {
		waiting := s.stack(queued{
			kind: kindChat, message: message, context: attached, files: files,
			// So dropping it from the queue takes the message with it.
			chatID: shown.ID,
		})
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "queued": waiting})
		return
	}

	s.spawn(func() { s.runChat(runCtx, attached, message, files) })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

// attach writes what came with a message under this checkout's attachments
// directory, which is why the bytes travel in the request.
func (s *Server) attach(files []chat.Incoming) ([]chat.File, error) {
	if len(files) == 0 {
		return nil, nil
	}
	return chat.Save(s.space().gitDir, files)
}

// fileBlocks is the attachments that go as content blocks: small pictures.
func fileBlocks(files []chat.File) []bridge.Image {
	var out []bridge.Image
	for _, f := range files {
		if media, data, ok := chat.Block(f); ok {
			out = append(out, bridge.Image{MediaType: media, Data: data})
		}
	}
	return out
}

// handleChatNew starts a fresh conversation from a prompt, on the model named.
// The one before it stays on disk and in the launcher; this only stops
// resuming it.
func (s *Server) handleChatNew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message  string `json:"message"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		// Files are what was attached to the prompt, the same as on a
		// message: a screenshot or a spec is often what the session is about.
		Files []chat.Incoming `json:"files"`
		// Context is a diff selection the prompt was written against, carried
		// in when a session is handed over from another checkout.
		Context *chat.Context `json:"context"`

		// Worktree starts the session in a new checkout beside this one, on
		// Branch, so it runs alongside whatever is going here rather than in
		// place of it.
		Worktree bool   `json:"worktree"`
		Branch   string `json:"branch"`
		// Scratch starts the session in a throwaway checkout under ~/.ade
		// instead, for work that has no repository to belong to yet.
		Scratch bool `json:"scratch"`
		// Carry brings a summary of this conversation along as the first
		// message's context: the same work, a transcript that starts small.
		Carry bool `json:"carry"`
		// Sandbox, when sent, is whether the new session's agent runs in a
		// container; left out, the session runs as this one does.
		Sandbox *bool `json:"sandbox"`
	}
	// Attachments put this request in the same size bracket as a chat message.
	if !decodeUpTo(w, r, &req, maxChatBody) {
		return
	}
	message := strings.TrimSpace(req.Message)
	// A picture on its own is a message here too, as it is on a turn.
	if message == "" && len(req.Files) == 0 {
		writeError(w, http.StatusBadRequest, "say what the new session is for")
		return
	}
	if err := chat.Check(req.Files); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validProvider(req.Provider) {
		writeError(w, http.StatusBadRequest, "choose claude or codex")
		return
	}
	provider := normalizeProvider(req.Provider)
	// A worktree is the way out of a snapshot, so it is checked first: the
	// checkout behind a fetched diff is real, and a branch can be added
	// beside it even though nothing can be done in place.
	if req.Worktree {
		s.newSessionInWorktree(w, r, message, req.Context, req.Files, provider, strings.TrimSpace(req.Model), req.Branch, req.Sandbox)
		return
	}
	// A scratch is nowhere near this checkout, so a snapshot is no obstacle
	// either.
	if req.Scratch {
		s.newSessionInScratch(w, message, req.Context, req.Files, provider, strings.TrimSpace(req.Model), req.Branch, req.Sandbox)
		return
	}
	if s.space().snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is nothing local to work on; start the session in a new worktree instead")
		return
	}
	// Claimed before the reset, so a run cannot start on the old session in
	// between, and refused rather than queued: a new session is a decision,
	// not the next thing to say.
	runCtx, ok := s.agent.begin(s.ctx, kindChat)
	if !ok {
		writeError(w, http.StatusConflict, "the agent is already running; wait for it or cancel it first")
		return
	}
	model := strings.TrimSpace(req.Model)
	if req.Sandbox != nil {
		s.agent.setSandbox(*req.Sandbox)
	}
	files, err := s.attach(req.Files)
	if err != nil {
		s.agent.finish(nil)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if carryOverEnabled && req.Carry && len(s.conversationProse()) > 0 {
		s.spawn(func() { s.carryOver(runCtx, model, message, files) })
		s.hub.Broadcast("agent", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "carrying": true})
		return
	}
	s.startFresh(provider, model)
	attached := attachment(req.Context)
	s.say(ChatMessage{Kind: chat.KindYou, Body: message, Context: attached, Files: files})
	s.spawn(func() { s.runChat(runCtx, attached, message, files) })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

// startFresh drops the conversation for a new one on the selected provider and model.
// The caller holds the run.
func (s *Server) startFresh(provider, model string) {
	s.agent.resetProvider(provider, model)
	s.freshened()
}

// freshened is what every reset does once the agent is back to nothing.
func (s *Server) freshened() {
	// A new session is new work, so the last one's design document is not its
	// brief. Building a document is the one path that starts a fresh agent on
	// purpose, and it does not come through here.
	s.closeDoc()
	// Whatever a fresh agent left here before it named itself belongs to a
	// conversation that never got going.
	if err := s.chatStore().Clear(chat.Pending); err != nil {
		slog.Debug("clear the pending conversation", "err", err)
	}
	s.hub.Broadcast("chat", chatEvent{Reset: true})
}

// handleChatClear empties the conversation without starting a turn, on the
// agent and model already chosen.
func (s *Server) handleChatClear(w http.ResponseWriter, r *http.Request) {
	if s.agent.State().Running {
		writeError(w, http.StatusConflict, "the agent is already running; wait for it or cancel it first")
		return
	}
	s.agent.reset("")
	s.freshened()
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	s.agent.touch()
	w.WriteHeader(http.StatusNoContent)
}

// clearForBuild empties the window the design was written in, on the agent
// picked. The document stays open, unlike startFresh: it is the build's brief.
//
// The pending conversation is not cleared, though startFresh clears its own.
// A design written without an agent turn is pending, and it is the user's.
func (s *Server) clearForBuild(provider, model string) {
	if provider == "" {
		s.agent.reset(model)
	} else {
		s.agent.resetProvider(provider, model)
	}
	s.hub.Broadcast("chat", chatEvent{Reset: true})
}

// carryOverEnabled is off while the hand-over is under suspicion. The summary
// it writes lands in the new session's first message, so the fresh transcript
// starts with a document in it, and that is room the compaction it was meant to
// avoid never gets back. Off until it can be shown to buy what it costs; the
// checkbox goes with it, in web/src/modals/SmallModals.tsx.
const carryOverEnabled = false

// carryOver is a new session that knows what this one knew: a one-shot on
// the same model writes the hand-over from the conversation — the prose,
// not the tool rows — and it goes into the fresh session as the first
// message's context, where the agent reads it the way it reads a highlight.
// The old transcript stays on disk for the picker; the new one starts
// small, which is the point. If the summary cannot be written the session
// stays as it was, and says why.
func (s *Server) carryOver(ctx context.Context, model, message string, files []chat.File) {
	defer s.recoverRun(kindChat)
	from := s.agent.Current()
	s.agent.step("summarising the conversation to carry it into a new session")

	// Four minutes is for the summary alone; the turn that follows runs on
	// the run's own context like any other.
	summaryCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	turn, err := s.oneShot(summaryCtx, bridge.OneShotOpts{
		Bin:    s.agent.bin,
		Dir:    s.space().homeRoot,
		Model:  s.agent.modelName(),
		Prompt: design.CarryPrompt(s.conversationProse()),
		Tools:  []string{},
		NoMCP:  true,
	})
	summary := ""
	if turn != nil {
		summary = strings.TrimSpace(turn.Text)
	}
	if err == nil && summary == "" {
		err = fmt.Errorf("the summary came back empty")
	}
	if err != nil {
		s.endRun(fmt.Errorf("could not carry the conversation over: %w", err))
		return
	}

	s.startFresh("claude", model)
	label := "carried over from the last session"
	if from != "" {
		label = fmt.Sprintf("carried over from session %s", from[:min(8, len(from))])
	}
	carried := &chat.Context{From: label, Text: summary}
	s.say(ChatMessage{Kind: chat.KindYou, Body: message, Context: carried, Files: files})
	s.hub.Broadcast("state", nil)
	s.runChat(ctx, carried, message, files)
}

// conversationProse is what was said, in order: your messages and the
// agents' replies, without the tool rows and notes — what a summary is
// made of.
func (s *Server) conversationProse() string {
	var b strings.Builder
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		body := strings.TrimSpace(msg.Body)
		if body == "" {
			continue
		}
		switch msg.Kind {
		case chat.KindYou:
			fmt.Fprintf(&b, "They said:\n%s\n\n", body)
		case chat.KindAgent:
			who := "The agent said"
			if msg.Author != "" {
				who = "@" + msg.Author + " said"
			}
			fmt.Fprintf(&b, "%s:\n%s\n\n", who, body)
		}
	}
	return b.String()
}

// newSessionInWorktree checks the repository out again beside this one and
// starts the session there, with its own server, agent and conversation. This
// server is left alone: the run in flight here carries on, and the two agents
// cannot tread on each other's files.
func (s *Server) newSessionInWorktree(w http.ResponseWriter, r *http.Request, message string, selected *chat.Context, files []chat.Incoming, provider, model, branch string, sandboxed *bool) {
	slug := strings.ToLower(Slug(branch))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "name a branch for the new worktree")
		return
	}
	root := s.space().homeRoot
	path := gitdiff.WorktreePath(root, slug)
	// Off the base branch, not HEAD: a new session is new work, and should
	// not carry whatever branch this checkout happens to be on. A repository
	// with no main to speak of branches from HEAD.
	if err := gitdiff.AddWorktreeFrom(r.Context(), root, path, slug, gitdiff.BaseBranch(r.Context(), root)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The repository's untracked seed — .env and the like. Never fatal: the
	// checkout is there either way.
	if _, err := worktrees.Sow(r.Context(), root, path); err != nil {
		slog.Warn("some of the worktree seed did not carry", "path", path, "err", err)
	}

	s.handOff(w, path, slug, message, selected, files, provider, model, sandboxed)
}

// newSessionInScratch starts the session in a checkout under ~/.ade, cut from
// nothing: this repository is not involved at all.
func (s *Server) newSessionInScratch(w http.ResponseWriter, message string, selected *chat.Context, files []chat.Incoming, provider, model, name string, sandboxed *bool) {
	dir, err := scratch.New(s.ctx, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.handOff(w, dir, filepath.Base(dir), message, selected, files, provider, model, sandboxed)
}

// handOff starts the new checkout's own ade and hands it the prompt, so that
// server writes the attachments: the checkout they belong in is that one.
func (s *Server) handOff(w http.ResponseWriter, path, branch, message string, selected *chat.Context, files []chat.Incoming, provider, model string, sandboxed *bool) {
	url, err := s.openIn(s.ctx, path, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("the checkout is at %s but its ade did not start: %v", path, err))
		return
	}
	// Nothing typed means nothing to hand over: land there and let them type.
	if message != "" || len(files) > 0 {
		body, err := json.Marshal(map[string]any{"message": message, "context": selected, "files": files, "provider": provider, "model": model, "sandbox": sandboxed})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.ask(s.ctx, url, http.MethodPost, "/api/chat/new", string(body)); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("ade is up at %s but would not take the prompt: %v", url, err))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path, "branch": branch})
}

// handleChatModel moves the conversation onto another model, keeping the
// session. Mid-run it waits its turn behind the run, like a message would:
// the process in flight is bound to the model it started with.
func (s *Server) handleChatModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if !decode(w, r, &req) {
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == s.agent.modelName() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_, ok := s.agent.begin(s.ctx, kindSwitchModel)
	if !ok {
		waiting := s.stack(queued{kind: kindSwitchModel, model: model})
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"queued": waiting})
		return
	}
	s.switchModel(model)
	s.agent.finish(nil)
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// switchModel does the switch and says so in the conversation, so the change
// of voice has a line explaining it. The caller holds the runner.
func (s *Server) switchModel(model string) {
	s.agent.switchModel(model)
	s.say(ChatMessage{Kind: chat.KindNote, Body: "switched to " + modelLabel(model), Run: kindSwitchModel})
}

// modelLabel names a model for a note, with the default spelled out.
func modelLabel(model string) string {
	if model == "" {
		return "the default model"
	}
	return model
}

// handleChatResume switches this server to another of its checkout's
// conversations. It is how the launcher hands a session to a server already
// running here, rather than starting a rival on the same checkout.
func (s *Server) handleChatResume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		// New sessions use the picked model; resumed sessions receive the model
		// recorded beside the transcript.
		Model string `json:"model"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.ID)
	if !validProvider(req.Provider) {
		writeError(w, http.StatusBadRequest, "choose claude or codex")
		return
	}
	provider := normalizeProvider(req.Provider)
	if id == "" {
		writeError(w, http.StatusBadRequest, "name a session to resume")
		return
	}
	// One server per checkout, so `ade --session none` in a served checkout
	// arrives here. It used to resolve to no transcript and do nothing.
	if id == sessions.None {
		// Held, not checked: startFresh resets provider, session and pending
		// conversation, and a turn could start between a check and the reset.
		if _, ok := s.agent.begin(s.ctx, kindChat); !ok {
			writeError(w, http.StatusConflict, "the agent is already running; wait for it or cancel it first")
			return
		}
		s.startFresh(provider, strings.TrimSpace(req.Model))
		s.agent.finish(nil)
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusOK, map[string]any{"session": ""})
		return
	}
	// railID, not Current: the launcher opens a checkout by the id it is on the
	// rail under, which before the first turn is this server's pending one.
	if id == s.railID() {
		writeJSON(w, http.StatusOK, map[string]any{"session": id})
		return
	}
	// Only a transcript that is actually here: --resume on a missing id fails
	// every run from then on.
	if providerTranscriptSize(s.space().homeRoot, provider, id) == 0 {
		writeError(w, http.StatusBadRequest, "that session has no transcript in this checkout")
		return
	}
	if s.agent.State().Running {
		writeError(w, http.StatusConflict, "the agent is already running; wait for it or cancel it first")
		return
	}
	s.agent.resumeProvider(provider, strings.TrimSpace(req.Model), id)
	s.hub.Broadcast("chat", chatEvent{Reset: true})
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"session": id})
}

// handleArchive puts a session away, or brings it back. The flag is machine
// wide, so the launcher and every server see the same list.
func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := settings.SetArchived(strings.TrimSpace(req.ID), req.Archived); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// runChat is one conversational turn.
func (s *Server) runChat(ctx context.Context, attached *chat.Context, message string, files []chat.File) {
	defer s.recoverRun(kindChat)

	if s.agent.State().Provider == "codex" {
		s.agent.step("Codex is working")
	}
	// Before the turn, so what the checks run against is what it changed.
	was := s.currentHash()
	turn, carried := s.turnPrompt(attached, message, files)
	err := s.sendTurn(ctx, turn, fileBlocks(files)...)
	if err == nil {
		s.dropHeard(carried)
	}
	s.endRun(err)
	if err != nil {
		slog.Warn("chat turn failed", "err", err)
	} else {
		s.spawn(func() { s.afterTurn(message) })
	}
	s.settleTurn()
	s.settleChange(was, err == nil)
}

// settleChange is what any finished turn earns, whoever sent it: the diff it
// may have changed, the checks against it, and the word that it is over.
func (s *Server) settleChange(was string, ok bool) {
	if err := s.refresh(s.ctx); err != nil {
		slog.Debug("refresh after a turn", "err", err)
	}
	if ok {
		s.spawn(func() { s.flashDesk() })
	}
	s.spawn(func() { s.verifyAfterTurn(was) })
	s.spawn(func() { s.capturePreview() })
	s.spawn(func() { s.attachOutbox() })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// afterTurn is the bookkeeping a finished turn earns: the session's cost so
// far, and — once, after the first exchange — a name for it.
func (s *Server) afterTurn(first string) {
	s.agent.tallyCost()
	s.nameSession(first)
	// A turn is the only thing that can have edited another mounted root.
	s.refreshRoots(s.ctx)
	s.hub.Broadcast("state", nil)
}

// retitleEvery is how many of your messages pass before ade looks again.
const retitleEvery = 12

// nameSession names a conversation after its first exchange, and renames it as
// it moves on — unless you named it yourself, which is final.
func (s *Server) nameSession(first string) {
	id := s.agent.Current()
	if id == "" || settings.NamedByYou(id) {
		return
	}
	named := settings.Titles()[id] != "" || s.agent.State().Title != ""
	said := s.chatStore().List(id)
	if named && !dueARetitle(said) {
		return
	}
	reply := ""
	for _, msg := range said {
		if msg.Kind == chat.KindAgent && msg.Author == "" && strings.TrimSpace(msg.Body) != "" {
			reply = msg.Body
			break
		}
	}
	if named {
		first, reply = lately(said)
	}
	if reply == "" {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
	defer cancel()
	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:    s.agent.bin,
		Dir:    s.space().homeRoot,
		Model:  outgoing.FastModel,
		Prompt: design.TitlePrompt(first, reply),
		Tools:  []string{},
		NoMCP:  true,
	})
	if err != nil {
		slog.Debug("name the session", "err", err)
		return
	}
	title := cleanTitle(turn.Text)
	if title == "" {
		return
	}
	if err := settings.SetTitle(id, title); err != nil {
		slog.Debug("save the session title", "err", err)
		return
	}
	s.agent.setTitle(title)
}

// dueARetitle counts your messages: the agent's are a turn, not a subject.
func dueARetitle(said []chat.Message) bool {
	yours := 0
	for _, msg := range said {
		if msg.Kind == chat.KindYou {
			yours++
		}
	}
	return yours > 0 && yours%retitleEvery == 0
}

// lately is your last message and the reply to it: what this is about now.
func lately(said []chat.Message) (first, reply string) {
	for i := len(said) - 1; i >= 0; i-- {
		msg := said[i]
		if reply == "" && msg.Kind == chat.KindAgent && msg.Author == "" && strings.TrimSpace(msg.Body) != "" {
			reply = msg.Body
			continue
		}
		if reply != "" && msg.Kind == chat.KindYou && strings.TrimSpace(msg.Body) != "" {
			return msg.Body, reply
		}
	}
	return "", reply
}

// cleanTitle takes the first line, without quotes or a full stop, capped.
func cleanTitle(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.Trim(strings.TrimSpace(line), `"'“”.`)
	if len(line) > 80 {
		line = line[:80]
	}
	return strings.TrimSpace(line)
}

// turnPrompt wraps what you said with the document, anything highlighted, and
// what named agents said. dropHeard, not this, is what forgets that last part.
func (s *Server) turnPrompt(attached *chat.Context, message string, files []chat.File) (string, []design.Heard) {
	doc := s.designDoc()
	from, text := "", ""
	if attached != nil {
		from, text = attached.From, attached.Text
	}
	// Named after the message, a slash command included.
	note := design.FilesNote(attachedFiles(files))
	// A slash command is Claude Code's, not the agent's: /compact, /clear, a
	// skill under .claude/commands. It is matched at the start of the prompt,
	// so it goes through as typed rather than wrapped in the turn's preamble.
	if strings.HasPrefix(message, "/") {
		return design.CommandPrompt(from, text, message) + note, nil
	}
	heard := s.heardSoFar()
	return design.TurnPrompt(doc, from, text, message, heard...) + note, heard
}

// attachedFiles is the attachments as the prompt names them.
func attachedFiles(files []chat.File) []design.Attached {
	var out []design.Attached
	for _, f := range files {
		out = append(out, design.Attached{Name: f.Name, Path: f.Path})
	}
	return out
}

// sendTurn runs one turn on the shared session, compacting and trying again if
// the window turns out to be full. The reply is not recorded here: it arrived
// as assistant events and was written into the conversation as it streamed,
// and the result event only repeats the last of them.
//
// Compacting is Claude Code's to do and it does it on its own when
// --autocompact is on. With it off, a full window ends the turn with a message
// saying to compact — which nobody is there to read mid-run, and the message
// the user sent is gone. So ade reads it, sends /compact, and puts the turn
// through again. Once: a second failure is the real answer.
func (s *Server) sendTurn(ctx context.Context, prompt string, images ...bridge.Image) error {
	err := s.turn(ctx, prompt, images...)
	if !fullWindow(err) {
		return err
	}
	s.agent.step("the context window is full — compacting and sending again")
	claude, ensure := s.agent.ensure(s.ctx, s.onAgentEvent)
	if ensure != nil {
		return err
	}
	if _, cerr := claude.SendCommand(ctx, "/compact"); cerr != nil {
		// The window is what stopped the turn, so that is what to report.
		slog.Debug("compact after a full window", "err", cerr)
		return err
	}
	return s.turn(ctx, prompt, images...)
}

// turn is one attempt at a turn.
func (s *Server) turn(ctx context.Context, prompt string, images ...bridge.Image) error {
	claude, err := s.agent.ensure(s.ctx, s.onAgentEvent)
	if err != nil {
		return err
	}
	// A skill or a custom command is a turn; nudging it for output talks over it.
	if strings.HasPrefix(prompt, "/") {
		home, _ := os.UserHomeDir()
		if !skills.Known(home, s.space().homeRoot, prompt) {
			_, err = claude.SendCommand(ctx, prompt)
			return err
		}
	}
	_, err = claude.Send(ctx, prompt, images...)
	return err
}

// fullWindowWords are how a turn says it ran out of context. The wording is
// Claude Code's and the API's underneath it, and has changed before, so this
// matches loosely: a miss costs the user a turn they have to send again by
// hand, and there is nothing else a message about compacting could mean.
var fullWindowWords = []string{
	"prompt is too long",
	"context low",
	"context left until auto-compact",
	"conversation is too long",
	"exceeds the context",
	"run /compact",
}

func fullWindow(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, word := range fullWindowWords {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

// endRun closes a run out: the reason it failed is written into the
// conversation, where "context canceled" would have told nobody anything.
func (s *Server) endRun(err error) {
	// Asked before finish, which clears the reason it was stopped for.
	why := s.agent.why(err)
	kind := s.agent.kind()
	// Before finish, which drains and forgets which prompts were welded.
	if err != nil {
		s.unweld()
	}
	s.agent.finish(err)
	// Written by root on a worker; read by the ssh user.
	session.HandToHost(statedir.Git(s.startGitDir))
	if err != nil {
		s.say(ChatMessage{Kind: chat.KindNote, Body: why, Failed: true, Run: kind})
		return
	}
	s.noteFinished(kind)
}

// noteFinished records how a turn ended, for the badge a finished run leaves
// blank. A commit message or a review reports in its own place.
func (s *Server) noteFinished(kind string) {
	if kind != kindChat && kind != kindWake {
		return
	}
	s.agent.finished(&Finished{
		Files:  len(s.agent.State().Touched),
		Asking: sessions.IsQuestion(s.lastReply()),
		At:     time.Now(),
	})
}

// lastReply is the agent's final word in this conversation, which is what
// decides whether the turn ended by asking you something.
func (s *Server) lastReply() string {
	all := s.chatStore().List(s.agent.Current())
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind == chat.KindAgent && all[i].Author == "" {
			return all[i].Body
		}
	}
	return ""
}

// onAgentEvent writes what the agent says and does into the conversation as it
// happens: prose as a reply, each tool call as a folded row, and each tool
// result onto the row of the call it answers.
func (s *Server) onAgentEvent(ev bridge.Event) { s.onRunEvent("")(ev) }

// onRunEvent is onAgentEvent for a run with a name. The name rides on every
// row, so the page can tell a commit message from an ordinary reply.
func (s *Server) onRunEvent(run string) func(bridge.Event) {
	return func(ev bridge.Event) { s.writeEvent(run, ev) }
}

func (s *Server) writeEvent(run string, ev bridge.Event) {
	if s.agent.tellBusy() {
		s.hub.Broadcast("agent", nil)
	}
	if ev.Notice != "" {
		s.children.noticed(ev.Notice)
	}
	s.noteEdits(ev.ToolUses)
	for _, tool := range ev.ToolUses {
		s.agent.touch()
		s.agent.noteTool(stepLine(tool))
		s.hub.Broadcast("agent", nil)
		msg := s.say(ChatMessage{
			Kind: chat.KindTool, Run: run, Tool: tool.Name, ToolID: tool.ID,
			Target: toolTarget(tool.Input), Input: string(tool.Input),
		})
		s.mu.Lock()
		s.tools[tool.ID] = msg.ID
		rule, ruled := s.ruled[tool.ID]
		delete(s.ruled, tool.ID)
		s.mu.Unlock()
		if ruled {
			msg.Rule = rule
			s.amend(msg)
		}
		if isAgentCall(tool.Name) {
			s.children.open(tool.ID, msg.ID, toolTarget(tool.Input))
		}
	}
	if text := strings.TrimSpace(ev.Text); text != "" {
		// Text is progress too, and a turn that is all prose would otherwise
		// look like silence to the watchdog.
		s.agent.touch()
		s.say(ChatMessage{Kind: chat.KindAgent, Run: run, Body: text, HTML: s.replyHTML(text)})
	}
	for _, result := range ev.ToolResults {
		s.agent.touch()
		s.answerTool(result)
	}
}

// onColdEvent is onAgentEvent for a cold run beside the main agent: the
// same rows, marked with the run's own kind so the page folds them, and
// without the watchdog touches, which belong to the main runner.
func (s *Server) onColdEvent(kind string) func(bridge.Event) {
	return func(ev bridge.Event) {
		s.noteEdits(ev.ToolUses)
		for _, tool := range ev.ToolUses {
			msg := s.say(ChatMessage{
				Kind: chat.KindTool, Run: kind, Tool: tool.Name, ToolID: tool.ID,
				Target: toolTarget(tool.Input), Input: string(tool.Input),
			})
			s.mu.Lock()
			s.tools[tool.ID] = msg.ID
			s.mu.Unlock()
		}
		if text := strings.TrimSpace(ev.Text); text != "" {
			s.say(ChatMessage{Kind: chat.KindAgent, Run: kind, Body: text, HTML: s.replyHTML(text)})
		}
		for _, result := range ev.ToolResults {
			s.answerTool(result)
		}
	}
}

// answerTool puts a tool's result onto the row of the call that made it.
// isAgentCall is a tool that starts a sub-agent, under either of its names.
func isAgentCall(name string) bool { return name == "Agent" || name == "Task" }

// amendChild writes a sub-agent's progress on the row of the call that made it.
func (s *Server) amendChild(msgID, doing string, steps int) {
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.ID != msgID {
			continue
		}
		msg.Doing, msg.Steps = doing, steps
		s.amend(msg)
		return
	}
}

// subagentsDir is where Claude Code writes the session's sub-agent
// transcripts, beside its own. Empty until the session has one.
func (s *Server) subagentsDir() string {
	path := s.agent.transcript()
	if path == "" {
		return ""
	}
	return filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
}

func (s *Server) answerTool(result bridge.ToolResult) {
	s.mu.Lock()
	id, ok := s.tools[result.ID]
	if ok {
		delete(s.tools, result.ID)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	session := s.agent.Current()
	for _, msg := range s.chatStore().List(session) {
		if msg.ID != id {
			continue
		}
		msg.Output = result.Content
		msg.Failed = result.IsError
		s.amend(msg)
		s.children.answered(result.ID, msg.ID, msg.Target, result.Content)
		// A dev server prints where it is listening; on a worker that is not
		// this machine, so the launcher is asked to bring the port over.
		go s.exposeDevPorts(context.Background(), result.Content)
		return
	}
}

// say writes a message into the current conversation and tells the page.
func (s *Server) say(msg ChatMessage) ChatMessage {
	if msg.Run == "" {
		msg.Run = s.agent.kind()
	}
	// The main agent's prose carries the model that wrote it, so a switch
	// mid-conversation is legible later. A named agent's model is its own.
	if msg.Kind == chat.KindAgent && msg.Author == "" && msg.Model == "" {
		msg.Model = s.agent.modelName()
	}
	if msg.At.IsZero() {
		msg.At = time.Now()
	}
	if msg.Kind == chat.KindYou {
		msg.Undo = s.snapshotTurn()
	}
	saved, err := s.chatStore().Append(s.agent.Current(), msg)
	if err != nil {
		slog.Warn("could not save the conversation", "err", err)
		return msg
	}
	// A prompt begins a turn, so whatever the agent then writes can be traced
	// back to what you asked for. Every flow that runs the agent says one first,
	// so this is the only hook lineage needs.
	if saved.Kind == chat.KindYou {
		s.beginTurn(saved.ID, saved.Body)
	}
	s.hub.Broadcast("chat", chatEvent{Message: &saved})
	return saved
}

// amend replaces a message already said, and tells the page.
func (s *Server) amend(msg ChatMessage) {
	if err := s.chatStore().Update(s.agent.Current(), msg); err != nil {
		slog.Debug("could not update the conversation", "err", err)
		return
	}
	s.hub.Broadcast("chat", chatEvent{Message: &msg})
}

// unsay removes a message, for a queued prompt you decided against. Without it
// the conversation keeps a prompt that will never run.
func (s *Server) unsay(id string) {
	if id == "" {
		return
	}
	session := s.agent.Current()
	// A prompt folded into this one was shown under it, and goes with it. One
	// level, not a walk: a fold never nests, and the store is not trusted to say so.
	for _, msg := range s.chatStore().List(session) {
		if msg.FoldedInto == id && msg.ID != id {
			if err := s.chatStore().Remove(session, msg.ID); err != nil {
				slog.Warn("could not remove the folded message", "err", err)
			}
		}
	}
	if err := s.chatStore().Remove(session, id); err != nil {
		slog.Warn("could not remove the dropped message", "err", err)
		return
	}
	s.hub.Broadcast("chat", chatEvent{Reset: true})
}

// resay rewrites a message on screen: what runs and what you see must agree.
func (s *Server) resay(id, body string) {
	if id == "" {
		return
	}
	session := s.agent.Current()
	for _, msg := range s.chatStore().List(session) {
		if msg.ID != id {
			continue
		}
		msg.Body = body
		if err := s.chatStore().Update(session, msg); err != nil {
			slog.Warn("could not rewrite the queued message", "err", err)
			return
		}
		s.hub.Broadcast("chat", chatEvent{Reset: true})
		return
	}
}

// onStep is what the runner's progress lines become: a note in the
// conversation, between the messages they came between.
func (s *Server) onStep(text string) {
	s.say(ChatMessage{Kind: chat.KindNote, Body: text})
	s.hub.Broadcast("agent", nil)
}

// beforeWake records the tree as a turn nobody sent begins, so the checks
// afterwards run against what that turn changed.
func (s *Server) beforeWake() {
	was := s.currentHash()
	s.mu.Lock()
	s.wakeWas = was
	s.mu.Unlock()
}

// afterWake is the same bookkeeping a sent turn earns. It is not a lesser
// turn: it spends the subscription and edits the tree exactly as one does.
func (s *Server) afterWake(err error) {
	s.mu.Lock()
	was := s.wakeWas
	s.mu.Unlock()
	if err == nil {
		s.noteFinished(kindWake)
		s.spawn(func() { s.agent.tallyCost(); s.refreshRoots(s.ctx) })
	}
	s.spawn(func() { s.settleChange(was, err == nil) })
}

func (s *Server) onAsk() {
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// onSession follows the conversation when the agent's id moves: a fresh agent
// naming itself, or a resume that forked.
func (s *Server) onSession(from, to string) {
	if err := s.chatStore().Rename(from, to); err != nil {
		slog.Warn("could not move the conversation to its session", "err", err)
	}
	// A session named for the first time is one the launcher's proxy cannot
	// resolve until it is told.
	go s.announceServing(s.ctx, to)
	// The record's proxied address has the old id in its path, and a reader
	// on another machine has nothing else to reach this server by.
	go s.reproxy(s.ctx)
	s.hub.Broadcast("chat", chatEvent{Reset: true})
}

// chatStore is the conversations of the checkout being served, read under the
// lock because a checkout swap replaces it.
func (s *Server) chatStore() *chat.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chat
}

// rehome points the conversation at another checkout's store. The old one is
// closed after the swap, so a message in flight lands somewhere.
//
// The same store asked for again — a rebind in the same directory, reached by
// a different spelling of its path — is left as it is: the file is locked to
// the handle already open, so opening it again would only wait on ourselves.
func (s *Server) rehome(gitDir string) error {
	path := chat.Path(gitDir)
	if sameFile(path, s.chatStore().Path()) {
		return nil
	}
	store, err := chat.Open(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	old := s.chat
	s.chat = store
	s.tools = map[string]string{}
	s.ruled = map[string]string{}
	s.mu.Unlock()
	if err := old.Close(); err != nil {
		slog.Debug("close the old chat store", "err", err)
	}
	s.hub.Broadcast("chat", chatEvent{Reset: true})
	return nil
}

// sameFile is whether two paths name one file on disk, whatever they look
// like: a temp dir on macOS is reachable under /var and /private/var.
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	infoA, err := os.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

// attachment trims what was highlighted, and drops an attachment with nothing
// in it.
func attachment(ctx *chat.Context) *chat.Context {
	if ctx == nil {
		return nil
	}
	text := strings.TrimSpace(ctx.Text)
	if text == "" {
		return nil
	}
	return &chat.Context{From: strings.TrimSpace(ctx.From), Text: text}
}

// qualifyView names an attachment taken from a foreign diff by its whole path.
// The agent runs at home, where the same relative path is a different file.
func qualifyView(root, from string) string {
	if from == "" || filepath.IsAbs(from) {
		return from
	}
	full := filepath.Join(root, from)
	if _, err := os.Stat(full); err != nil {
		return from
	}
	return full
}

// The agent's own screenshots, now that an attachment is internal/chat's.
// The API refuses an image over 5MB, so a bigger one is refused here.
const (
	maxImages     = 4
	maxImageBytes = 5 << 20
)

// pictures keeps the images that can be sent: image media, some bytes, not too
// many and none too big. An empty list is nil, so a message without any
// stores without the field.
func pictures(list []chat.Image) ([]chat.Image, error) {
	var out []chat.Image
	for _, img := range list {
		img.MediaType = strings.TrimSpace(img.MediaType)
		img.Data = strings.TrimSpace(img.Data)
		if img.Data == "" {
			continue
		}
		if !strings.HasPrefix(img.MediaType, "image/") {
			return nil, fmt.Errorf("%s is not an image", cmp.Or(img.MediaType, "that"))
		}
		if base64.StdEncoding.DecodedLen(len(img.Data)) > maxImageBytes {
			return nil, fmt.Errorf("an image is over %dMB, which is more than the agent can take", maxImageBytes>>20)
		}
		out = append(out, img)
	}
	if len(out) > maxImages {
		return nil, fmt.Errorf("%d images is too many; %d at most", len(out), maxImages)
	}
	return out, nil
}

// pictureBlocks is the images as the bridge sends them.
func pictureBlocks(list []chat.Image) []bridge.Image {
	if len(list) == 0 {
		return nil
	}
	out := make([]bridge.Image, len(list))
	for i, img := range list {
		out[i] = bridge.Image{MediaType: img.MediaType, Data: img.Data}
	}
	return out
}

// prettyInput reformats a tool's arguments for reading, or leaves them alone
// when they are not JSON after all.
func prettyInput(raw string) string {
	var buf json.RawMessage
	if err := json.Unmarshal([]byte(raw), &buf); err != nil {
		return raw
	}
	out, err := json.MarshalIndent(buf, "", "  ")
	if err != nil {
		return raw
	}
	return string(out)
}

// handleChatDecide allows or denies the tool call a turn is paused on. The
// reason reaches the model, so a denial can say what to do instead.
func (s *Server) handleChatDecide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToolID string `json:"toolId"`
		Allow  bool   `json:"allow"`
		// Remember is "", "session" or "always".
		Remember string `json:"remember"`
		Reason   string `json:"reason"`

		// Rule is what to remember, as the card's own box had it. DenyRule
		// puts it on the deny list rather than the allow list.
		Rule     string `json:"rule"`
		DenyRule bool   `json:"denyRule"`

		// Free stops the asking for the rest of the session, as one gesture
		// with allowing the call the card is on.
		Free bool `json:"free"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ToolID == "" {
		writeError(w, http.StatusBadRequest, "name the tool call to decide")
		return
	}
	switch req.Remember {
	case "", bridge.RememberSession, bridge.RememberAlways:
	default:
		writeError(w, http.StatusBadRequest, "remember is empty, session or always")
		return
	}
	// Both before the decision: the turn resumes on it, and the next call may
	// be gated before a write that happened afterwards had landed.
	if req.Free {
		rules := s.agent.rulesStore()
		if rules == nil {
			writeError(w, http.StatusConflict, "ade has no rules to stop checking")
			return
		}
		rules.SetFree(true)
		slog.Info("permission asking", "free", true)
	}
	if req.Remember != "" && req.Rule != "" {
		if err := s.remember(permissions.Rule(req.Rule), req.DenyRule, req.Remember == bridge.RememberSession); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := s.agent.decide(req.ToolID, req.Allow, req.Remember, req.Reason); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.agent.touch()
	w.WriteHeader(http.StatusNoContent)
}

// handleChatAnswer answers the question a turn is paused on, by tool call id.
func (s *Server) handleChatAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToolID  string            `json:"toolId"`
		Answers map[string]string `json:"answers"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.ToolID == "" || len(req.Answers) == 0 {
		writeError(w, http.StatusBadRequest, "choose an answer first")
		return
	}
	if err := s.agent.answer(req.ToolID, req.Answers); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.agent.touch()
	w.WriteHeader(http.StatusNoContent)
}

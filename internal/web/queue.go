package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// queued is a chat turn waiting its turn at the agent.
//
// There is one agent and one run at a time, and a conversation is a poor place
// to be told to come back later: you have the next thing to say while it is
// still working on the last. So a turn sent during a run stacks up here and
// goes when the agent is free.
type queued struct {
	// id names this entry so it can be removed without counting positions,
	// which shift as the queue drains under you.
	id string
	at time.Time

	kind    string
	message string
	context *chat.Context
	files   []chat.File

	// model is what a switch-model entry switches to.
	model string

	// thread is the comment a convert or a single draft-reply is for. Held as an
	// id, so the run works from what the thread says when its turn comes.
	thread string

	// threads is the same for a resolve, which takes a set.
	threads []string

	// level is a review's depth; reply is the named agent's message a fold is
	// to put into the open document.
	level string
	reply string

	// chatID is the message already on screen for this turn. A queued prompt is
	// shown the moment you send it, so dropping the turn has to take the
	// message with it.
	chatID string
}

// queueState is the queue on disk, so a restart picks up what was waiting
// rather than leaving messages on screen that will never run.
const queueState = "queue"

// queuedRecord is a queued turn as stored: the same fields, exported.
type queuedRecord struct {
	ID      string        `json:"id"`
	At      time.Time     `json:"at,omitempty"`
	Kind    string        `json:"kind"`
	Message string        `json:"message,omitempty"`
	Context *chat.Context `json:"context,omitempty"`
	Files   []chat.File   `json:"files,omitempty"`
	Model   string        `json:"model,omitempty"`
	Thread  string        `json:"thread,omitempty"`
	Threads []string      `json:"threads,omitempty"`
	Level   string        `json:"level,omitempty"`
	Reply   string        `json:"reply,omitempty"`
	ChatID  string        `json:"chatId,omitempty"`
}

// saveQueue writes the queue through. The caller holds the lock.
func (s *Server) saveQueue() {
	if len(s.queue) == 0 {
		if err := s.chat.PutState(queueState, nil); err != nil {
			slog.Debug("clear the saved queue", "err", err)
		}
		return
	}
	records := make([]queuedRecord, 0, len(s.queue))
	for _, q := range s.queue {
		records = append(records, queuedRecord{
			ID: q.id, Kind: q.kind, Message: q.message, Context: q.context, Files: q.files,
			At: q.at, Model: q.model, Thread: q.thread, Threads: q.threads,
			Level: q.level, Reply: q.reply, ChatID: q.chatID,
		})
	}
	data, err := json.Marshal(records)
	if err == nil {
		err = s.chat.PutState(queueState, data)
	}
	if err != nil {
		slog.Debug("save the queue", "err", err)
	}
}

// A restored queue is bounded: the store is a file in the git dir, which
// anything that can write there can plant a queue into, so a turn is only
// taken back when it is the size of something typed and names a message
// that is actually in the conversation.
const (
	maxRestored        = 20
	maxRestoredMessage = 64 * 1024
)

// loadQueue reads back what was waiting when the last server stopped and
// reports how many turns came back. They wait rather than run: the user says
// when, since nothing on screen asked for them this time. Ids carry on from
// the highest saved, so nothing on the page collides.
func (s *Server) loadQueue() int {
	data := s.chat.State(queueState)
	if len(data) == 0 {
		return 0
	}
	var records []queuedRecord
	if err := json.Unmarshal(data, &records); err != nil {
		slog.Debug("read the saved queue", "err", err)
		return 0
	}
	if len(records) > maxRestored {
		records = records[:maxRestored]
	}
	shown := map[string]bool{}
	for _, msg := range s.chat.List(s.agent.Current()) {
		shown[msg.ID] = true
	}
	attachments := chat.AttachDir(s.space().gitDir)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range records {
		// A model switch has no message of its own; everything else does.
		onScreen := shown[r.ChatID] || (r.Kind == kindSwitchModel && r.ChatID == "")
		if !onScreen || len(r.Message) > maxRestoredMessage {
			slog.Debug("dropping a saved queue entry", "id", r.ID, "kind", r.Kind)
			continue
		}
		s.queue = append(s.queue, queued{
			id: r.ID, at: r.At, kind: r.Kind, message: r.Message, context: r.Context,
			files: restoredFiles(attachments, r.Files),
			model: r.Model, thread: r.Thread, threads: r.Threads,
			level: r.Level, reply: r.Reply, chatID: r.ChatID,
		})
		var n int
		if _, err := fmt.Sscanf(r.ID, "q%d", &n); err == nil && n > s.nextQueued {
			s.nextQueued = n
		}
	}
	s.saveQueue()
	return len(s.queue)
}

// restoredFiles keeps only attachments this checkout wrote. A stored path is
// read by chat.Block and named to the agent, so dir bounds it like the message.
func restoredFiles(dir string, files []chat.File) []chat.File {
	var out []chat.File
	for _, f := range files {
		if filepath.Dir(filepath.Clean(f.Path)) != dir {
			slog.Debug("dropping a saved attachment from outside the attachments directory", "path", f.Path)
			continue
		}
		out = append(out, f)
	}
	return out
}

// Waiting is one queued turn, as the page shows it. ChatID names the message
// it will run, so the conversation can mark that message as still waiting.
type Waiting struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	What string `json:"what"`
	// Message is the whole prompt; What is truncated for the row.
	Message string `json:"message,omitempty"`
	ChatID  string `json:"chatId,omitempty"`
}

// describe is what the queue entry says it will do. Without it the list is a
// column of kinds, which tells you how many are waiting and nothing else.
func (q queued) describe() string {
	switch q.kind {
	case kindConvert:
		return "rewrite a review comment for GitHub"
	case kindSwitchModel:
		return "switch to " + modelLabel(q.model)
	case kindCommitMessage:
		return "write a commit message"
	case kindFixChecks:
		return "read the failing checks"
	case kindReview:
		return strings.TrimSpace("review the diff " + q.level)
	case kindResolve:
		return fmt.Sprintf("apply %s", count(len(q.threads), "comment"))
	case kindDraftReply:
		if q.thread != "" {
			return "answer one review comment"
		}
		return "answer every open review comment"
	case kindPRCreate:
		return "open a pull request"
	case kindSeed:
		return "read the design document"
	case kindFold:
		return "fold a reply into the design document"
	default:
		return truncate(strings.TrimSpace(q.message), 140)
	}
}

func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// label is the human name of a run kind, used by the queue and the page.
func label(kind string) string {
	switch kind {
	case kindChat:
		return "Chat"
	case kindConvert:
		return "Convert for GitHub"
	case kindSwitchModel:
		return "Switch model"
	case kindCommitMessage:
		return "Commit message"
	case kindReview:
		return "Review"
	case kindResolve:
		return "Resolve"
	case kindDraftReply:
		return "Draft reply"
	case kindPRCreate:
		return "Create PR"
	case kindFixChecks:
		return "Fix checks"
	case kindSeed:
		return "Read the design"
	case kindFold:
		return "Fold into the design"
	case "build":
		return "Build the design"
	}
	return kind
}

// stack puts a turn behind whatever is running and reports how many are now
// waiting.
func (s *Server) stack(q queued) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextQueued++
	q.id = fmt.Sprintf("q%d", s.nextQueued)
	if q.at.IsZero() {
		q.at = time.Now()
	}
	s.queue = append(s.queue, q)
	s.saveQueue()
	return len(s.queue)
}

// stackOnce is stack for a button: a second press while one waits meant once.
func (s *Server) stackOnce(q queued) int {
	s.mu.Lock()
	for _, held := range s.queue {
		if sameAsk(held, q) {
			n := len(s.queue)
			s.mu.Unlock()
			return n
		}
	}
	s.mu.Unlock()
	return s.stack(q)
}

// queueBehind is the answer a button gets when a run has the agent. Refusing
// sent you back later for something that would still want doing.
func (s *Server) queueBehind(w http.ResponseWriter, q queued) {
	n := s.stackOnce(q)
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"queued": n})
}

// sameAsk is whether two button presses asked for the same thing. The kind is
// not enough: two draft replies to different comments are two asks.
func sameAsk(a, b queued) bool {
	return a.kind == b.kind && a.thread == b.thread && a.reply == b.reply &&
		a.level == b.level && strings.Join(a.threads, "\x00") == strings.Join(b.threads, "\x00")
}

// Waiting is the queue as the browser sees it, in the order it will run.
func (s *Server) waiting() []Waiting {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Waiting, 0, len(s.queue))
	for _, q := range s.queue {
		out = append(out, Waiting{ID: q.id, Kind: label(q.kind),
			What: q.describe(), Message: q.message, ChatID: q.chatID})
	}
	return out
}

// edit rewrites a waiting prompt and the message on screen with it.
func (s *Server) edit(id, message string) (found, editable bool) {
	var shown string
	defer func() {
		if shown == "" {
			return
		}
		s.resay(shown, message)
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.queue {
		if q.id != id {
			continue
		}
		if q.message == "" {
			return true, false
		}
		s.queue[i].message = message
		shown = q.chatID
		s.saveQueue()
		return true, true
	}
	return false, false
}

// drop removes one queued turn, for changing your mind about a single prompt
// rather than cancelling everything.
func (s *Server) drop(id string) bool {
	var dropped queued
	// Unsaid after the lock: removing the message takes the store's own.
	defer func() { s.unsay(dropped.chatID) }()

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, q := range s.queue {
		if q.id == id {
			s.queue = append(s.queue[:i:i], s.queue[i+1:]...)
			dropped = q
			s.saveQueue()
			return true
		}
	}
	return false
}

// promote moves a waiting turn to the front, for when what you said next
// matters more than what you said before it. It does not interrupt the run
// that is going: the queue only moves when that one ends.
func (s *Server) promote(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, q := range s.queue {
		if q.id != id {
			continue
		}
		if i > 0 {
			s.queue = append([]queued{q}, append(s.queue[:i:i], s.queue[i+1:]...)...)
			s.saveQueue()
		}
		return true
	}
	return false
}

// peek is the next turn, left where it is.
func (s *Server) peek() (queued, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.queue) == 0 {
		return queued{}, false
	}
	return s.queue[0], true
}

// take removes one turn by id, wherever Cancel or promote left it.
func (s *Server) take(id string) (queued, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.queue, func(q queued) bool { return q.id == id })
	if i < 0 {
		return queued{}, false
	}
	next := s.queue[i]
	s.queue = slices.Delete(s.queue, i, i+1)
	s.saveQueue()
	return next, true
}

func (s *Server) unstack() (queued, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return queued{}, false
	}
	next := s.queue[0]
	s.queue = s.queue[1:]
	s.saveQueue()
	return next, true
}

// restack puts a turn back at the front, for when the agent was claimed out
// from under it.
func (s *Server) restack(q queued) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append([]queued{q}, s.queue...)
	s.saveQueue()
}

// dropStack throws away everything waiting and reports how much went.
func (s *Server) dropStack() int {
	s.mu.Lock()
	gone := s.queue
	s.queue = nil
	s.saveQueue()
	s.mu.Unlock()

	// Cancelling drops the queue, so it drops what the queue put on screen too.
	for _, q := range gone {
		s.unsay(q.chatID)
	}
	return len(gone)
}

// stackedThreads is the comments waiting to be rewritten, so each button can
// say whether it is the one working or one of the ones behind it.
func (s *Server) stackedThreads() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []string
	for _, q := range s.queue {
		if q.kind == kindConvert && q.thread != "" {
			out = append(out, q.thread)
		}
	}
	return out
}

func (s *Server) stacked() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.queue)
}

func (s *Server) queueSince() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.queue) == 0 {
		return time.Time{}
	}
	return s.queue[0].at
}

// drain starts the next stacked turn once the agent is free.
func (s *Server) drain() { s.drainAfter(time.Time{}) }

// drainAfter is drain as the end of a run calls it, with when that run began,
// so chat prompts typed while it ran can go to the agent as one turn.
func (s *Server) drainAfter(since time.Time) {
	s.forgetWeld()
	next, ok := s.peek()
	if !ok {
		// Nothing waiting: the session is idle, and what was said in it is
		// ready to be distilled once it stays that way.
		s.quietly()
		return
	}

	// Decided with the queue left alone: the model takes seconds, and a turn
	// still queued is one Cancel can still drop.
	var folded []queued
	if next.kind == kindChat {
		folded = s.weldable(next, since)
	}

	runCtx, started := s.agent.begin(s.ctx, next.kind)
	if !started {
		// Something else claimed the agent first. That run will drain on its
		// way out, and the turn is still queued for it.
		return
	}
	if next, ok = s.take(next.id); !ok {
		// Dropped while the model read. Finishing drains whatever is left.
		s.agent.finish(nil)
		return
	}
	if len(folded) > 0 {
		next = s.weld(next, folded)
	}

	switch next.kind {
	case kindChat:
		s.spawn(func() { s.runChatTurn(runCtx, next.context, next.message, next.files) })
	case kindSwitchModel:
		// Not a run: the switch is the whole job, and finishing lets the
		// next thing waiting go.
		s.switchModel(next.model)
		s.agent.finish(nil)
		return
	case kindCommitMessage:
		// Read now, not when it was queued: staging goes on while the agent
		// works, and the message describes the index it will commit.
		staged, err := gitdiff.StagedFiles(s.ctx, s.space().diffRoot())
		if err != nil || len(staged) == 0 {
			s.agent.finish(nil)
			return
		}
		s.setDraft(nil)
		s.spawn(func() { s.runCommitMessage(runCtx, staged) })
	case kindFixChecks:
		// Read now, not when it was queued: a run that took ten minutes is
		// long enough for the checks to have gone green on their own.
		pr := s.currentBranchPR()
		blocked := s.space().pr
		if pr == nil || (blocked != nil && blocked.ResolveBlocked != "") {
			s.agent.finish(nil)
			return
		}
		failing := failingChecks(pr.Checks)
		if len(failing) == 0 {
			s.agent.finish(nil)
			return
		}
		s.spawn(func() { s.runFixChecks(runCtx, pr.Number, failing) })
	case kindReview:
		s.mu.RLock()
		empty := s.diff == nil || len(s.diff.Files) == 0
		s.mu.RUnlock()
		if empty {
			s.agent.finish(nil)
			return
		}
		s.spawn(func() { s.runReview(runCtx, next.level) })
	case kindResolve:
		threads := s.threadsToResolve(next.threads)
		if s.resolveBlocked() != "" || len(threads) == 0 {
			s.agent.finish(nil)
			return
		}
		s.spawn(func() { s.runResolve(runCtx, threads) })
	case kindDraftReply:
		threads, ok := s.repliesToDraft(next.thread)
		if !ok {
			s.agent.finish(nil)
			return
		}
		s.spawn(func() { s.runDraftReplies(runCtx, threads) })
	case kindPRCreate:
		if _, why := s.prCreateBlocked(); why != "" {
			s.agent.finish(nil)
			return
		}
		s.spawn(func() { s.runPRCreate(runCtx) })
	case kindSeed:
		file, prompt, ok := s.seedNow()
		if !ok {
			s.agent.finish(nil)
			return
		}
		s.saySeed(file)
		s.spawn(func() { s.runTurn(runCtx, prompt, "reading "+file) })
	case kindFold:
		file, author, prompt, ok := s.foldNow(next.reply)
		if !ok {
			s.agent.finish(nil)
			return
		}
		s.sayFold(author, file)
		s.spawn(func() { s.runTurn(runCtx, prompt, "updating "+file) })
	case kindConvert:
		// Converts no longer queue; one saved by an older ade runs the way
		// they run now, beside the agent, and the runner is let go.
		s.agent.finish(nil)
		if thread, err := s.space().store.Get(next.thread); err == nil {
			s.startJobs([]sideJob{{name: kindConvert, cold: true, run: func(ctx context.Context) { s.runConvert(ctx, []comments.Thread{thread}) }}}, 0)
		}
		return
	default:
		// Nothing knows how to run this, so let it go rather than hold the lock.
		slog.Warn("dropping a queued turn of an unknown kind", "kind", next.kind)
		s.agent.finish(nil)
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// agentState is the run state with the queue depth folded in, since the runner
// knows about runs and the server knows about what is waiting for one.
func (s *Server) agentState() RunState {
	out := s.agent.State()
	out.Queued = s.stacked()
	out.QueuedThreads = s.stackedThreads()
	out.Waiting = s.waiting()
	out.SideRuns = s.sideRuns()
	out.Welded = s.weldedID()
	out.Children = s.children.snapshot()
	return out
}

// handleRunQueued starts the next waiting turn now. A queue a restart brought
// back does not run on its own; this is how it is set going.
func (s *Server) handleRunQueued(w http.ResponseWriter, _ *http.Request) {
	if s.stacked() == 0 {
		writeError(w, http.StatusNotFound, "nothing is queued")
		return
	}
	s.drain()
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handlePromoteQueued moves one waiting turn to the front of the queue.
func (s *Server) handlePromoteQueued(w http.ResponseWriter, r *http.Request) {
	if !s.promote(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "that is not in the queue; it may have already run")
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleEditQueued rewrites a waiting prompt, keeping its place.
func (s *Server) handleEditQueued(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if !decode(w, r, &req) {
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "say what it should ask instead, or remove it")
		return
	}
	found, editable := s.edit(r.PathValue("id"), message)
	if !found {
		writeError(w, http.StatusNotFound, "that is not in the queue; it may have already run")
		return
	}
	if !editable {
		writeError(w, http.StatusBadRequest, "that entry is a button, not a prompt: there is no wording to change")
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleDropQueued removes one waiting turn. Cancel stops everything; this is
// for changing your mind about a single prompt.
func (s *Server) handleDropQueued(w http.ResponseWriter, r *http.Request) {
	if !s.drop(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "that is not in the queue; it may have already run")
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

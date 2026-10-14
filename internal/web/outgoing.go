package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/outgoing"
)

// readComments is a seam: the tests for stampSentExternalIDs have no GitHub to
// read back from. repoOf and postReply are the same for the reply batch.
var (
	readComments = ghpr.Comments
	repoOf       = ghpr.Repo
	postReply    = ghpr.PostReply
)

// handleConvert rewrites one finding as a comment you could leave on the pull
// request, and shows it to you. Nothing is sent.
//
// One at a time, on the comment itself: you decide per comment whether it is
// worth saying to the author, and a single short rewrite comes back quickly.
//
// It runs cold and tool-less on the small model with no MCP servers, because it
// is a rephrase of text you already wrote with no code to read.
func (s *Server) handleConvert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	thread, err := s.space().store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if why := notSendable(thread); why != "" {
		writeError(w, http.StatusBadRequest, why)
		return
	}

	// Beside the main agent, not in its queue: a rewrite is a second on the
	// small model, and waiting for a build to finish first is waiting for
	// nothing. It takes a side slot, so Stop ends it and the graph shows it.
	if !s.startJobs([]sideJob{{name: kindConvert, cold: true, run: func(ctx context.Context) {
		s.runConvert(ctx, []comments.Thread{thread})
	}}}, 0) {
		writeError(w, http.StatusConflict, fmt.Sprintf("%d agents are already running; wait for one, or stop them", maxSides))
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

// notSendable says why a thread cannot go to the pull request, or "" when it
// can. The reasons are all about whose words they are and where they came from.
func notSendable(thread comments.Thread) string {
	switch {
	case thread.ExternalID != "":
		// A thread from the pull request is answered with a reply in its own
		// thread, not a new comment on the line. handleSendReply does that.
		return "this came from the pull request; answer it with a reply"
	case thread.Sent():
		return "this has already been sent"
	case thread.Resolved:
		return "this thread is resolved"
	case thread.Orphaned:
		return "the line this was about has gone from the diff"
	case thread.Scope == comments.ScopeRepo:
		return "a comment on the change as a whole has no line to attach to"
	case thread.File == "" || thread.Line <= 0:
		return "this comment is not anchored to a line"
	}
	return ""
}

// runConvert is one rewrite, run as a side job: it never touches the main
// runner, so what it says goes into the conversation under its own kind.
func (s *Server) runConvert(ctx context.Context, pending []comments.Thread) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("convert panicked", "panic", r)
		}
	}()
	err := s.convertOnce(ctx, pending)
	if err != nil {
		slog.Warn("convert for github failed", "err", err)
		if ctx.Err() == nil {
			s.say(ChatMessage{Kind: chat.KindNote, Run: kindConvert, Failed: true, Body: "convert for GitHub: " + err.Error()})
		}
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) convertOnce(ctx context.Context, pending []comments.Thread) error {
	s.say(ChatMessage{Kind: chat.KindNote, Run: kindConvert, Body: fmt.Sprintf("rewriting %d comment(s) for the author", len(pending))})

	// Always the small model, whatever the conversation is on: this is a
	// rephrase of text you already wrote, with no code to read, and a big
	// model does it no better and much slower.
	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:          s.agent.bin,
		Model:        outgoing.FastModel,
		Dir:          s.space().homeRoot,
		Prompt:       outgoing.Prompt(pending),
		AllowedTools: outgoing.Tools(),
		// No tools and nothing to look up, so the MCP servers are pure startup
		// cost — most of it, in fact. This is the difference between a rewrite
		// you wait for and one you do not.
		NoMCP:   true,
		OnEvent: s.onColdEvent(kindConvert),
	})
	if err != nil {
		return err
	}

	rewritten, err := outgoing.Parse(turn.Text)
	if err != nil {
		return err
	}

	store := s.space().store
	saved := 0
	for _, thread := range pending {
		text, ok := rewritten[thread.ID]
		if !ok {
			continue
		}
		if _, err := store.SetOutgoing(thread.ID, text); err != nil {
			slog.Debug("save rewritten comment", "thread", thread.ID, "err", err)
			continue
		}
		saved++
	}
	if saved == 0 {
		return fmt.Errorf("none of the %d comment(s) came back rewritten", len(pending))
	}

	s.say(ChatMessage{Kind: chat.KindNote, Run: kindConvert, Body: fmt.Sprintf("%d comment(s) ready to read", saved)})
	return nil
}

// handleSend posts the rewritten comments to the pull request as one review.
//
// Only what you have read: a thread with no rewritten text is not sent, and the
// request has to name the pull request it means.
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm int    `json:"confirm"`
		Body    string `json:"body"`
		// Thread sends just that one, for the button on the comment itself.
		Thread string `json:"thread"`
	}
	if !decode(w, r, &req) {
		return
	}

	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to comment on")
		return
	}
	if req.Confirm != pr.Number {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("sending to #%d has to name it: send confirm=%d", pr.Number, pr.Number))
		return
	}

	// One thread, or everything converted. Sending one at a time is what the
	// buttons on a comment do; the bar sends the lot as a single review.
	ready := readyToSend(s.space().store.Sendable())
	if req.Thread != "" {
		ready = onlyThread(ready, req.Thread)
		if len(ready) == 0 {
			writeError(w, http.StatusBadRequest, whySendRefused(s.space().store, req.Thread))
			return
		}
	}
	if len(ready) == 0 {
		writeError(w, http.StatusBadRequest, "nothing has been converted yet")
		return
	}

	ws := s.space()
	owner, repo, err := ghpr.Repo(r.Context(), ws.homeRoot)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	inline := make([]ghpr.InlineComment, 0, len(ready))
	ids := make([]string, 0, len(ready))
	for _, thread := range ready {
		side := "RIGHT"
		if thread.Side == ghpr.SideLeft {
			side = "LEFT"
		}
		inline = append(inline, ghpr.InlineComment{
			Path: thread.File, Line: thread.Line, Side: side, Body: thread.Outgoing,
		})
		ids = append(ids, thread.ID)
	}

	// Not the request's context: a closed tab would kill gh after GitHub had
	// accepted the review, and the unmarked threads would go again on the next click.
	post := context.WithoutCancel(r.Context())
	if err := ghpr.PostReview(post, ws.homeRoot, owner, repo, pr.Number, req.Body, inline); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	slog.Info("sent review comments", "number", pr.Number, "comments", len(inline))

	// Marked before anything else can fail: they are on GitHub now, and
	// offering them again would post them twice.
	if err := ws.store.MarkSent(ids, time.Now()); err != nil {
		slog.Warn("could not record what was sent", "err", err)
	}
	s.stampSentExternalIDs(post, ws, pr.Number, ready)

	s.refreshBranchPR(s.ctx)
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"sent": len(inline), "pr": pr.Number})
}

func onlyThread(pending []comments.Thread, id string) []comments.Thread {
	for _, thread := range pending {
		if thread.ID == id {
			return []comments.Thread{thread}
		}
	}
	return nil
}

// readyToSend is the threads you have text for and have therefore seen.
// whySendRefused says why one named thread did not make it into the batch.
// Without it the refusal blamed conversion, which is wrong for a thread that
// converted and then drifted off the diff.
func whySendRefused(store *comments.Store, id string) string {
	thread, err := store.Get(id)
	if err != nil {
		return "that comment is no longer here"
	}
	if why := notSendable(thread); why != "" {
		return why
	}
	if thread.Outgoing == "" {
		return "that comment has not been converted yet"
	}
	return "that comment cannot be sent"
}

func readyToSend(pending []comments.Thread) []comments.Thread {
	var out []comments.Thread
	for _, thread := range pending {
		if thread.Outgoing != "" {
			out = append(out, thread)
		}
	}
	return out
}

// stampSentExternalIDs finds the GitHub ID PostReview just gave each comment
// it posted, so a reply to it can be recognised on the next import. GitHub's
// review API does not hand these back; the only way to learn them is to read
// the thread again and match what was just sent by where it landed and what
// it said.
//
// Best-effort: a miss here just means a reply to that comment imports as a
// new thread instead of merging into this one, not a lost comment.
func (s *Server) stampSentExternalIDs(ctx context.Context, ws *workspace, number int, sent []comments.Thread) {
	found, err := readComments(ctx, ws.homeRoot, number)
	if err != nil {
		slog.Debug("read comments after send", "err", err)
		return
	}
	for _, thread := range sent {
		id, ok := matchSent(thread, found)
		if !ok {
			continue
		}
		if _, err := ws.store.SetExternal(thread.ID, strconv.FormatInt(id, 10)); err != nil {
			slog.Debug("stamp external id", "thread", thread.ID, "err", err)
		}
	}
}

// matchSent finds the upstream comment a thread was just posted as. Path, line,
// side and the exact body are enough: the body is text you wrote and sent once,
// to that line.
func matchSent(thread comments.Thread, found []ghpr.ReviewComment) (int64, bool) {
	for _, c := range found {
		if c.ReplyTo != 0 || c.Path != thread.File || c.Line != thread.Line || c.Side != thread.Side {
			continue
		}
		if strings.TrimSpace(c.Body) != strings.TrimSpace(thread.Outgoing) {
			continue
		}
		return c.ID, true
	}
	return 0, false
}

// handleSetOutgoing replaces the rewritten text for one thread. The agent
// drafted it; changing it before it goes is the point of showing it.
func (s *Server) handleSetOutgoing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}

	thread, err := s.space().store.SetOutgoing(r.PathValue("id"), strings.TrimSpace(req.Text))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("threads", nil)
	writeJSON(w, http.StatusOK, thread)
}

// handleSendReply posts a thread's pending reply into the thread the reviewer is
// reading, rather than as a new comment on the line.
func (s *Server) handleSendReply(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	thread, err := ws.store.Get(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if why := notReplyable(thread); why != "" {
		writeError(w, http.StatusBadRequest, why)
		return
	}

	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to reply on")
		return
	}
	owner, repo, err := repoOf(r.Context(), ws.homeRoot)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Not the request's context: see handleSendReplies.
	sent, err := s.sendOneReply(context.WithoutCancel(r.Context()), ws, pr.Number, owner, repo, thread)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent, "pr": pr.Number})
}

// handleSendReplies posts every pending reply, one request per thread. A
// partial failure reports what went: GitHub has those already.
func (s *Server) handleSendReplies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm int `json:"confirm"`
		// Threads is what the modal listed. Only these are posted: a draft
		// that landed while it was open was never on screen to be read.
		Threads []string `json:"threads"`
	}
	if !decode(w, r, &req) {
		return
	}

	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to reply on")
		return
	}
	if req.Confirm != pr.Number {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("replying on #%d has to name it: send confirm=%d", pr.Number, pr.Number))
		return
	}
	if len(req.Threads) == 0 {
		writeError(w, http.StatusBadRequest, "name the threads to reply on: send threads=[…]")
		return
	}

	ws := s.space()
	ready := onlyThreads(replyable(ws.store.List()), req.Threads)
	if len(ready) == 0 {
		writeError(w, http.StatusBadRequest, "none of those threads has a reply waiting")
		return
	}
	owner, repo, err := repoOf(r.Context(), ws.homeRoot)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Not the request's context: a closed tab would kill gh after GitHub had
	// accepted the post, and the unmarked reply would go again on the next click.
	ctx := context.WithoutCancel(r.Context())
	sent, threads := 0, 0
	failed := []map[string]string{}
	for _, thread := range ready {
		count, err := s.sendOneReply(ctx, ws, pr.Number, owner, repo, thread)
		if err != nil {
			slog.Warn("could not send a reply", "thread", thread.ID, "err", err)
			failed = append(failed, map[string]string{"thread": thread.ID, "error": err.Error()})
			if errors.Is(err, errNotRecorded) {
				break // on GitHub, unrecorded: the rest would risk the same
			}
			continue
		}
		sent += count
		threads++
	}

	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"sent": sent, "threads": threads, "failed": failed, "pr": pr.Number,
	})
}

// onlyThreads is the threads the caller named. One it does not know is left
// out rather than refused: that reply may have gone in another tab.
func onlyThreads(list []comments.Thread, ids []string) []comments.Thread {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]comments.Thread, 0, len(ids))
	for _, thread := range list {
		if want[thread.ID] {
			out = append(out, thread)
		}
	}
	return out
}

// replyable is every thread with something waiting to go back to a reviewer.
func replyable(list []comments.Thread) []comments.Thread {
	out := make([]comments.Thread, 0, len(list))
	for _, thread := range list {
		if notReplyable(thread) == "" {
			out = append(out, thread)
		}
	}
	return out
}

// sendOneReply posts one thread's pending reply and records it as gone.
func (s *Server) sendOneReply(
	ctx context.Context, ws *workspace, number int, owner, repo string, thread comments.Thread,
) (int, error) {
	root, err := strconv.ParseInt(thread.ExternalID, 10, 64)
	if err != nil {
		return 0, errors.New("this thread has no upstream comment to reply to")
	}

	body, ids := replyBody(thread.PendingReply())
	if err := postReply(ctx, ws.homeRoot, owner, repo, number, root, body); err != nil {
		return 0, err
	}
	slog.Info("sent review reply", "number", number, "comment", root, "parts", len(ids))

	// It is on GitHub now. A store that did not record that would offer the
	// same reply again, so this is a failure even though the post succeeded.
	if err := ws.store.MarkReplySent(thread.ID, ids, time.Now()); err != nil {
		return 0, fmt.Errorf("%w: it is on #%d but ade could not record it: %w", errNotRecorded, number, err)
	}
	return len(ids), nil
}

// errNotRecorded is a reply GitHub took that the store did not record. Sending
// anything else risks the same, and the batch stops.
var errNotRecorded = errors.New("sent but not recorded")

// notReplyable says why a thread cannot be replied to, or "" when it can. A
// thread you resolved is one you decided against answering.
func notReplyable(thread comments.Thread) string {
	switch {
	case thread.ExternalID == "":
		return "this comment is not on the pull request, so there is nobody to reply to"
	case thread.Resolved:
		return "this thread is resolved"
	case thread.Orphaned:
		return "the line this was about has gone from the diff"
	case len(thread.PendingReply()) == 0:
		return "write a reply first, or draft one with the agent"
	}
	return ""
}

// replyBody joins a thread's pending comments into one reply, and reports which
// they were. Several become one comment on GitHub: separate notifications for
// consecutive things you said to the same person read as nagging.
func replyBody(pending []comments.Comment) (string, []string) {
	parts := make([]string, 0, len(pending))
	ids := make([]string, 0, len(pending))
	for _, c := range pending {
		parts = append(parts, strings.TrimSpace(c.Body))
		ids = append(ids, c.ID)
	}
	return strings.Join(parts, "\n\n"), ids
}

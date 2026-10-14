package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/reply"
)

// handleDraftReply drafts an answer to a review comment. The draft is a local
// comment on the thread: ade never posts to GitHub, so sending it stays a
// deliberate act of copying it across.
func (s *Server) handleDraftReply(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	thread, err := ws.store.Get(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindDraftReply)
	if !ok {
		s.queueBehind(w, queued{kind: kindDraftReply, thread: thread.ID})
		return
	}

	s.spawn(func() { s.runDraftReplies(runCtx, []comments.Thread{thread}) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

// repliesToDraft is what a queued draft-reply would answer when its turn comes,
// read again: a comment resolved while it waited is not one to answer.
func (s *Server) repliesToDraft(one string) ([]comments.Thread, bool) {
	if one == "" {
		threads := s.imported()
		return threads, len(threads) > 0
	}
	thread, err := s.space().store.Get(one)
	if err != nil || thread.Resolved {
		return nil, false
	}
	return []comments.Thread{thread}, true
}

// handleDraftReplies drafts answers to every open thread that came from the
// pull request.
func (s *Server) handleDraftReplies(w http.ResponseWriter, _ *http.Request) {
	threads := s.imported()
	if len(threads) == 0 {
		writeError(w, http.StatusBadRequest, "no open review comments to answer")
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindDraftReply)
	if !ok {
		s.queueBehind(w, queued{kind: kindDraftReply})
		return
	}

	s.spawn(func() { s.runDraftReplies(runCtx, threads) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "threads": len(threads)})
}

// imported returns the open threads that came from the pull request, which are
// the only ones there is anybody to reply to.
func (s *Server) imported() []comments.Thread {
	var out []comments.Thread
	for _, thread := range s.space().store.Unresolved() {
		if thread.ExternalID != "" {
			out = append(out, thread)
		}
	}
	return out
}

func (s *Server) runDraftReplies(ctx context.Context, threads []comments.Thread) {
	defer s.recoverRun("draft-reply")

	err := s.draftRepliesOnce(ctx, threads)
	s.agent.finish(err)
	if err != nil {
		slog.Warn("draft reply failed", "err", err)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) draftRepliesOnce(ctx context.Context, threads []comments.Thread) error {
	claude, err := s.agent.ensure(s.ctx, s.onAgentEvent)
	if err != nil {
		return err
	}

	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	ws := s.space()

	for _, thread := range threads {
		s.agent.step(fmt.Sprintf("drafting a reply for %s:%d", thread.File, thread.Line))

		turn, err := claude.Send(ctx, reply.Prompt(diff, thread))
		if err != nil {
			return err
		}
		draft := strings.TrimSpace(turn.Text)
		if draft == "" {
			s.agent.step(fmt.Sprintf("%s:%d — the agent had nothing to say", thread.File, thread.Line))
			continue
		}

		if _, err := ws.store.Reply(thread.ID, comments.AuthorDraft, draft); err != nil {
			slog.Debug("record drafted reply", "err", err)
			continue
		}
		s.hub.Broadcast("threads", nil)
	}
	return nil
}

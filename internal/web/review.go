package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/review"
)

// snapWindow is how far a reported finding may be nudged to reach a line that is
// actually in the diff. Beyond it the thread is kept but flagged orphaned.
const snapWindow = 12

// commenterAgent is the named agent that files another agent's review as
// comments on the diff. Any named agent that emits ade-finding blocks has
// them filed; this one exists to be handed a review and do only that.
const commenterAgent = "commenter"

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Level string `json:"level"`

		// Agents names the checkout's agents to review as, each a side run
		// whose review is prose in the conversation, beside whatever the
		// main agent is doing. Agent is the same for one. Neither means a
		// cold reviewer that files findings as comments.
		Agents []string `json:"agents"`
		Agent  string   `json:"agent"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(diff.Files) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to review: the diff is empty")
		return
	}
	names := reviewers(req.Agents, req.Agent)
	if len(names) == 0 {
		level := strings.TrimSpace(req.Level)
		runCtx, ok := s.agent.begin(s.ctx, kindReview)
		if !ok {
			s.queueBehind(w, queued{kind: kindReview, level: level})
			return
		}
		s.spawn(func() { s.runReview(runCtx, level) })
		s.hub.Broadcast("agent", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
		return
	}

	// User scope only: a repository agent shadows a user one, so a branch
	// under review could otherwise ship .claude/agents/security.md and
	// supply the reviewer that reviews it.
	list := s.userAgentsList()
	batch := s.newBatch(names)
	calls := make([]sideCall, 0, len(names))
	mentions := make([]string, 0, len(names))
	for _, name := range names {
		agent, ok := agents.Find(list, name)
		if !ok {
			s.dropBatch(batch)
			writeError(w, http.StatusBadRequest, "there is no agent called @"+name)
			return
		}
		// One that does not review would answer, and it would not be a review.
		if !agent.Reviews {
			s.dropBatch(batch)
			writeError(w, http.StatusBadRequest, "@"+name+" does not review changes: its definition says reviews: false")
			return
		}
		// Away from home the diff is another repository's, and a reviewer that
		// can write would be writing where no session is homed.
		if s.space().viewRoot != "" && writesCode(agentTools(agent)) {
			s.dropBatch(batch)
			writeError(w, http.StatusConflict, "@"+name+" can edit a checkout, so it does not review one this session is not in")
			return
		}
		calls = append(calls, sideCall{agent: agent, prompt: design.DiffReviewPrompt(name, s.reviewDocPath(), review.RenderDiff(diff)), batch: batch})
		mentions = append(mentions, "@"+name)
	}
	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf("%s review the diff (%d files)", strings.Join(mentions, " "), len(diff.Files))})
	// No slots held: the reviewers file their own findings, so there is no
	// @commenter hand-off to keep room for. A review costs one slot per
	// reviewer rather than two, which is what let three of them be refused
	// against a cap of four.
	started := s.startSides(calls)
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	if !started {
		s.dropBatch(batch)
		writeError(w, http.StatusConflict, fmt.Sprintf("no room for another agent; %d at most, and one is kept for a rewrite. Wait for one, or stop them", maxSides))
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "agents": names})
}

// commenterFor is the agent to hand a review to for filing, when the
// checkout defines one.
func commenterFor(list []agents.Agent) string {
	if _, ok := agents.Find(list, commenterAgent); ok {
		return commenterAgent
	}
	return ""
}

// reviewers is the names asked for, trimmed, without blanks or repeats, in
// the order given.
func reviewers(many []string, one string) []string {
	var out []string
	seen := map[string]bool{}
	for _, name := range append(many, one) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func (s *Server) runReview(ctx context.Context, level string) {
	defer s.recoverRun("review")

	found, err := s.reviewOnce(ctx, level)
	s.agent.finish(err)
	if err != nil {
		slog.Warn("review failed", "err", err)
	} else {
		s.agent.step(fmt.Sprintf("filed %d finding(s)", len(found)))
	}
	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after review", "err", refreshErr)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// reviewOnce runs a cold reviewer and files each finding as a thread. Findings
// are filed as the reviewer emits them, then reconciled against its final list.
func (s *Server) reviewOnce(ctx context.Context, level string) ([]review.Finding, error) {
	w := s.space()
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	s.agent.step("starting a cold review agent")

	streamer := review.NewStreamer()
	streamed := map[string]filed{}

	onEvent := func(ev bridge.Event) {
		s.onAgentEvent(ev)
		found := streamer.Push(ev.Text)
		if len(found) == 0 {
			return
		}
		record(streamed, s.fileFindings(review.Relative(found, w.diffRoot()), comments.AuthorAgent, s.agent.step))
		s.hub.Broadcast("state", nil)
	}

	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:          s.agent.bin,
		Model:        s.agent.model,
		Dir:          w.diffRoot(),
		Prompt:       review.Prompt(level, review.RenderDiff(diff)),
		SystemPrompt: s.elsewhereNote(),
		AllowedTools: review.ReadOnlyTools(),
		OnEvent:      onEvent,
	})
	if err != nil {
		return nil, err
	}

	findings, structured, err := review.ParseTurn(turn)
	if err != nil {
		return nil, err
	}
	return s.reconcile(review.Relative(findings, w.diffRoot()), streamed, structured), nil
}

// reconcile settles the streamed findings against the reviewer's final list:
// anything only in the final list is filed, anything it dropped is withdrawn.
// filed is a finding and the thread it became, so a retraction can find it again
// and a later step can be told what the review actually said.
type filed struct {
	id      string
	finding review.Finding

	// merged means this finding was folded into a thread another reviewer
	// already opened, so withdrawing it must not take that thread with it.
	merged bool
}

func record(into map[string]filed, added map[string]filed) {
	for key, entry := range added {
		into[key] = entry
	}
}

func (s *Server) reconcile(final []review.Finding, streamed map[string]filed, structured bool) []review.Finding {
	if !structured {
		// Without a final list there is nothing to reconcile against, so the
		// streamed findings stand rather than being thrown away.
		if len(streamed) == 0 {
			s.agent.step("the reviewer reported no findings")
		} else {
			s.agent.step("no final list in the reply; keeping the streamed findings")
		}
		return surviving(streamed)
	}

	kept := map[string]bool{}
	var missing []review.Finding
	for _, f := range final {
		key := review.Key(f)
		kept[key] = true
		if _, already := streamed[key]; !already {
			missing = append(missing, f)
		}
	}
	record(streamed, s.fileFindings(missing, comments.AuthorAgent, s.agent.step))

	for key, entry := range streamed {
		if kept[key] {
			continue
		}
		if entry.merged {
			delete(streamed, key)
			continue
		}
		if err := s.space().store.Delete(entry.id); err != nil {
			slog.Debug("withdraw retracted finding", "err", err)
			continue
		}
		delete(streamed, key)
		s.agent.step(fmt.Sprintf("withdrew %s — dropped from the reviewer's final list", key))
	}

	if len(streamed) == 0 {
		s.agent.step("the reviewer reported no findings")
	}
	return surviving(streamed)
}

// surviving lists what is still filed, in a stable order so two runs over the
// same review read the same way.
func surviving(streamed map[string]filed) []review.Finding {
	keys := make([]string, 0, len(streamed))
	for key := range streamed {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]review.Finding, 0, len(keys))
	for _, key := range keys {
		out = append(out, streamed[key].finding)
	}
	return out
}

// fileFindings turns findings into threads, keeping any that cannot be anchored
// rather than dropping a real finding on a technicality. It returns the thread
// id filed for each finding key, so one can be withdrawn later. author is
// who the comments are from — the cold reviewer, or a named agent — and
// note is told what happened to each.
func (s *Server) fileFindings(findings []review.Finding, author string, note func(string)) map[string]filed {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	out := make(map[string]filed, len(findings))
	for _, f := range findings {
		anchor, line, ok := comments.LocateNearest(diff, f.File, comments.SideRight, f.Line, snapWindow)
		thread := comments.Thread{
			File:     f.File,
			Side:     comments.SideRight,
			Line:     line,
			Anchor:   anchor,
			Category: f.Category,
			Severity: f.Severity,
			Orphaned: !ok,
			Comments: []comments.Comment{{Author: author, Body: review.Body(f)}},
		}
		added, merged, err := s.space().store.AddOrReply(thread)
		if err != nil {
			note(fmt.Sprintf("could not file %s:%d — %v", f.File, f.Line, err))
			continue
		}
		switch {
		case merged:
			note(fmt.Sprintf("%s:%d — %s, added to the comment already there", f.File, line, f.Severity))
		case ok:
			note(fmt.Sprintf("%s:%d — %s %s", f.File, line, f.Severity, f.Category))
		default:
			note(fmt.Sprintf("%s:%d — not in the diff, filed as orphaned", f.File, f.Line))
		}
		out[review.Key(f)] = filed{id: added.ID, finding: f, merged: merged}
	}
	return out
}

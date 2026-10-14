package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/verify"
)

// VerifyState is the last check run, for the editor's badge. Absent until a
// list has been run once in this session.
type VerifyState struct {
	Running bool `json:"running,omitempty"`
	Passed  bool `json:"passed,omitempty"`
	Stopped bool `json:"stopped,omitempty"`
	// Failed is the check that stopped the run, when one did.
	Failed string `json:"failed,omitempty"`
	// Ran and Total are how far it got: 2 of 3 means the second failed.
	Ran   int       `json:"ran"`
	Total int       `json:"total"`
	At    time.Time `json:"at,omitempty"`
	// Narrow says the numbers are the narrow pass's, not the whole list's.
	Narrow bool `json:"narrow,omitempty"`
	// Offer is a detected list waiting on your yes, for a repository that has
	// none. Empty once the offer has been answered either way.
	Offer []verify.Check `json:"offer,omitempty"`
}

// verifyPath is where this repository's check list lives.
func (s *Server) verifyPath() string { return verify.Path(s.commonGitDir) }

// verifyAfterTurn runs the checks when a turn changed something. before is the
// diff hash as it was when the turn started: unchanged means nothing to check.
func (s *Server) verifyAfterTurn(before string) {
	if before == s.currentHash() {
		return
	}
	config, err := verify.Load(s.verifyPath())
	if err != nil {
		slog.Debug("read the check list", "err", err)
		return
	}
	if len(config.Checks) == 0 {
		s.offerChecks(config)
		return
	}
	s.runChecks(config.Checks, s.changedPaths())
}

// changedPaths is what the diff says moved, for the narrow pass. Nothing while
// another root is on view: those paths are not in the checkout the checks run in.
func (s *Server) changedPaths() []string {
	if s.space().viewRoot != "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.diff == nil {
		return nil
	}
	paths := make([]string, 0, len(s.diff.Files))
	for _, file := range s.diff.Files {
		paths = append(paths, file.Path)
	}
	return paths
}

// offerChecks proposes a detected list, once, to a repository that has none.
// Declining is remembered by the same flag that records the asking.
func (s *Server) offerChecks(config verify.Config) {
	if config.Asked {
		return
	}
	found := verify.Detect(s.space().homeRoot)
	if len(found) == 0 {
		if err := verify.Save(s.verifyPath(), verify.Config{Asked: true}); err != nil {
			slog.Debug("remember that there is nothing to detect", "err", err)
		}
		return
	}
	s.mu.Lock()
	s.verifyOffer = found
	s.mu.Unlock()
	s.hub.Broadcast("state", nil)
}

// runChecks works through a list, with the last run's own cancel pulled first:
// the tree has moved, so what that one is checking no longer exists.
// changed is what the narrow pass is scoped to; none means the full list only.
func (s *Server) runChecks(checks []verify.Check, changed []string) {
	ctx, cancel := context.WithCancel(s.ctx)
	// Read before the lock: space takes the same one.
	root := s.space().homeRoot
	s.mu.Lock()
	if s.verifyCancel != nil {
		s.verifyCancel()
	}
	s.verifyGen++
	mine := s.verifyGen
	s.verifyCancel, s.verifyRunning, s.verifyLast = cancel, true, nil
	s.mu.Unlock()
	defer cancel()

	s.hub.Broadcast("state", nil)
	if narrow := verify.Narrowed(root, checks, changed); len(narrow) > 0 {
		out := verify.Run(ctx, root, narrow)
		out.Narrow = true
		if !s.landChecks(mine, out) || !out.NarrowPassed() {
			return
		}
	}
	s.landChecks(mine, verify.Run(ctx, root, checks))
}

// landChecks records one pass and says so. False means a later run took over:
// its answer is the one about the tree as it is now, and this one says nothing.
func (s *Server) landChecks(mine int, out verify.Outcome) bool {
	s.mu.Lock()
	stale := s.verifyGen != mine
	if !stale {
		s.verifyRunning, s.verifyLast = out.NarrowPassed(), &out
	}
	s.mu.Unlock()
	if stale {
		return false
	}
	s.sayVerdict(out)
	s.hub.Broadcast("state", nil)
	return true
}

// sayVerdict writes the result into the conversation, and hands a failure to
// the agent for the next thing you say.
func (s *Server) sayVerdict(out verify.Outcome) {
	if out.Stopped {
		s.say(ChatMessage{Kind: chat.KindNote, Body: "checks stopped"})
		return
	}
	if out.NarrowPassed() {
		s.say(ChatMessage{Kind: chat.KindNote,
			Body: fmt.Sprintf("narrow checks passed (%d), running the full list", out.Total)})
		return
	}
	if out.Passed() {
		s.say(ChatMessage{Kind: chat.KindNote,
			Body: fmt.Sprintf("checks passed (%d)", out.Total)})
		return
	}
	if out.Failed == nil {
		return
	}
	s.say(ChatMessage{
		Kind:   chat.KindNote,
		Failed: true,
		Body:   verdictLine(*out.Failed),
		Tool:   out.Failed.Name,
		Output: out.Failed.Output,
	})
	s.tellAgentOfFailure(*out.Failed)
}

func verdictLine(failed verify.Result) string {
	if failed.TimedOut {
		return failed.Name + " timed out"
	}
	return failed.Name + " failed"
}

// tellAgentOfFailure rides on your next message: the agent that broke the
// build does not otherwise know it did.
func (s *Server) tellAgentOfFailure(failed verify.Result) {
	what := "failed"
	if failed.TimedOut {
		what = "timed out"
	}
	text := fmt.Sprintf("ade ran the checks after your last turn and %s %s.\n\n"+
		"    $ %s\n\n%s\n\nFix the cause before anything else, and say so if it was already broken.",
		failed.Name, what, failed.Run, failed.Output)
	s.mu.Lock()
	s.heard = append(s.heard, design.Heard{Who: "ade", Text: text})
	s.mu.Unlock()
}

// stopChecks ends a check list that is running. Stop is one button: the turn
// and what ade started off the back of it both go.
func (s *Server) stopChecks() bool {
	s.mu.Lock()
	cancel, running := s.verifyCancel, s.verifyRunning
	s.mu.Unlock()
	if !running || cancel == nil {
		return false
	}
	cancel()
	return true
}

// verifyState is the badge: what the last run said, or what is being offered.
func (s *Server) verifyState() *VerifyState {
	s.mu.RLock()
	last, running, offer := s.verifyLast, s.verifyRunning, s.verifyOffer
	s.mu.RUnlock()
	if last == nil && !running && len(offer) == 0 {
		return nil
	}
	state := &VerifyState{Running: running, Offer: offer}
	if last != nil {
		state.Passed, state.Stopped = last.Passed(), last.Stopped
		state.Ran, state.Total, state.At = len(last.Ran), last.Total, last.At
		state.Narrow = last.Narrow
		if last.Failed != nil {
			state.Failed = last.Failed.Name
		}
	}
	return state
}

// handleVerify is the check list, and running it on demand.
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	config, err := verify.Load(s.verifyPath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if config.Checks == nil {
		config.Checks = []verify.Check{}
	}
	detected := verify.Detect(s.space().homeRoot)
	s.mu.RLock()
	offer := s.verifyOffer
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"checks": config.Checks, "asked": config.Asked,
		"offer": offer, "detected": detected,
	})
}

// handleVerifyRun runs the list now, for when you want it without a turn.
func (s *Server) handleVerifyRun(w http.ResponseWriter, _ *http.Request) {
	config, err := verify.Load(s.verifyPath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(config.Checks) == 0 {
		writeError(w, http.StatusConflict, "no checks are configured for this repository")
		return
	}
	s.mu.RLock()
	running := s.verifyRunning
	s.mu.RUnlock()
	if running {
		writeError(w, http.StatusConflict, "the checks are already running")
		return
	}
	s.spawn(func() { s.runChecks(config.Checks, nil) })
	w.WriteHeader(http.StatusAccepted)
}

// handleVerifySave writes the list, and answers the offer either way: an
// empty list saved against an offer is you declining it.
func (s *Server) handleVerifySave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Checks []verify.Check `json:"checks"`
	}
	if !decode(w, r, &req) {
		return
	}
	for i, check := range req.Checks {
		req.Checks[i].Name = strings.TrimSpace(check.Name)
		req.Checks[i].Run = strings.TrimSpace(check.Run)
		req.Checks[i].Dir = strings.TrimSpace(check.Dir)
		req.Checks[i].Narrow = strings.TrimSpace(check.Narrow)
	}
	if err := verify.Save(s.verifyPath(), verify.Config{Checks: req.Checks, Asked: true}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	s.verifyOffer = nil
	s.mu.Unlock()
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"checks": req.Checks})
}

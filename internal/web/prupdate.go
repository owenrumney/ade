package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/ghpr"
)

// handleEditPR saves a new title and description on the pull request under
// review. This and the merge are the only two things ade changes upstream, and
// both are about your own change rather than anything said to a reviewer.
func (s *Server) handleEditPR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}

	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to edit")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "a pull request needs a title")
		return
	}
	if !strings.EqualFold(pr.State, "OPEN") {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("#%d is %s, so it can no longer be edited", pr.Number, strings.ToLower(pr.State)))
		return
	}

	if err := ghpr.Edit(r.Context(), s.space().homeRoot, pr.Number, title, req.Body); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.refreshBranchPR(s.ctx)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"pr": s.currentBranchPR()})
}

// handleMergePR merges the pull request under review.
func (s *Server) handleMergePR(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string `json:"method"`
	}
	if !decode(w, r, &req) {
		return
	}
	method := req.Method
	if method == "" {
		method = ghpr.MergeRebase
	}
	if method != ghpr.MergeRebase && method != ghpr.MergeCommit {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown merge method %q", req.Method))
		return
	}

	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to merge")
		return
	}

	// What is already known to be in the way is answered without a round trip.
	if pr.MergeBlocked != "" {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("#%d cannot be merged: %s", pr.Number, pr.MergeBlocked))
		return
	}

	// The Review tab's copy can be half a minute old. Merging is the one thing
	// here that cannot be taken back, so ask GitHub again rather than acting on
	// what was true last time anyone looked.
	root := s.space().homeRoot
	fresh, err := ghpr.Fetch(r.Context(), root, pr.Number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if blocked := fresh.MergeBlocked(); blocked != "" {
		s.refreshBranchPR(s.ctx)
		s.hub.Broadcast("state", nil)
		writeError(w, http.StatusConflict, fmt.Sprintf("#%d cannot be merged: %s", pr.Number, blocked))
		return
	}

	if err := ghpr.Merge(r.Context(), root, pr.Number, method); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	slog.Info("merged pull request", "number", pr.Number, "method", method)

	s.refreshBranchPR(s.ctx)
	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after merge", "err", refreshErr)
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"merged": pr.Number, "method": method})
}

// who is your GitHub login, worked out once. It decides whether the pull
// request in front of you is your own, which GitHub will not let you approve.
func (s *Server) who(ctx context.Context, dir string) string {
	s.mu.RLock()
	known, asked := s.viewer, s.viewerAsked
	s.mu.RUnlock()
	if asked {
		return known
	}

	login := ghpr.Viewer(ctx, dir)
	s.mu.Lock()
	s.viewer, s.viewerAsked = login, true
	s.mu.Unlock()
	return login
}

// verdict is one of the two reviews a button sends. word names it in errors
// and in the log, cached and blocked read the reason it cannot be sent from
// the cached and the freshly-fetched pull request, and send posts it.
type verdict struct {
	word    string
	cached  func(*BranchPR) string
	blocked func(*ghpr.PR, string) string
	send    func(context.Context, string, int, string) error
	// event is this verdict as GitHub names it, for finishing a draft review
	// rather than starting a second one beside it.
	event  string
	result string
	// bus is this verdict as agentbus names it, for a review the team asked
	// for here rather than GitHub.
	bus string
}

// giveVerdict finishes the draft review when there is one. `gh pr review`
// would start a second, stranding the draft and posting an empty one.
func (s *Server) giveVerdict(
	ctx context.Context,
	root string,
	number int,
	body string,
	v verdict,
) error {
	owner, repo, err := repoOf(ctx, root)
	if err != nil {
		return v.send(ctx, root, number, body)
	}
	draft, err := pendingReview(ctx, root, owner, repo, number)
	if err != nil || draft.ID == "" {
		return v.send(ctx, root, number, body)
	}
	if err := submitDraft(ctx, root, draft.ID, v.event, body); err != nil {
		return err
	}
	return s.markDraftSent(ctx, number, draft.ID)
}

// markDraftSent records that everything in the draft is now on the pull
// request for the author to read.
func (s *Server) markDraftSent(ctx context.Context, number int, reviewID string) error {
	ws := s.space()
	sent := ws.store.InDraft(reviewID)
	ids := make([]string, 0, len(sent))
	for _, thread := range sent {
		ids = append(ids, thread.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	if err := ws.store.MarkSent(ids, time.Now()); err != nil {
		return err
	}
	s.stampSentExternalIDs(ctx, ws, number, sent)
	s.hub.Broadcast("threads", nil)
	return nil
}

// handleApprovePR records your approval of the pull request under review.
func (s *Server) handleApprovePR(w http.ResponseWriter, r *http.Request) {
	s.review(w, r, verdict{
		word:    "approved",
		cached:  func(pr *BranchPR) string { return pr.ApproveBlocked },
		blocked: (*ghpr.PR).ApproveBlocked,
		send:    ghpr.Approve,
		event:   ghpr.EventApprove,
		result:  "approved",
		bus:     agentbus.ReviewApproved,
	})
}

// handleRequestChangesPR marks the pull request as changes requested. The body
// is required here: GitHub will not take the verdict without one.
func (s *Server) handleRequestChangesPR(w http.ResponseWriter, r *http.Request) {
	s.review(w, r, verdict{
		word:    "sent back",
		cached:  func(pr *BranchPR) string { return pr.RequestChangesBlocked },
		blocked: (*ghpr.PR).RequestChangesBlocked,
		send:    ghpr.RequestChanges,
		event:   ghpr.EventRequestChanges,
		result:  "changesRequested",
		bus:     agentbus.ReviewChangesRequested,
	})
}

// review posts one verdict on the pull request under review.
//
// This is the only place ade speaks to a reviewer, and it speaks as you: it is
// reached by pressing a button, the body is your own words, and nothing the
// agent runs can call it.
func (s *Server) review(w http.ResponseWriter, r *http.Request, v verdict) {
	var req struct {
		Body string `json:"body"`
		// Confirm must name the pull request being reviewed. A verdict is
		// public and posted as you, so it should not be something a bare POST
		// to this path can do to whatever happens to be open.
		Confirm int `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}

	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to review")
		return
	}
	if req.Confirm != pr.Number {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("reviewing #%d has to name it: send confirm=%d", pr.Number, pr.Number))
		return
	}
	if why := v.cached(pr); why != "" {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("#%d cannot be %s: %s", pr.Number, v.word, why))
		return
	}

	// Asked again before acting: a verdict is public and cannot be taken back
	// quietly, and the cached copy can be half a minute old.
	root := s.space().homeRoot
	fresh, err := ghpr.Fetch(r.Context(), root, pr.Number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if why := v.blocked(fresh, s.who(r.Context(), root)); why != "" {
		s.refreshBranchPR(s.ctx)
		s.hub.Broadcast("state", nil)
		writeError(w, http.StatusConflict,
			fmt.Sprintf("#%d cannot be %s: %s", pr.Number, v.word, why))
		return
	}

	if err := s.giveVerdict(r.Context(), root, pr.Number, req.Body, v); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	slog.Info("reviewed pull request", "number", pr.Number, "verdict", v.word)
	s.busReviewed(r.Context(), s.project(), pr.Number, v.bus, req.Body, pr.URL)

	s.refreshBranchPR(s.ctx)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{v.result: pr.Number})
}

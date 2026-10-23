package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// The review ade builds is GitHub's own pending review: comments join it one
// at a time, and one verdict finishes it. See the design doc.

// Seams, so the tests have no GitHub to reach.
var (
	pendingReview   = ghpr.PendingReview
	pullRequestID   = ghpr.PullRequestID
	startDraft      = ghpr.StartDraftReview
	addToDraft      = ghpr.AddToDraftReview
	removeFromDraft = ghpr.RemoveFromDraftReview
	submitDraft     = ghpr.SubmitDraftReview
	prDiffText      = ghpr.Diff
)

// DraftState is what the Review place draws: how much is waiting on GitHub,
// how much could still join it, and what is going nowhere.
type DraftState struct {
	PR       int    `json:"pr,omitempty"`
	Review   string `json:"review,omitempty"`
	InDraft  int    `json:"inDraft"`
	Ready    int    `json:"ready"`
	NotGoing int    `json:"notGoing"`
}

// handleDraftState answers the counts without touching GitHub.
func (s *Server) handleDraftState(w http.ResponseWriter, _ *http.Request) {
	ws := s.space()
	state := DraftState{}
	if pr := s.currentBranchPR(); pr != nil {
		state.PR = pr.Number
	}
	for _, thread := range ws.store.InDraft("") {
		state.InDraft++
		state.Review = thread.DraftReview
	}
	for _, thread := range ws.store.Sendable() {
		if thread.Drafted() {
			continue
		}
		if thread.Outgoing == "" {
			state.NotGoing++
			continue
		}
		state.Ready++
	}
	writeJSON(w, http.StatusOK, state)
}

// handleAddToDraft puts converted comments into the pull request's pending
// review, starting one if there is none. `thread` adds just that one.
func (s *Server) handleAddToDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm int    `json:"confirm"`
		Thread  string `json:"thread"`
	}
	if !decode(w, r, &req) {
		return
	}
	ws := s.space()
	ctx := context.WithoutCancel(r.Context())
	pr := s.currentBranchPR()
	if pr == nil {
		writeError(w, http.StatusBadRequest, "there is no pull request to comment on")
		return
	}
	if req.Confirm != pr.Number {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("drafting on #%d has to name it: send confirm=%d", pr.Number, pr.Number))
		return
	}

	// Every comment goes on a line of the pull request's diff, so a checkout
	// that is not at its head can only produce comments GitHub throws away.
	if why := behindThePR(ctx, ws, pr); why != "" {
		writeError(w, http.StatusConflict, why)
		return
	}

	wanted := readyToSend(ws.store.Sendable())
	if req.Thread != "" {
		wanted = onlyThread(wanted, req.Thread)
		if len(wanted) == 0 {
			writeError(w, http.StatusBadRequest, whySendRefused(ws.store, req.Thread))
			return
		}
	}
	wanted = notYetDrafted(wanted)
	if len(wanted) == 0 {
		writeError(w, http.StatusBadRequest, "nothing new to add: convert a finding first")
		return
	}

	owner, repo, err := repoOf(ctx, ws.homeRoot)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	added, refused, err := s.draftAll(ctx, ws, owner, repo, pr.Number, wanted)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"drafted": added, "refused": refused, "pr": pr.Number,
	})
}

// draftAll adds each comment to the pending review, opening one if GitHub has
// none. Re-read first: the draft may have gone from the GitHub UI since.
func (s *Server) draftAll(
	ctx context.Context,
	ws *workspace,
	owner, repo string,
	number int,
	wanted []comments.Thread,
) (int, []string, error) {
	draft, err := pendingReview(ctx, ws.homeRoot, owner, repo, number)
	if err != nil {
		return 0, nil, err
	}
	if draft.ID == "" {
		if err := ws.store.ForgetDraft("", ""); err != nil {
			slog.Debug("forget a draft that has gone", "err", err)
		}
	}

	// The lines GitHub will take are the ones in its own diff of the pull
	// request, which is not always the one this checkout can see.
	takes := prLines(ctx, ws.homeRoot, number)

	// A comment GitHub will not take does not stop the rest: it is named in
	// the refusals and left where it was, which is the only true state.
	added := 0
	refused := []string{}
	for _, thread := range wanted {
		if takes != nil && !takes(thread.File, thread.Side, thread.Line) {
			refused = append(refused, fmt.Sprintf(
				"%s:%d is not a line #%d's diff touches, so GitHub will not take a comment there",
				thread.File, thread.Line, number))
			continue
		}
		comment := ghpr.InlineComment{
			Path: thread.File, Line: thread.Line, Side: thread.Side,
			Body: comments.Badged(thread.Severity, thread.Outgoing),
		}
		var commentID string
		var failed error
		if draft.ID == "" {
			prID, err := pullRequestID(ctx, ws.homeRoot, owner, repo, number)
			if err != nil {
				return added, refused, err
			}
			var reviewID string
			reviewID, commentID, failed = startDraft(ctx, ws.homeRoot, prID, comment)
			// The review opens even when the comment in it is refused, and
			// the next comment has to join that one, not open a second.
			if reviewID != "" {
				draft.ID = reviewID
			}
		} else {
			commentID, failed = addToDraft(ctx, ws.homeRoot, draft.ID, comment)
		}
		if failed != nil {
			slog.Warn("a comment did not join the review", "file", thread.File, "line", thread.Line, "err", failed)
			refused = append(refused, failed.Error())
			continue
		}
		if err := ws.store.MarkDrafted(thread.ID, draft.ID, commentID); err != nil {
			return added, refused, err
		}
		added++
	}
	return added, refused, nil
}

// prLines reports whether a line is one the pull request's diff touches, read
// from GitHub rather than from here. Nil when it could not be read.
func prLines(ctx context.Context, dir string, number int) func(string, string, int) bool {
	text, err := prDiffText(ctx, dir, number)
	if err != nil {
		slog.Debug("read the pull request's diff", "pr", number, "err", err)
		return nil
	}
	diff := gitdiff.FromText("base", "head", text)
	return func(file, side string, line int) bool {
		_, ok := comments.Locate(diff, file, side, line)
		return ok
	}
}

// behindThePR is why this checkout cannot be commented from: it is not the
// code the pull request is showing. Measured on infracost/coast#1248.
func behindThePR(ctx context.Context, ws *workspace, pr *BranchPR) string {
	if pr.HeadSHA == "" {
		return ""
	}
	head, err := gitdiff.HeadSHA(ctx, ws.homeRoot)
	if err != nil || head == pr.HeadSHA {
		return ""
	}
	// Your own branch with commits you have not pushed is still the right
	// code to comment on, which is the rule Resolve already follows.
	if branch := gitdiff.Branch(ctx, ws.homeRoot); branch != "" && branch == pr.HeadRef {
		return ""
	}
	return fmt.Sprintf(
		"this checkout is at %s and #%d is at %s, so GitHub would take none of these comments; check it out again with gh pr checkout %d",
		short(head), pr.Number, short(pr.HeadSHA), pr.Number)
}

// handleTakeOutOfDraft removes one comment from the pending review.
func (s *Server) handleTakeOutOfDraft(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	thread, err := ws.store.Get(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !thread.Drafted() {
		writeError(w, http.StatusBadRequest, "this comment is not waiting in a draft review")
		return
	}
	// Forgetting it here would leave it on the review, where the author
	// will read it after the verdict and ade will not have listed it.
	if thread.DraftThread == "" {
		writeError(w, http.StatusConflict,
			"GitHub did not name this comment when it went in, so ade cannot take it out; remove it in the review on GitHub")
		return
	}
	ctx := context.WithoutCancel(r.Context())
	if err := removeFromDraft(ctx, ws.homeRoot, thread.DraftThread); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := ws.store.ForgetDraft(thread.ID, ""); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("threads", nil)
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread.ID})
}

// handleFinishReview submits the pending review with a verdict, which is the
// moment the author can see any of it.
func (s *Server) handleFinishReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm int    `json:"confirm"`
		Event   string `json:"event"`
		Body    string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	event := strings.ToUpper(strings.TrimSpace(req.Event))
	switch event {
	case ghpr.EventApprove, ghpr.EventRequestChanges, ghpr.EventComment:
	default:
		writeError(w, http.StatusBadRequest, "finish with approve, request-changes or comment")
		return
	}

	ws := s.space()
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
	if why := blockedVerdict(pr, event); why != "" {
		writeError(w, http.StatusConflict, fmt.Sprintf("#%d cannot be reviewed: %s", pr.Number, why))
		return
	}

	ctx := context.WithoutCancel(r.Context())
	owner, repo, err := repoOf(ctx, ws.homeRoot)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	draft, err := pendingReview(ctx, ws.homeRoot, owner, repo, pr.Number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if draft.ID == "" {
		writeError(w, http.StatusBadRequest, "there is no draft review to finish")
		return
	}
	if err := submitDraft(ctx, ws.homeRoot, draft.ID, event, req.Body); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Marked before anything else can fail: the author can see them now.
	sent := ws.store.InDraft(draft.ID)
	ids := make([]string, 0, len(sent))
	for _, thread := range sent {
		ids = append(ids, thread.ID)
	}
	if err := ws.store.MarkSent(ids, time.Now()); err != nil {
		slog.Warn("could not record what was sent", "err", err)
	}
	s.stampSentExternalIDs(ctx, ws, pr.Number, sent)

	s.refreshBranchPR(s.ctx)
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"sent": len(ids), "event": event, "pr": pr.Number})
}

// blockedVerdict is the server's own reason a verdict cannot be given, which
// the pull request already knows.
func blockedVerdict(pr *BranchPR, event string) string {
	switch event {
	case ghpr.EventApprove:
		return pr.ApproveBlocked
	case ghpr.EventRequestChanges:
		return pr.RequestChangesBlocked
	}
	return ""
}

func notYetDrafted(all []comments.Thread) []comments.Thread {
	var out []comments.Thread
	for _, thread := range all {
		if !thread.Drafted() {
			out = append(out, thread)
		}
	}
	return out
}

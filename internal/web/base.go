package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
)

const (
	// rebaseBudget bounds the replay, and abortBudget the undo after one that
	// ran out: an abort on the dead budget would leave it half done.
	rebaseBudget = 2 * time.Minute
	abortBudget  = 30 * time.Second
)

// handlePullBase fetches origin's base branch and reloads the diff against
// it. A stale origin/main puts every commit main has had since the fork
// into the diff as if the branch made them; GitHub compares against main as
// it is now, and after this so does the page.
func (s *Server) handlePullBase(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot; its base is GitHub's already")
		return
	}
	fetched, err := s.pullBase(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"base": fetched})
}

// pullBase fetches origin's base branch and reloads the diff against it.
func (s *Server) pullBase(ctx context.Context) (string, error) {
	ws := s.space()
	fetched, err := gitdiff.FetchBase(ctx, ws.diffRoot(), ws.opts.Base)
	if err != nil {
		return "", err
	}
	if err := s.refresh(s.ctx); err != nil {
		slog.Debug("refresh after pulling the base", "err", err)
	}
	s.hub.Broadcast("state", nil)
	return fetched, nil
}

// handlePullBranch fetches origin's copy of the checked-out branch and
// fast-forwards onto it, so a reopened review reads what was pushed since.
func (s *Server) handlePullBranch(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot; there is no checkout to pull into")
		return
	}
	dir := ws.diffRoot()
	branch := gitdiff.Branch(r.Context(), dir)
	if branch == "detached" {
		writeError(w, http.StatusConflict, "no branch is checked out here to pull")
		return
	}
	fetched, err := gitdiff.FetchBranch(r.Context(), dir, branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The base comes too, or the diff is cut against a main the branch was
	// rebased past and shows main's commits as the branch's own.
	if _, err := gitdiff.FetchBase(r.Context(), dir, ws.opts.Base); err != nil {
		slog.Debug("fetch the base with the branch", "err", err)
	}
	behind, err := gitdiff.CommitsBehind(r.Context(), dir, fetched)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if behind > 0 {
		if err := gitdiff.FastForward(r.Context(), dir, fetched); err != nil {
			writeError(w, http.StatusConflict,
				fmt.Sprintf("%s cannot be fast-forwarded onto this checkout; it was rewritten or has local commits: %v", fetched, err))
			return
		}
	}
	s.afterRebase()
	writeJSON(w, http.StatusOK, map[string]any{"branch": fetched, "pulled": behind})
}

// handleRebaseBase replays this branch on top of a freshly fetched base. The
// guards are worktrees.syncOne's: a rebase over uncommitted work loses it.
func (s *Server) handleRebaseBase(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot; there is nothing here to rebase")
		return
	}
	dir := ws.diffRoot()
	status, err := gitdiff.WorktreeStatus(r.Context(), dir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n := len(status.Staged) + len(status.Unstaged); n > 0 {
		writeError(w, http.StatusConflict,
			fmt.Sprintf("commit or stash %d changed file(s) first; a rebase over them loses them", n))
		return
	}

	base, err := s.pullBase(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	behind, err := gitdiff.CommitsBehind(r.Context(), dir, base)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if behind == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"base": base, "behind": 0, "replayed": 0})
		return
	}
	replayed, err := gitdiff.CommitsAhead(r.Context(), dir, base)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Uncancellable, and its own budget for the abort: a closed tab between a
	// rebase and its abort leaves the checkout mid-rebase.
	ctx, stop := context.WithTimeout(context.WithoutCancel(r.Context()), rebaseBudget)
	defer stop()
	if err := gitdiff.Rebase(ctx, dir, base); err != nil {
		why := fmt.Sprintf("rebase onto %s hit a conflict and was undone; do it by hand", base)
		abortCtx, done := context.WithTimeout(context.WithoutCancel(r.Context()), abortBudget)
		defer done()
		if abort := gitdiff.RebaseAbort(abortCtx, dir); abort != nil {
			why = fmt.Sprintf("rebase onto %s failed and could not be undone: %v", base, abort)
		}
		s.afterRebase()
		writeError(w, http.StatusConflict, why)
		return
	}
	s.afterRebase()
	writeJSON(w, http.StatusOK, map[string]any{"base": base, "behind": behind, "replayed": replayed})
}

// afterRebase reloads what the rebase moved: the diff, and the commit panel's
// view of how far ahead and behind the branch now is.
func (s *Server) afterRebase() {
	if err := s.refresh(s.ctx); err != nil {
		slog.Debug("refresh after the rebase", "err", err)
	}
	s.refreshWorktree(s.ctx)
	s.hub.Broadcast("state", nil)
}

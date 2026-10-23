package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
)

// Catching up is the one shot: stash, replay on a fresh base, pop, and settle
// what conflicts. Which failures are undone and which are left is in the doc.

const (
	// catchUpBudget covers the whole of it: a rebase that stops on every one
	// of six commits is six model calls, each on the fast model.
	catchUpBudget = 10 * time.Minute
	// stops bounds the loop. A rebase stops at most once per replayed commit;
	// more than that and something other than conflicts is happening.
	maxStops = 50
)

// CaughtUp is what the one shot did, in the order it did it.
type CaughtUp struct {
	Base     string   `json:"base"`
	Behind   int      `json:"behind"`
	Replayed int      `json:"replayed"`
	Stashed  bool     `json:"stashed"`
	Resolved []string `json:"resolved,omitempty"`
	By       string   `json:"by,omitempty"`
	// Stuck is what came back from the stash conflicted and could not be
	// settled. The rebase stands; these are in the tree with markers.
	Stuck []string `json:"stuck,omitempty"`
	// Stale is why the base could not be fetched, when it could not be.
	Stale string `json:"stale,omitempty"`
}

// handleCatchUp replays the branch on a fresh base over the top of whatever
// is uncommitted, settling what conflicts.
func (s *Server) handleCatchUp(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot; there is nothing here to rebase")
		return
	}
	dir := ws.diffRoot()

	// A fetch that fails leaves the base as stale as it was, which is worth
	// saying and not worth refusing: a local base is still one to replay on.
	base, err := s.pullBase(r.Context())
	stale := ""
	if err != nil {
		stale, base = err.Error(), ws.opts.Base
		if base == "" {
			base = gitdiff.BaseBranch(r.Context(), dir)
		}
	}
	if base == "" {
		writeError(w, http.StatusBadRequest, "this checkout has no base branch to rebase onto")
		return
	}
	behind, err := gitdiff.CommitsBehind(r.Context(), dir, base)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if behind == 0 {
		writeJSON(w, http.StatusOK, CaughtUp{Base: base, Behind: 0, Stale: stale})
		return
	}
	replayed, err := gitdiff.CommitsAhead(r.Context(), dir, base)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Uncancellable: a closed tab mid-rebase leaves the checkout in one.
	ctx, stop := context.WithTimeout(context.WithoutCancel(r.Context()), catchUpBudget)
	defer stop()

	got, err := s.catchUp(ctx, dir, base)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	got.Base, got.Behind, got.Replayed, got.Stale = base, behind, replayed, stale
	s.afterRebase()
	writeJSON(w, http.StatusOK, got)
}

// catchUp is the whole shot. The rebase is all or nothing; the stash coming
// back is not, because by then the replay is committed.
func (s *Server) catchUp(ctx context.Context, dir, base string) (CaughtUp, error) {
	out := CaughtUp{}
	stashed, err := gitdiff.Stash(ctx, dir, "ade: catching up with "+base)
	if err != nil {
		return out, err
	}
	out.Stashed = stashed

	settled, err := s.replay(ctx, dir, base)
	out.Resolved = append(out.Resolved, settled...)
	if err != nil {
		return out, s.putBack(ctx, dir, stashed, err)
	}
	if !stashed {
		out.By = byWhom(out.Resolved)
		return out, nil
	}

	stuck, err := gitdiff.StashPop(ctx, dir)
	if err != nil {
		return out, fmt.Errorf("the rebase landed, but your work did not come back: %w", err)
	}
	if len(stuck) > 0 {
		// The replay is committed. Undoing it to tidy these away would throw
		// away good work, and the stash still holds them, so they stay.
		if err := s.resolveConflicts(ctx, dir, stuck); err != nil {
			out.Stuck = stuck
			out.By = byWhom(out.Resolved)
			return out, nil
		}
		if err := gitdiff.StashDrop(ctx, dir); err != nil {
			slog.Debug("drop the stash after a resolved pop", "err", err)
		}
		out.Resolved = append(out.Resolved, stuck...)
	}
	out.By = byWhom(out.Resolved)
	return out, nil
}

// replay rebases, settling the conflicts of each commit as it stops.
func (s *Server) replay(ctx context.Context, dir, base string) ([]string, error) {
	var resolved []string
	stuck, err := gitdiff.StartRebase(ctx, dir, base)
	if err != nil {
		return resolved, err
	}
	for n := 0; len(stuck) > 0; n++ {
		if n >= maxStops {
			return resolved, fmt.Errorf("the rebase onto %s kept stopping; do it by hand", base)
		}
		if err := s.resolveConflicts(ctx, dir, stuck); err != nil {
			return resolved, err
		}
		if err := gitdiff.StageAll(ctx, dir, stuck); err != nil {
			return resolved, err
		}
		resolved = append(resolved, stuck...)
		if stuck, err = gitdiff.RebaseContinue(ctx, dir); err != nil {
			return resolved, err
		}
	}
	return resolved, nil
}

// putBack undoes a rebase that could not be settled and returns the work to
// the tree, so a refusal leaves the checkout where it was found.
func (s *Server) putBack(ctx context.Context, dir string, stashed bool, why error) error {
	undo, done := context.WithTimeout(context.WithoutCancel(ctx), abortBudget)
	defer done()
	if err := gitdiff.RebaseAbort(undo, dir); err != nil {
		return fmt.Errorf("%v, and the rebase could not be undone: %w", why, err)
	}
	if stashed {
		if _, err := gitdiff.StashPop(undo, dir); err != nil {
			return fmt.Errorf("%v; the rebase was undone but your work is still stashed: %w", why, err)
		}
	}
	s.afterRebase()
	return fmt.Errorf("%v; nothing was rebased and your work is back", why)
}

func byWhom(resolved []string) string {
	if len(resolved) == 0 {
		return ""
	}
	return outgoing.FastModel
}

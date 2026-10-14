package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/highlight"
	"github.com/owenrumney/ade/internal/words"
)

// handleListPRs lists the open pull requests on this repository, so one can be
// picked to review without leaving the browser.
func (s *Server) handleListPRs(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	listed, err := ghpr.List(r.Context(), ws.homeRoot, 0)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prs": listed})
}

// handleChecksFor is one pull request's CI, read on demand. Not folded into the
// list: that would be a gh call per row every time the list refreshes.
func (s *Server) handleChecksFor(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "which pull request?")
		return
	}
	found, err := ghpr.Checks(r.Context(), s.space().homeRoot, number)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if found == nil {
		found = []ghpr.Check{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"checks": found})
}

// handleOpenPR points ade at a pull request. Someone else's branch is not
// checked out, so that arrives as a read-only snapshot from GitHub and Resolve
// stays blocked; your own checked-out branch is diffed locally as before.
func (s *Server) handleOpenPR(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "which pull request?")
		return
	}
	if s.agent.State().Running {
		writeError(w, http.StatusConflict, "the agent is running; wait for it before changing what is under it")
		return
	}
	if err := s.openPR(r.Context(), number); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pr": s.space().pr})
}

// handleClosePR goes back to reviewing your own branch.
func (s *Server) handleClosePR(w http.ResponseWriter, r *http.Request) {
	if s.space().pr == nil {
		writeError(w, http.StatusBadRequest, "no pull request is open")
		return
	}
	if s.agent.State().Running {
		writeError(w, http.StatusConflict, "the agent is running; wait for it before changing what is under it")
		return
	}
	if err := s.backToBranch(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// openPR swaps the workspace for one describing a pull request.
func (s *Server) openPR(ctx context.Context, number int) error {
	current := s.space()

	pr, err := ghpr.Fetch(ctx, current.homeRoot, number)
	if err != nil {
		return err
	}
	head, err := gitdiff.HeadSHA(ctx, current.homeRoot)
	if err != nil {
		return err
	}

	info := &PRInfo{
		Number: pr.Number, Title: pr.Title, Author: pr.Author,
		URL: pr.URL, State: pr.State, HeadRef: pr.HeadRef,
	}
	info.ResolveBlocked = ResolveBlockedReason(pr, head, gitdiff.Branch(ctx, current.homeRoot))

	// A pull request keeps its own comment store. Its threads are anchored to
	// its diff, and mixing them into your branch's would orphan both halves the
	// moment you switched back.
	store, err := comments.Open(current.gitDir, fmt.Sprintf("pr-%d", pr.Number))
	if err != nil {
		return err
	}

	next := &workspace{
		homeRoot: current.homeRoot, branch: current.branch, gitDir: current.gitDir,
		opts: current.opts, store: store, pr: info,
	}

	if info.ResolveBlocked == "" {
		// Your own branch is checked out, so diff it locally and edits show
		// up — against origin's base branch, as GitHub does, not the local
		// one.
		next.opts.Base = gitdiff.RemoteBase(ctx, current.homeRoot, pr.BaseRef)
		next.source = gitSource(next.opts, true)
	} else {
		text, err := ghpr.Diff(ctx, current.homeRoot, number)
		if err != nil {
			return err
		}
		fixed := gitdiff.FromText("base", pr.HeadRef, text)
		next.snapshot = true
		next.source = func(context.Context) (*gitdiff.Diff, error) { return fixed, nil }
	}

	if err := s.swap(ctx, next); err != nil {
		return err
	}

	if found, err := ghpr.Comments(ctx, current.homeRoot, number); err == nil {
		if n := s.importComments(ghpr.GroupThreads(found)); n > 0 {
			slog.Info("imported review comments", "pr", number, "count", n)
		}
	} else {
		slog.Debug("read review comments", "err", err)
	}

	// Read now rather than waiting up to thirty seconds for the poll: the tab
	// you are about to land on is the one describing this pull request.
	s.refreshBranchPR(ctx)

	s.hub.Broadcast("state", nil)
	s.hub.Broadcast("threads", nil)
	return nil
}

// backToBranch restores the checkout's own diff and comment store.
func (s *Server) backToBranch(ctx context.Context) error {
	current := s.space()

	store, err := comments.Open(current.gitDir, current.branch)
	if err != nil {
		return err
	}
	opts := current.opts
	// The pull request set its own base; the branch goes back to working it out.
	opts.Base = s.startBase

	next := &workspace{
		homeRoot: current.homeRoot, branch: current.branch, gitDir: current.gitDir,
		opts: opts, store: store, source: gitSource(opts, false),
	}
	if err := s.swap(ctx, next); err != nil {
		return err
	}
	s.refreshBranchPR(ctx)

	s.hub.Broadcast("state", nil)
	s.hub.Broadcast("threads", nil)
	return nil
}

// swap replaces the workspace and the diff together, so no reader sees one
// checkout's files described by the other's.
func (s *Server) swap(ctx context.Context, next *workspace) error {
	diff, err := next.source(ctx)
	if err != nil {
		return fmt.Errorf("read the diff: %w", err)
	}
	highlight.Apply(diff)
	words.Apply(diff)

	s.mu.Lock()
	s.ws = next
	s.diff, s.diffHash = diff, hashDiff(diff)
	s.worktree, s.worktreeHash = nil, ""
	s.draft, s.created = nil, nil
	s.mu.Unlock()

	sweptComments(next.store.Reanchor(diff))
	return nil
}

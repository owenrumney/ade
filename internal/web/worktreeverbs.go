package web

import (
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/worktrees"
)

// stopWait is how long a session gets to go after SIGTERM. The prune that
// follows a close would fail anyway if it took longer.
const stopWait = 10 * time.Second

// namedWorktree resolves a path against a fresh scan, so a request can only
// ever reach a worktree of this repository.
func (s *Server) namedWorktree(r *http.Request, path string) (worktrees.Worktree, bool) {
	found, err := worktrees.Switchable(r.Context(), s.space().homeRoot)
	if err != nil {
		return worktrees.Worktree{}, false
	}
	return worktrees.Find(found, path)
}

// handlePushWorktree pushes a worktree's branch, which is what turns unpushed
// work from a reason to keep the checkout into a reason not to.
func (s *Server) handlePushWorktree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	wt, ok := s.namedWorktree(r, req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "not one of this repository's worktrees")
		return
	}
	if wt.Branch == "" {
		writeError(w, http.StatusBadRequest, wt.Name+" is detached, so there is no branch to push")
		return
	}
	if _, err := gitdiff.Push(r.Context(), wt.Path); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pushed": wt.Branch})
}

// handleCloseWorktree stops the ade serving another worktree so it can be
// removed. Never this one: the caller would lose the answer with the session.
func (s *Server) handleCloseWorktree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	wt, ok := s.namedWorktree(r, req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "not one of this repository's worktrees")
		return
	}
	if worktrees.SamePath(wt.Path, s.space().homeRoot) {
		writeError(w, http.StatusBadRequest, "that is this session; leave it from the chat instead")
		return
	}
	gitDir, err := gitdiff.GitDir(r.Context(), wt.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := session.Stop(session.Path(gitDir), stopWait); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"closed": wt.Name})
}

// handleMergeWorktree lands a branch on the one it forked from. The caller
// names the target, which is why every check that it is sane is here.
func (s *Server) handleMergeWorktree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Into string `json:"into"`
	}
	if !decode(w, r, &req) {
		return
	}
	from, ok := s.namedWorktree(r, req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "not one of this repository's worktrees")
		return
	}
	into, ok := s.namedWorktree(r, req.Into)
	if !ok {
		writeError(w, http.StatusBadRequest, "the branch to merge into is not one of this repository's worktrees")
		return
	}
	if worktrees.SamePath(from.Path, into.Path) {
		writeError(w, http.StatusBadRequest, "a branch cannot be merged into itself")
		return
	}
	if from.Branch == "" || into.Branch == "" {
		writeError(w, http.StatusBadRequest, "a detached worktree has no branch to merge")
		return
	}
	// Into a dirty tree the merge would mix someone's work in with the commit
	// it writes; out of one, it would leave that work behind unmerged.
	if into.Uncommitted > 0 {
		writeError(w, http.StatusBadRequest, into.Name+" has uncommitted work; commit or stash it first")
		return
	}
	if from.Uncommitted > 0 {
		writeError(w, http.StatusBadRequest, from.Name+" has uncommitted work that the merge would not carry")
		return
	}
	stuck, err := gitdiff.StartMerge(r.Context(), into.Path, from.Branch)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if len(stuck) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"merged": from.Branch, "into": into.Branch})
		return
	}
	if err := s.settleMerge(r.Context(), into.Path, from.Branch, into.Branch, stuck); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"merged": from.Branch, "into": into.Branch,
		"resolved": stuck, "by": outgoing.FastModel,
	})
}

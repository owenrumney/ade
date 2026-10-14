package web

import (
	"net/http"
	"path/filepath"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/worktrees"
)

// handleListWorktrees is the repository's linked worktrees and what stands in
// the way of removing each, so the page can offer to clear the finished ones.
func (s *Server) handleListWorktrees(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"
	found, err := worktrees.Scan(r.Context(), s.space().homeRoot, force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found == nil {
		found = []worktrees.Worktree{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"worktrees": found})
}

// handlePruneWorktrees removes the worktrees named, each checked again against
// a fresh scan: only one the scan calls removable goes, so a request cannot
// name a directory and have it deleted. Force lets an unpushed one go — the
// branch stays — and never an uncommitted one.
func (s *Server) handlePruneWorktrees(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
		Force bool     `json:"force"`
	}
	if !decode(w, r, &req) {
		return
	}
	root := s.space().homeRoot
	found, err := worktrees.Scan(r.Context(), root, req.Force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type kept struct {
		Path string `json:"path"`
		Why  string `json:"why"`
	}
	removed := []string{}
	left := []kept{}
	for _, path := range req.Paths {
		wt, ok := worktrees.Find(found, path)
		if !ok {
			left = append(left, kept{Path: path, Why: "not one of this repository's worktrees"})
			continue
		}
		if err := worktrees.Remove(r.Context(), root, wt); err != nil {
			left = append(left, kept{Path: path, Why: err.Error()})
			continue
		}
		removed = append(removed, path)
	}
	if len(removed) > 0 {
		if common, err := gitdiff.CommonGitDir(r.Context(), root); err == nil {
			_ = gitdiff.PruneWorktrees(r.Context(), filepath.Dir(common))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed, "kept": left})
}

// handleSwitchableWorktrees is where the status bar switcher can go: the
// same list the switch itself is gated on, so nothing offered is refused.
func (s *Server) handleSwitchableWorktrees(w http.ResponseWriter, r *http.Request) {
	found, err := worktrees.Switchable(r.Context(), s.space().homeRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"worktrees": found, "here": s.space().homeRoot})
}

// handleSwitchWorktree moves ade into another checkout of the repository,
// taking the agent. The path must be one a fresh scan offered, as prune gates.
func (s *Server) handleSwitchWorktree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	found, err := worktrees.Switchable(r.Context(), s.space().homeRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	wt, ok := worktrees.Find(found, req.Path)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of this repository's worktrees")
		return
	}

	// Claimed first: relocate moves the agent out from under any run.
	ctx, free := s.agent.begin(s.ctx, "switch")
	if !free {
		writeError(w, http.StatusConflict, "the agent is already running")
		return
	}
	err = s.relocate(ctx, wt.Path)
	s.agent.finish(err)
	s.hub.Broadcast("agent", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": wt.Path, "branch": gitdiff.Branch(r.Context(), wt.Path)})
}

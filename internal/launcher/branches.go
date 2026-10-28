package launcher

import (
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/worktrees"
)

// What the New session screen's Open branch row lists and opens. A branch is
// worked on in a checkout, so opening one means finding or making its
// worktree; starting the session is /api/open's job, as it is for every row.

// branchRow is a branch with no checkout of its own.
type branchRow struct {
	Name string `json:"name"`
	At   string `json:"at,omitempty"`
}

// handleBranches is a repository's checkouts and the branches that have none,
// so one list can offer both without the page knowing the difference.
func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	repo, ok, err := s.repoOf(r.URL.Query().Get("repo"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the repositories on this machine")
		return
	}
	found, err := worktrees.Switchable(r.Context(), repo.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found == nil {
		found = []worktrees.Worktree{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worktrees": found,
		"branches":  s.branchesWithout(r, repo.Path, found),
	})
}

// branchesWithout is every local branch no listed checkout is on: one that has
// a worktree is offered as that worktree, not twice.
func (s *Server) branchesWithout(
	r *http.Request,
	root string,
	held []worktrees.Worktree,
) []branchRow {
	found, err := gitdiff.BranchesByDate(r.Context(), root)
	if err != nil {
		return []branchRow{}
	}
	taken := map[string]bool{}
	for _, one := range held {
		if one.Branch != "" {
			taken[one.Branch] = true
		}
	}
	out := []branchRow{}
	for _, branch := range found {
		if !taken[branch.Name] {
			out = append(out, branchRow{Name: branch.Name, At: branch.At})
		}
	}
	return out
}

// handleBranchWorktree answers with the checkout to work on a branch in,
// making one when the branch has none. /api/open takes the path from here.
func (s *Server) handleBranchWorktree(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	branch := strings.TrimSpace(req.Branch)
	if branch == "" {
		writeError(w, http.StatusBadRequest, "name the branch to open")
		return
	}
	repo, ok, err := s.repoOf(req.Repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the repositories on this machine")
		return
	}
	path, added, err := gitdiff.EnsureWorktreeForBranch(r.Context(), repo.Path, branch,
		func() string {
			ref, _ := gitdiff.FreshBase(r.Context(), repo.Path)
			return ref
		})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if added {
		s.hub.Broadcast("repos", nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "added": added})
}

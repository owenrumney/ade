package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/worktrees"
)

// Switching the branch of the checkout ade is in, for someone who works in one
// checkout. Design: .git/ade/docs/ade-fixed-chat.md.

// switchBranch is one row of the picker.
type switchBranch struct {
	Name string `json:"name"`
	At   string `json:"at,omitempty"`
	// Stashed is what ade set aside last time this branch was left.
	Stashed int `json:"stashed,omitempty"`
}

// switched is what a branch switch did, each count a file count.
type switched struct {
	Branch   string   `json:"branch"`
	Stashed  int      `json:"stashed,omitempty"`
	Restored int      `json:"restored,omitempty"`
	Stuck    []string `json:"stuck,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// switchableBranches leaves out the current branch and any a worktree holds:
// git refuses a branch checked out elsewhere.
func (s *Server) switchableBranches(ctx context.Context) []switchBranch {
	root := s.space().homeRoot
	found, err := gitdiff.BranchesByDate(ctx, root)
	if err != nil {
		slog.Debug("list the branches for the switcher", "err", err)
		return nil
	}
	taken := map[string]bool{gitdiff.Branch(ctx, root): true}
	if held, err := worktrees.Switchable(ctx, root); err == nil {
		for _, wt := range held {
			if wt.Branch != "" {
				taken[wt.Branch] = true
			}
		}
	}
	repo := repoKey(ctx, root)
	waiting := settings.StashedIn(repo)

	out := []switchBranch{}
	for _, branch := range found {
		if taken[branch.Name] {
			continue
		}
		row := switchBranch{Name: branch.Name, At: branch.At}
		if sha := waiting[branch.Name]; sha != "" {
			row.Stashed = len(gitdiff.StashFiles(ctx, root, sha))
		}
		out = append(out, row)
	}
	return out
}

// handleSwitchBranch moves this checkout onto a branch the same GET offered.
func (s *Server) handleSwitchBranch(w http.ResponseWriter, r *http.Request, name string) {
	offered := false
	for _, row := range s.switchableBranches(r.Context()) {
		offered = offered || row.Name == name
	}
	if !offered {
		writeError(w, http.StatusBadRequest, "that is not a branch this checkout can switch to")
		return
	}

	// Claimed first: the checkout changes the files a run is editing.
	ctx, free := s.agent.begin(s.ctx, "switch")
	if !free {
		writeError(w, http.StatusConflict, "the agent is already running")
		return
	}
	out, err := s.moveOnto(ctx, s.space().homeRoot, name)
	s.agent.finish(err)
	s.hub.Broadcast("agent", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.afterRebase()
	writeJSON(w, http.StatusOK, out)
}

// moveOnto checks the branch out, stashing only where git refuses.
func (s *Server) moveOnto(ctx context.Context, root, name string) (switched, error) {
	out := switched{Branch: name}
	repo := repoKey(ctx, root)
	from := gitdiff.Branch(ctx, root)

	err := gitdiff.CheckoutBranch(ctx, root, name)
	if errors.Is(err, gitdiff.ErrLocalChanges) {
		sha, keepErr := gitdiff.StashKeep(ctx, root, "ade: off "+from+" for "+name)
		if keepErr != nil {
			return out, keepErr
		}
		out.Stashed = len(gitdiff.StashFiles(ctx, root, sha))
		// A detached HEAD is no branch to come back to, so nothing is keyed.
		if sha != "" && from != "detached" {
			if err := settings.KeepStash(repo, from, sha); err != nil {
				slog.Debug("record the stash", "branch", from, "err", err)
			}
		}
		if err = gitdiff.CheckoutBranch(ctx, root, name); err != nil {
			s.putStashBack(ctx, root, repo, from, sha)
			return switched{Branch: from}, fmt.Errorf("%v; your work is back on %s", err, from)
		}
	} else if err != nil {
		return out, err
	}

	if sha := settings.Stashed(repo, name); sha != "" {
		s.restore(ctx, root, repo, name, sha, &out)
	}
	return out, nil
}

// restore puts a branch's own stash back. A conflict keeps the entry.
func (s *Server) restore(ctx context.Context, root, repo, name, sha string, out *switched) {
	held := gitdiff.StashFiles(ctx, root, sha)
	stuck, err := gitdiff.StashApplyAt(ctx, root, sha)
	switch {
	case err != nil:
		if err := settings.ForgetStash(repo, name); err != nil {
			slog.Debug("forget the stash", "branch", name, "err", err)
		}
		out.Note = err.Error()
	case len(stuck) > 0:
		out.Stuck = stuck
		out.Note = "the work ade set aside conflicted, and is still in the stash"
	default:
		out.Restored = len(held)
		if err := gitdiff.StashDropAt(ctx, root, sha); err != nil {
			slog.Debug("drop the applied stash", "branch", name, "err", err)
		}
		if err := settings.ForgetStash(repo, name); err != nil {
			slog.Debug("forget the stash", "branch", name, "err", err)
		}
	}
}

// putStashBack leaves the checkout as it was found.
func (s *Server) putStashBack(ctx context.Context, root, repo, from, sha string) {
	if sha == "" {
		return
	}
	if _, err := gitdiff.StashApplyAt(ctx, root, sha); err != nil {
		slog.Debug("put the stash back", "err", err)
		return
	}
	if err := gitdiff.StashDropAt(ctx, root, sha); err != nil {
		slog.Debug("drop the stash put back", "err", err)
	}
	if err := settings.ForgetStash(repo, from); err != nil {
		slog.Debug("forget the stash", "branch", from, "err", err)
	}
}

// repoKey is the ledger's name for the repository.
func repoKey(ctx context.Context, root string) string {
	if common, err := gitdiff.CommonGitDir(ctx, root); err == nil {
		return common
	}
	return root
}

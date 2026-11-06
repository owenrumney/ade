package web

import (
	"context"
	"fmt"
	"net/http"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/highlight"
	"github.com/owenrumney/ade/internal/words"
)

// rebind picks up a branch change in the same directory. The agent stays.
func (s *Server) rebind(ctx context.Context, root string) error {
	gitDir, err := gitdiff.GitDir(ctx, root)
	if err != nil {
		return err
	}
	branch := gitdiff.Branch(ctx, root)

	// Threads were anchored to a diff that no longer applies, so this starts empty.
	store, err := comments.Open(gitDir, branch)
	if err != nil {
		return err
	}

	current := s.space()
	opts := current.opts
	opts.Repo = root
	next := &workspace{
		homeRoot: root,
		branch:   branch,
		gitDir:   gitDir,
		opts:     opts,
		store:    store,
		source:   gitSource(opts, current.pr != nil),
		pr:       current.pr,
		snapshot: current.snapshot,
	}

	// Read the new diff before swapping, so no reader sees a checkout with none.
	diff, err := next.source(ctx)
	if err != nil {
		return fmt.Errorf("read the worktree's diff: %w", err)
	}
	highlight.Apply(diff)
	words.Apply(diff)

	s.mu.Lock()
	s.ws = next
	s.diff, s.diffHash = diff, hashDiff(diff)
	s.worktree, s.worktreeHash = nil, ""
	s.draft = nil
	s.homeOpts = gitdiff.Options{}
	s.mu.Unlock()

	s.rehomeRoots(ctx, root)
	s.rememberWorkspace()
	s.rerecord(current.gitDir)
	s.refreshWorktree(ctx)
	sweptComments(store.Reanchor(diff))
	s.hub.Broadcast("state", nil)
	return nil
}

// setScope changes what the diff is showing. Unlike relocate this is the same
// checkout, so the thread store carries over and threads re-anchor against the
// narrower view rather than being started again.
func (s *Server) setScope(ctx context.Context, scope string) error {
	if !gitdiff.ValidScope(scope) {
		return fmt.Errorf("%q is not a diff scope", scope)
	}

	current := s.space()
	if current.opts.Scope == scope {
		return nil
	}

	opts := current.opts
	opts.Scope = scope
	next := &workspace{
		homeRoot: current.homeRoot,
		branch:   current.branch,
		gitDir:   current.gitDir,
		opts:     opts,
		store:    current.store,
		source:   gitSource(opts, current.pr != nil),
		pr:       current.pr,
		snapshot: current.snapshot,
		// Scope is per root, so narrowing the view does not leave it.
		viewRoot: current.viewRoot,
	}

	diff, err := next.source(ctx)
	if err != nil {
		return err
	}
	highlight.Apply(diff)
	words.Apply(diff)

	s.mu.Lock()
	s.ws = next
	s.diff, s.diffHash = diff, hashDiff(diff)
	s.mu.Unlock()

	sweptComments(next.store.Reanchor(diff))
	s.hub.Broadcast("state", nil)
	return nil
}

// handleScope switches the diff between the branch, what is committed, and what
// is still pending commit.
func (s *Server) handleScope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
	}
	if !decode(w, r, &req) {
		return
	}
	if s.space().snapshot {
		writeError(w, http.StatusBadRequest, "a fetched snapshot only has the one view")
		return
	}
	if err := s.setScope(r.Context(), req.Scope); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"scope": req.Scope})
}

// project is owner/repo for the checkout being served, worked out once and then
// remembered: reading it per request would shell out to git on every poll, and
// a remote does not change under a running server. A checkout with no remote is
// asked again, since gaining one is a thing that happens.
func (s *Server) project() string {
	root := s.space().homeRoot

	s.mu.RLock()
	known, asked := s.projectName, s.projectFor
	s.mu.RUnlock()
	if asked == root {
		return known
	}

	owner, repo := gitdiff.Origin(s.ctx, root)
	if repo == "" {
		// Not cached: a checkout can gain a remote while a server is running,
		// and remembering that it had none would never notice.
		return ""
	}

	name := owner + "/" + repo
	s.mu.Lock()
	s.projectName, s.projectFor = name, root
	s.mu.Unlock()
	return name
}

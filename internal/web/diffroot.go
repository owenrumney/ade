package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/highlight"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/words"
)

// DiffRoot is one repository the Diff tab can be pointed at: this session's
// own, or a directory its sandbox mounts read-write.
type DiffRoot struct {
	Root   string `json:"root"`
	Name   string `json:"name"`
	Branch string `json:"branch"`

	// Home marks the session's own checkout, which is always offered first.
	Home bool `json:"home,omitempty"`

	// Changes is the paths with uncommitted work, Threads the open comments in
	// that repository's own store. Both are read when a turn settles.
	Changes int `json:"changes"`
	Threads int `json:"threads"`
}

// snapshotRoots recomputes which repositories may be diffed, at start and on a
// mounts save. An agent writes sandbox.json, so a live read would grant a root.
func (s *Server) snapshotRoots(ctx context.Context) {
	found := s.rootsFrom(ctx)
	s.mu.Lock()
	s.roots = found
	s.mu.Unlock()
	s.refreshRoots(ctx)
	s.holdViewRoot(ctx)
}

// holdViewRoot returns the diff home when the root it is on is no longer
// granted. On the tick too: a switch is refused mid-run, so the save retries.
func (s *Server) holdViewRoot(ctx context.Context) {
	root := s.space().viewRoot
	if root == "" {
		return
	}
	if _, ok := s.grantedRoot(root); ok {
		return
	}
	// Not on the request's context: a browser that leaves as the save returns
	// would cancel the way back and strand the diff there.
	if err := s.setViewRoot(s.detached(ctx), ""); err != nil {
		slog.Debug("could not return the diff to this checkout yet", "err", err)
	}
}

// rehomeRoots renames this session's entry after a bind. sandbox.json is not
// read again: only a user act may grant a root.
func (s *Server) rehomeRoots(ctx context.Context, root string) {
	s.mu.Lock()
	if len(s.roots) == 0 {
		s.mu.Unlock()
		return
	}
	resolved := resolveRoot(root)
	next := make([]DiffRoot, 0, len(s.roots))
	for _, have := range s.roots {
		switch {
		case have.Home:
			have.Root, have.Name = root, filepath.Base(root)
		case have.Root == resolved:
			// Bound into a directory that was also a mount: one entry, not two.
			continue
		}
		next = append(next, have)
	}
	if len(next) == 1 {
		next = nil
	}
	s.roots = next
	s.mu.Unlock()
	s.refreshRoots(ctx)
}

// detached is the server's own context, for work the request only triggered.
// Nil before Run, which is the tests, where the caller's will do.
func (s *Server) detached(ctx context.Context) context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return ctx
}

// rootsFrom reads the sandbox file and keeps the read-write mounts that are
// git work trees, one entry per repository, the session's own first.
func (s *Server) rootsFrom(ctx context.Context) []DiffRoot {
	ws := s.space()
	if ws.snapshot || ws.homeRoot == "" {
		return nil
	}
	home := resolveRoot(ws.homeRoot)
	roots := []DiffRoot{{Root: ws.homeRoot, Name: filepath.Base(ws.homeRoot), Home: true}}
	seen := map[string]bool{home: true}

	mounts, err := sandbox.LoadWorkspace(s.agent.workspace)
	if err != nil {
		slog.Debug("read the sandbox for diff roots", "err", err)
		return nil
	}
	for _, path := range mounts.Paths {
		// A read-only mount cannot have been edited, so it has nothing to show.
		if !path.RW {
			continue
		}
		root, err := gitdiff.RepoRoot(ctx, resolveRoot(path.Path))
		if err != nil {
			continue
		}
		root = resolveRoot(root)
		if seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, DiffRoot{Root: root, Name: filepath.Base(root)})
	}

	// Nothing but home is no choice, and the page collapses the control.
	if len(roots) == 1 {
		return nil
	}
	return roots
}

// refreshRoots re-reads each root's branch, changes and open comments. Off the
// state poll: only a finished turn can move them.
func (s *Server) refreshRoots(ctx context.Context) {
	s.mu.RLock()
	roots := append([]DiffRoot(nil), s.roots...)
	s.mu.RUnlock()
	if len(roots) == 0 {
		return
	}

	for i := range roots {
		roots[i].Branch = gitdiff.Branch(ctx, roots[i].Root)
		if status, err := gitdiff.WorktreeStatus(ctx, roots[i].Root); err == nil {
			roots[i].Changes = len(status.Staged) + len(status.Unstaged)
		}
		roots[i].Threads = openThreadsIn(ctx, roots[i].Root, roots[i].Branch)
	}

	// A mounts save during the git calls is the newer answer: take the counts
	// into the list as it is now, so a revoked root is never put back.
	read := make(map[string]DiffRoot, len(roots))
	for _, root := range roots {
		read[root.Root] = root
	}
	s.mu.Lock()
	next := append([]DiffRoot(nil), s.roots...)
	for i, root := range next {
		if found, ok := read[root.Root]; ok {
			next[i] = found
		}
	}
	s.roots = next
	s.mu.Unlock()
}

// openThreadsIn counts what is unresolved in a repository's own store, so the
// menu says what switching would bring into the sidebar.
func openThreadsIn(ctx context.Context, root, branch string) int {
	if branch == "" {
		return 0
	}
	gitDir, err := gitdiff.GitDir(ctx, root)
	if err != nil {
		return 0
	}
	store, err := comments.Open(gitDir, branch)
	if err != nil {
		return 0
	}
	return len(store.Unresolved())
}

// currentRoots is the list as last read, for the state.
func (s *Server) currentRoots() []DiffRoot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roots
}

// grantedRoot resolves a requested path and checks it against the snapshot,
// never against the file. "" is home and is always granted.
func (s *Server) grantedRoot(want string) (string, bool) {
	if want == "" {
		return "", true
	}
	resolved := resolveRoot(want)
	for _, root := range s.currentRoots() {
		if !root.Home && root.Root == resolved {
			return root.Root, true
		}
	}
	return "", false
}

// resolveRoot follows symlinks so a mount and its target are one path. A path
// that cannot be resolved is cleaned and compared as given.
func resolveRoot(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// elsewhereNote is what a reviewer is told when the diff is not the session's
// own checkout, and why it gets nothing of the session's.
func (s *Server) elsewhereNote() string {
	root := s.space().viewRoot
	if root == "" {
		return ""
	}
	return fmt.Sprintf(`The change under review is in %s. That is your working directory, and it is `+
		`not the repository this ade session is working in, so read it for context and judge the `+
		`change on its own terms. There is no design document and no project memory for it here: `+
		`take the repository's own conventions from the files you read.`, root)
}

// handleDiffRoot points the Diff tab at another repository, or back home.
func (s *Server) handleDiffRoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Root string `json:"root"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.setViewRoot(r.Context(), req.Root); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"root": s.space().viewRoot})
}

// setViewRoot moves the diff, and only the diff, to another mounted repository;
// "" goes home. guardHome is what stops anything acting from there.
func (s *Server) setViewRoot(ctx context.Context, want string) error {
	current := s.space()
	if current.snapshot || current.pr != nil {
		return fmt.Errorf("a pull request has only the one repository to show")
	}
	root, ok := s.grantedRoot(want)
	if !ok {
		return fmt.Errorf("%s is not a read-write root of this session's sandbox", want)
	}
	if root == current.viewRoot {
		return nil
	}
	// A run in flight is reading and filing against the diff on screen.
	if s.agent.State().Running {
		return fmt.Errorf("the agent is running against this diff; wait for it or stop it first")
	}
	if runs := s.sideRuns(); len(runs) > 0 {
		return fmt.Errorf("@%s is still reviewing this diff; wait for it or stop it first", runs[0].Name)
	}

	next, err := s.viewWorkspace(ctx, current, root)
	if err != nil {
		return err
	}
	diff, err := next.source(ctx)
	if err != nil {
		return fmt.Errorf("read %s's diff: %w", filepath.Base(next.diffRoot()), err)
	}

	// A sandbox repository is usually sitting on its own main branch with
	// uncommitted edits, where branch scope shows nothing at all.
	if root != "" && diff.Commits == 0 && next.opts.Scope == gitdiff.ScopeBranch {
		next.opts.Scope = gitdiff.ScopeUncommitted
		next.source = gitSource(next.opts, false)
		if again, err := next.source(ctx); err == nil {
			diff = again
		}
	}
	highlight.Apply(diff)
	words.Apply(diff)

	// Held idle across the swap: a run begun during the git calls above would
	// otherwise be filing against the diff being replaced.
	var side error
	idle := s.agent.holdIdle(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.sides) > 0 {
			side = fmt.Errorf("a named agent is reviewing this diff; wait for it or stop it first")
			return
		}
		if current.viewRoot == "" {
			// So coming home restores the base and the scope it was left on.
			s.homeOpts = current.opts
		}
		s.ws = next
		s.diff, s.diffHash = diff, hashDiff(diff)
		s.worktree, s.worktreeHash = nil, ""
		// The draft describes the index of the checkout being left, the way it
		// does across a bind.
		s.draft = nil
	})
	if !idle {
		return fmt.Errorf("the agent is running against this diff; wait for it or stop it first")
	}
	if side != nil {
		return side
	}

	// The branch home moved to while the view was away has its own design.
	if next.branch != current.branch {
		s.rematchDesign(next.homeRoot, next.branch)
	}

	sweptComments(next.store.Reanchor(diff))
	s.refreshWorktree(ctx)
	s.refreshRoots(ctx)
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	return nil
}

// viewWorkspace builds the workspace for a view switch. Everything that names
// the session is copied across; only what the diff needs is opened again.
func (s *Server) viewWorkspace(ctx context.Context, current *workspace, root string) (*workspace, error) {
	next := &workspace{
		homeRoot: current.homeRoot,
		branch:   current.branch,
		gitDir:   current.gitDir,
	}

	gitDir, branch := current.gitDir, current.branch
	opts := s.takeHomeOpts()
	if root != "" {
		var err error
		if gitDir, err = gitdiff.GitDir(ctx, root); err != nil {
			return nil, err
		}
		branch = gitdiff.Branch(ctx, root)
		next.viewRoot = root
		// Base empty so the foreign repository resolves its own default branch
		// rather than inheriting this one's.
		opts = gitdiff.Options{Scope: gitdiff.ScopeBranch, Context: current.opts.Context}
	} else if now := gitdiff.Branch(ctx, current.homeRoot); now != "" && now != branch {
		// followBranch stands down while the view is away, so home may have been
		// checked out elsewhere since it left.
		branch, next.branch = now, now
	}
	opts.Repo = next.diffRoot()

	// Not cached: a key by git directory would miss the branch, and a repository
	// that moved branch between visits would be served the one before's threads.
	store, err := comments.Open(gitDir, branch)
	if err != nil {
		return nil, err
	}
	next.opts, next.store, next.source = opts, store, gitSource(opts, false)
	return next, nil
}

// takeHomeOpts is the diff options home was left on, or this session's own when
// it has not been left yet.
func (s *Server) takeHomeOpts() gitdiff.Options {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.homeOpts.Repo != "" {
		return s.homeOpts
	}
	return s.ws.opts
}

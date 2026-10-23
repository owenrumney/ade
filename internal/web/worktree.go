package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/highlight"
	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/words"
	"github.com/owenrumney/ade/internal/worktrees"
)

// unsafeSlug is anything that has no business in a branch name or a directory.
var unsafeSlug = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Worktree is where a build would be isolated, offered before you commit to it.
type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

// proposedWorktree names the worktree a build of the open document would use.
func (s *Server) proposedWorktree() *Worktree {
	file := s.currentDoc()
	if file == "" || s.space().snapshot {
		return nil
	}
	slug := Slug(file)
	if slug == "" {
		return nil
	}
	// Already on the branch this document names, so this checkout is its
	// worktree; offering another one nested off it helps nobody.
	if strings.EqualFold(s.space().branch, slug) {
		return nil
	}
	return &Worktree{Path: gitdiff.WorktreePath(s.space().homeRoot, slug), Branch: slug}
}

// Slug turns a document name into something usable as a branch and a directory.
func Slug(file string) string {
	base := filepath.Base(strings.TrimSuffix(file, filepath.Ext(file)))
	base = unsafeSlug.ReplaceAllString(base, "-")
	return strings.Trim(base, "-.")
}

// intoWorktree checks the repository out again beside itself and moves ade
// there, so a build runs isolated from the tree you are working in.
func (s *Server) intoWorktree(ctx context.Context) (*Worktree, error) {
	proposed := s.proposedWorktree()
	if proposed == nil {
		file := s.currentDoc()
		if file == "" {
			return nil, fmt.Errorf("there is no document to name a worktree after")
		}
		return nil, fmt.Errorf("this checkout is already %s, which is the worktree %s names", s.space().branch, file)
	}
	// Off the fetched base, not this checkout's HEAD: a second track is not
	// built on the first.
	start, note := gitdiff.FreshBase(ctx, s.space().homeRoot)
	if note != "" {
		slog.Info("worktree base", "note", note)
	}
	if err := gitdiff.AddWorktreeFrom(ctx, s.space().homeRoot, proposed.Path, proposed.Branch, start); err != nil {
		return nil, err
	}
	if _, err := worktrees.Sow(ctx, s.space().homeRoot, proposed.Path); err != nil {
		slog.Warn("some of the worktree seed did not carry", "path", proposed.Path, "err", err)
	}
	if err := s.relocate(ctx, proposed.Path); err != nil {
		return nil, err
	}
	return proposed, nil
}

// relocate points ade at a different checkout, taking the agent with it.
//
// It must only be called while holding the agent lock, since it moves the agent
// out from under any run.
func (s *Server) relocate(ctx context.Context, root string) error {
	return s.bind(ctx, root, true)
}

// rebind picks up a branch change in the same directory. The agent stays: the
// working directory has not moved, and dropping its session on every checkout
// would be a bug of its own.
func (s *Server) rebind(ctx context.Context, root string) error {
	return s.bind(ctx, root, false)
}

// bind points ade at a checkout. Everything tied to the old one is replaced
// together, so no reader sees half of each.
func (s *Server) bind(ctx context.Context, root string, moveAgent bool) error {
	gitDir, err := gitdiff.GitDir(ctx, root)
	if err != nil {
		return err
	}
	branch := gitdiff.Branch(ctx, root)

	// A new checkout means a new branch, and threads were anchored to a diff
	// that no longer applies, so this starts empty rather than carrying them.
	store, err := comments.Open(gitDir, branch)
	if err != nil {
		return err
	}

	current := s.space()
	opts := current.opts
	opts.Repo = root

	// pr and snapshot are carried, not recomputed: moving between checkouts does
	// not change which pull request is being served, and dropping them here
	// would quietly re-enable Resolve on someone else's code.
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

	// Read the new diff before swapping anything. Clearing the old one first
	// would leave a window where the workspace is new and the diff is nil, and a
	// reader landing in it sees a checkout with no diff at all.
	diff, err := next.source(ctx)
	if err != nil {
		return fmt.Errorf("read the worktree's diff: %w", err)
	}
	highlight.Apply(diff)
	words.Apply(diff)

	s.mu.Lock()
	s.ws = next
	s.diff, s.diffHash = diff, hashDiff(diff)
	// These all describe the checkout being left behind.
	s.worktree, s.worktreeHash = nil, ""
	s.draft, s.created = nil, nil
	s.homeOpts = gitdiff.Options{}
	s.mu.Unlock()

	// Home has moved, so the list names the wrong root as this session's. Renamed
	// rather than re-read: the branch poll reaches here, and the agent writes that file.
	s.rehomeRoots(ctx, root)

	// Moving into a worktree is arriving somewhere, so the history says so.
	s.rememberWorkspace()

	if moveAgent {
		// The session belongs to the old directory: Claude Code keys transcripts
		// by working directory, so resuming it here would silently start fresh.
		// The agent takes on this checkout's own memory instead, which is a
		// fresh agent for a new worktree and the last conversation for one you
		// have built in before.
		s.agent.relocate(root, runlog.Open(runlog.Path(gitDir)))
	}
	// The conversations belong to the checkout, the same way the threads do,
	// so they are reopened rather than carried across. A rebind stays in the
	// same git dir and keeps the store it has.
	if err := s.rehome(gitDir); err != nil {
		return err
	}
	s.rerecord(current.gitDir)

	// A different branch usually means a different design, and any PR opened
	// from here belonged to the branch being left. Designs are looked for under
	// the common git dir, not this checkout's, so a worktree finds the document
	// that was written for it.
	s.rematchDesign(root, branch)

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

// rematchDesign re-runs the branch match after a bind, so the Design tab follows
// the branch rather than holding a document for the one just left.
func (s *Server) rematchDesign(root, branch string) {
	if s.docsDir == "" {
		return
	}
	dirs := docs.SearchDirs(root, s.commonGitDir, s.docsOverride)
	dir, file, ok := docs.Find(dirs, s.commonGitDir, branch, s.agent.Current())

	s.mu.Lock()
	if ok {
		s.docsDir, s.docFile = dir, file
	} else {
		// Nothing for this branch: no document rather than the previous one.
		s.docFile = ""
	}
	open := s.docFile
	s.mu.Unlock()

	// Not the empty case: that would record a close that never happened.
	if open != "" {
		s.rememberDoc(branch, open)
	}
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

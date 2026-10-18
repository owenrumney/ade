// Package worktrees knows which of a repository's linked checkouts can go.
//
// A session in a new worktree is cheap to start, so worktrees pile up. This
// is the one place that says, for each, whether anything would be lost by
// removing it — a live server, uncommitted files, commits that exist nowhere
// else — so the CLI and the page prune by the same rules.
package worktrees

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/provision"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/statedir"
)

// The kinds of worktree ade makes. A session worktree sits beside the
// repository and is somewhere you work; a review one sits under the git dir
// and holds someone else's pull request.
const (
	KindSession = "session"
	KindReview  = "review"
	// KindClone is a whole checkout a session was cloned into on a worker,
	// not a linked worktree: removing it takes the session directory around it.
	KindClone = "clone"
)

// Why a worktree is kept.
const (
	KeepServing     = "serving"
	KeepUncommitted = "uncommitted"
	KeepUnpushed    = "unpushed"
)

// Worktree is one linked checkout and what stands in the way of removing it.
type Worktree struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Branch string `json:"branch,omitempty"`
	Kind   string `json:"kind"`

	// Serving is the URL of the ade serving it, when one is. A served
	// worktree is never removed: something is using it.
	Serving string `json:"serving,omitempty"`

	// Keep is the first reason it should stay, or empty when nothing would be
	// lost. Removable is that, folded with whether the caller asked to lose
	// unpushed work anyway.
	Keep      string `json:"keep,omitempty"`
	Removable bool   `json:"removable"`

	Uncommitted int  `json:"uncommitted,omitempty"`
	Unpushed    int  `json:"unpushed,omitempty"`
	Merged      bool `json:"merged,omitempty"`

	// Behind is what the base has and this does not, as of the last fetch.
	// Scan does not fetch: a list is not a network call.
	Behind int `json:"behind,omitempty"`

	// Main marks the repository's own checkout, which Scan never lists and
	// Switchable puts first.
	Main bool `json:"main,omitempty"`

	// LastUsed is when git last touched it: a commit, a checkout, a status.
	LastUsed time.Time `json:"lastUsed,omitempty"`
}

// Scan lists the repository's linked worktrees, oldest first. With force, a
// worktree whose only problem is unpushed commits is removable: the branch is
// kept, so the commits are not lost, only the checkout.
func Scan(ctx context.Context, root string, force bool) ([]Worktree, error) {
	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return nil, err
	}
	main := filepath.Dir(common)
	paths, err := gitdiff.Worktrees(ctx, main)
	if err != nil {
		return nil, err
	}
	reviews := filepath.Join(statedir.Git(common), "worktrees")
	// Resolved once: every worktree of a repository is behind the same branch.
	base := gitdiff.RemoteBase(ctx, main, gitdiff.BaseBranch(ctx, main))

	var out []Worktree
	for _, path := range paths {
		// git reports real paths; a temp dir on macOS is reached through a
		// symlink, so the main checkout has to be matched by what it resolves
		// to rather than how it was spelled.
		if SamePath(path, main) {
			continue
		}
		wt := inspect(ctx, path, underneath(path, reviews), base)
		wt.Removable = wt.Keep == "" || (force && wt.Keep == KeepUnpushed)
		out = append(out, wt)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsed.Before(out[j].LastUsed) })
	return out, nil
}

// Clone judges a worker's session clone by the rules a worktree is judged by:
// it is a checkout like any other, and the same work can be lost with it.
func Clone(ctx context.Context, path string, force bool) Worktree {
	base := gitdiff.RemoteBase(ctx, path, gitdiff.BaseBranch(ctx, path))
	wt := inspect(ctx, path, false, base)
	wt.Kind = KindClone
	wt.Removable = wt.Keep == "" || (force && wt.Keep == KeepUnpushed)
	return wt
}

// Switchable is where an ade can move to: the main checkout first, then the
// session worktrees. A review checkout is someone else's code, so never one.
func Switchable(ctx context.Context, root string) ([]Worktree, error) {
	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return nil, err
	}
	main := filepath.Dir(common)
	found, err := Scan(ctx, root, false)
	if err != nil {
		return nil, err
	}

	base := gitdiff.RemoteBase(ctx, main, gitdiff.BaseBranch(ctx, main))
	head := inspect(ctx, main, false, base)
	head.Main = true
	out := []Worktree{head}
	for _, wt := range found {
		if wt.Kind == KindReview {
			continue
		}
		out = append(out, wt)
	}
	return out, nil
}

// Find picks a worktree out of a scan, so a request can only name one that
// was actually offered.
func Find(list []Worktree, path string) (Worktree, bool) {
	for _, wt := range list {
		if SamePath(wt.Path, path) {
			return wt, true
		}
	}
	return Worktree{}, false
}

// SamePath is whether two spellings name one directory. git reports real
// paths; a temp dir on macOS is reached through a symlink.
func SamePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// Remove takes a worktree away: its server is stopped first, and the checkout
// removed from the main one, since it is about to stop existing. The branch
// is never touched — it is the record of the work.
func Remove(ctx context.Context, root string, wt Worktree) error {
	if wt.Serving != "" {
		return fmt.Errorf("ade is serving %s on %s", wt.Path, wt.Serving)
	}
	if wt.Keep == KeepUncommitted && !wt.Removable {
		return fmt.Errorf("%d file(s) in %s are uncommitted", wt.Uncommitted, wt.Path)
	}
	if !wt.Removable {
		return fmt.Errorf("%s is kept: %s", wt.Path, wt.Keep)
	}
	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return err
	}
	main := filepath.Dir(common)

	// Stopped first: removing the directory out from under a running server
	// leaves it polling a checkout that is no longer there.
	if gitDir, err := gitdiff.GitDir(ctx, wt.Path); err == nil {
		_, _ = session.Stop(session.Path(gitDir), 5*time.Second)
	}
	if wt.Uncommitted > 0 {
		return gitdiff.RemoveWorktreeForce(ctx, main, wt.Path)
	}
	if err := gitdiff.RemoveWorktree(ctx, main, wt.Path); err != nil {
		return err
	}
	return nil
}

// RemoveClone takes a worker's session clone away, and the session directory
// around it: that directory holds this checkout and nothing else.
func RemoveClone(ctx context.Context, wt Worktree) error {
	owned, ok := provision.OwnedDir(wt.Path)
	if !ok {
		return fmt.Errorf("%s is not a session clone", wt.Path)
	}
	if wt.Serving != "" {
		return fmt.Errorf("ade is serving %s on %s", wt.Path, wt.Serving)
	}
	if !wt.Removable {
		return fmt.Errorf("%s is kept: %s", wt.Path, wt.Keep)
	}
	if gitDir, err := gitdiff.GitDir(ctx, wt.Path); err == nil {
		_, _ = session.Stop(session.Path(gitDir), 5*time.Second)
	}
	return os.RemoveAll(owned)
}

// underneath is whether path is inside dir, by what both resolve to.
func underneath(path, dir string) bool {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// inspect reads one worktree. Its reasons are found in the order they matter:
// something using it, then work that exists nowhere else.
func inspect(ctx context.Context, path string, review bool, base string) Worktree {
	wt := Worktree{Path: path, Name: filepath.Base(path), Branch: gitdiff.Branch(ctx, path), Kind: KindSession}
	if review {
		wt.Kind = KindReview
	}
	// A review checkout is detached at someone else's pull request head, so
	// the repository's base is not a branch it is behind.
	if !review && base != "" {
		if n, err := gitdiff.CommitsBehind(ctx, path, base); err == nil {
			wt.Behind = n
		}
	}
	gitDir, err := gitdiff.GitDir(ctx, path)
	if err == nil {
		wt.LastUsed = touched(gitDir)
		if rec, ok := session.Read(session.Path(gitDir)); ok && session.Live(ctx, rec) {
			wt.Serving = rec.URL
			wt.Keep = KeepServing
			return wt
		}
	}

	status, err := gitdiff.WorktreeStatus(ctx, path)
	if err != nil {
		wt.Keep = err.Error()
		return wt
	}
	wt.Uncommitted = len(status.Staged) + len(status.Unstaged)
	if wt.Uncommitted > 0 {
		wt.Keep = KeepUncommitted
		return wt
	}

	switch {
	case review:
		// A review checkout is detached, so it never has an upstream; the
		// fetched ref stands in for one.
		wt.Unpushed = ahead(ctx, path, "refs/ade/"+filepath.Base(path))
	case status.Upstream != "":
		wt.Unpushed = status.Ahead
	default:
		// Never pushed. What is on the branch is safe only if some other
		// branch, local or remote, already holds every commit of it — merged
		// into main, say, or nothing committed here at all.
		only, err := gitdiff.OnlyOn(ctx, path, wt.Branch)
		if err != nil {
			wt.Keep = err.Error()
			return wt
		}
		wt.Unpushed = only
		wt.Merged = only == 0
	}
	if wt.Unpushed > 0 {
		wt.Keep = KeepUnpushed
	}
	return wt
}

func ahead(ctx context.Context, path, ref string) int {
	n, err := gitdiff.CommitsAhead(ctx, path, ref)
	if err != nil {
		return 0
	}
	return n
}

// touched is when git last wrote to a worktree: HEAD moves on a commit or a
// checkout, the index on a status. The newer of the two is close enough to
// "last used" to order a list by.
func touched(gitDir string) time.Time {
	var latest time.Time
	for _, name := range []string{"HEAD", "index"} {
		if info, err := os.Stat(filepath.Join(gitDir, name)); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest
}

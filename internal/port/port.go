// Package port moves a scratch's commits into a worktree of the repository
// they belong in. Nothing to do with TCP ports. See the design doc.
package port

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/worktrees"
)

// Result is where the work landed.
type Result struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`

	Repo    string `json:"repo"`
	Commits int    `json:"commits"`

	// Committed says the scratch was left different from how it was found.
	Committed bool `json:"committed,omitempty"`
}

// wip is the message for whatever was still uncommitted: the usual case.
const wip = "scratch: work in progress"

// Move carries the scratch at from into the repository at into, on branch,
// and returns the worktree it made. An empty branch takes the scratch's.
func Move(ctx context.Context, from, into, branch string) (Result, error) {
	scratch, err := gitdiff.RepoRoot(ctx, from)
	if err != nil {
		return Result{}, fmt.Errorf("%s is not a checkout", from)
	}
	if !settings.IsScratch(scratch) {
		return Result{}, fmt.Errorf("%s is not a scratch; porting is for the checkouts ade made under ~/.ade/scratch", scratch)
	}
	repo, err := gitdiff.RepoRoot(ctx, into)
	if err != nil {
		return Result{}, fmt.Errorf("%s is not a checkout", into)
	}
	if repo == scratch {
		return Result{}, fmt.Errorf("that is the scratch itself; name the repository the work belongs in")
	}

	base := gitdiff.BaseBranch(ctx, scratch)
	if base == "" {
		return Result{}, fmt.Errorf("%s has no base branch to measure the work against", scratch)
	}
	// The scratch's own branch is what gets fetched; the target's is what the
	// work lands on, and only defaults to the same name.
	on := gitdiff.Branch(ctx, scratch)
	if on == "" || on == base {
		return Result{}, fmt.Errorf("the scratch is on %s rather than a branch of its own, so there is no work to take off it", base)
	}
	if branch = strings.TrimSpace(branch); branch == "" {
		branch = on
	}

	committed, err := commitTheRest(ctx, scratch)
	if err != nil {
		return Result{}, err
	}
	carry, err := commits(ctx, scratch, base)
	if err != nil {
		return Result{}, err
	}
	if len(carry) == 0 {
		return Result{}, fmt.Errorf("there is nothing in %s above %s to port", scratch, base)
	}

	path := gitdiff.WorktreePath(repo, branch)
	if err := gitdiff.AddWorktreeFrom(ctx, repo, path, branch, gitdiff.BaseBranch(ctx, repo)); err != nil {
		return Result{}, err
	}
	if _, err := worktrees.Sow(ctx, repo, path); err != nil {
		slog.Warn("some of the worktree seed did not carry", "path", path, "err", err)
	}

	result := Result{Path: path, Branch: branch, Repo: repo, Commits: len(carry), Committed: committed}
	// The objects have to be in the target's repository before anything can be
	// picked from them; the two share no remote.
	if _, err := git(ctx, path, "fetch", "--no-tags", scratch, on); err != nil {
		return result, undo(ctx, repo, path, err)
	}
	if out, err := git(ctx, path, append([]string{"cherry-pick", "--allow-empty"}, carry...)...); err != nil {
		// The worktree stays: a conflict is something to finish by hand, and
		// throwing the checkout away would take the half-applied work with it.
		return result, fmt.Errorf("the work did not apply cleanly onto %s.\n"+
			"Resolve it in %s and run `git cherry-pick --continue`.\n%s", branch, path, strings.TrimSpace(out))
	}
	return result, nil
}

// Remove deletes a scratch checkout once its work is somewhere else. It only
// ever removes a scratch, so a mistyped path cannot take a repository with it.
func Remove(ctx context.Context, dir string) error {
	scratch, err := gitdiff.RepoRoot(ctx, dir)
	if err != nil {
		return fmt.Errorf("%s is not a checkout", dir)
	}
	if !settings.IsScratch(scratch) {
		return fmt.Errorf("%s is not a scratch", scratch)
	}
	if err := os.RemoveAll(scratch); err != nil {
		return fmt.Errorf("remove %s: %w", scratch, err)
	}
	return nil
}

// commitTheRest folds anything uncommitted into a commit to carry it.
func commitTheRest(ctx context.Context, scratch string) (bool, error) {
	out, err := git(ctx, scratch, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(out) == "" {
		return false, nil
	}
	if _, err := git(ctx, scratch, "add", "-A"); err != nil {
		return false, err
	}
	if _, err := git(ctx, scratch, "commit", "-m", wip); err != nil {
		return false, err
	}
	return true, nil
}

// commits are the scratch's, oldest first, so they land in order.
func commits(ctx context.Context, scratch, base string) ([]string, error) {
	out, err := git(ctx, scratch, "rev-list", "--reverse", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// undo takes the worktree back out when nothing was applied to it.
func undo(ctx context.Context, repo, path string, cause error) error {
	if _, err := git(ctx, repo, "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("%w (and %s was left behind)", cause, path)
	}
	return cause
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- ade's own arguments
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s in %s: %w: %s", args[0], dir, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

package gitdiff

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Switching branch in place. Every stash here is named by its commit: the
// stack is shared, so stash@{0} is somebody else's by the time it is used.

// ErrLocalChanges is a checkout git refused over tracked changes.
var ErrLocalChanges = errors.New("the checkout would overwrite local changes")

// CheckoutBranch switches dir onto a local branch. Uncommitted work comes with
// it wherever git allows that.
func CheckoutBranch(ctx context.Context, dir, name string) error {
	if _, err := run(ctx, dir, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a branch name git will take", name)
	}
	if _, err := run(ctx, dir, "checkout", name); err != nil {
		if strings.Contains(err.Error(), "local changes to the following files") {
			return ErrLocalChanges
		}
		return err
	}
	return nil
}

// StashKeep sets tracked changes aside and returns the entry's commit, empty
// when there was nothing to set aside. Untracked files are left alone.
func StashKeep(ctx context.Context, dir, label string) (string, error) {
	was := stashTop(ctx, dir)
	if _, err := run(ctx, dir, "stash", "push", "--quiet", "-m", label); err != nil {
		return "", fmt.Errorf("stash: %w", err)
	}
	now := stashTop(ctx, dir)
	if now == "" || now == was {
		return "", nil
	}
	return now, nil
}

// StashApplyAt puts one entry back and says what conflicted; a conflicted
// apply leaves the entry alone.
func StashApplyAt(ctx context.Context, dir, sha string) ([]string, error) {
	if sha == "" {
		return nil, errors.New("no stash entry named")
	}
	if !StashHeld(ctx, dir, sha) {
		return nil, fmt.Errorf("stash %s is no longer in the stash", short(sha))
	}
	if _, err := run(ctx, dir, "stash", "apply", "--quiet", sha); err == nil {
		return nil, nil
	}
	stuck, err := ConflictedFiles(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("put the stashed work back: %w", err)
	}
	if len(stuck) == 0 {
		return nil, errors.New("the stashed work would not come back")
	}
	return stuck, nil
}

// StashDropAt removes one entry by its commit. One already gone is not a
// failure: the record of it outlives the stash.
func StashDropAt(ctx context.Context, dir, sha string) error {
	at, ok := stashAt(ctx, dir, sha)
	if !ok {
		return nil
	}
	if _, err := run(ctx, dir, "stash", "drop", "--quiet", at); err != nil {
		return fmt.Errorf("drop the stash: %w", err)
	}
	return nil
}

// StashFiles is what one entry holds.
func StashFiles(ctx context.Context, dir, sha string) []string {
	out, err := run(ctx, dir, "stash", "show", "--name-only", "--format=", sha)
	if err != nil {
		return nil
	}
	var files []string
	for line := range strings.SplitSeq(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			files = append(files, name)
		}
	}
	return files
}

// StashHeld reports whether the stash still holds that entry.
func StashHeld(ctx context.Context, dir, sha string) bool {
	_, ok := stashAt(ctx, dir, sha)
	return ok
}

// stashAt is the stash@{n} an entry is at now. Looked up every time.
func stashAt(ctx context.Context, dir, sha string) (string, bool) {
	if sha == "" {
		return "", false
	}
	out, err := run(ctx, dir, "stash", "list", "--format=%H %gd")
	if err != nil {
		return "", false
	}
	for line := range strings.SplitSeq(out, "\n") {
		held, at, found := strings.Cut(strings.TrimSpace(line), " ")
		if found && held == sha {
			return at, true
		}
	}
	return "", false
}

func stashTop(ctx context.Context, dir string) string {
	out, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/stash")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// LocalBranch is one branch of this repository.
type LocalBranch struct {
	Name string `json:"name"`
	// At is how long ago its last commit was, in git's own words.
	At string `json:"at,omitempty"`
}

// BranchesByDate lists the local branches, most recently committed to first.
func BranchesByDate(ctx context.Context, dir string) ([]LocalBranch, error) {
	out, err := run(ctx, dir,
		"for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)\t%(committerdate:relative)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	var found []LocalBranch
	for line := range strings.SplitSeq(out, "\n") {
		name, at, _ := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		if name = strings.TrimSpace(name); name != "" {
			found = append(found, LocalBranch{Name: name, At: strings.TrimSpace(at)})
		}
	}
	return found, nil
}

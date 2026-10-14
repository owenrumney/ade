package gitdiff

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Rev is one commit of a range, enough to name it in a list.
type Rev struct {
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`

	// PatchID keys the commit by its diff: a cherry-pick of it carries the
	// same value under another SHA. Empty for a merge.
	PatchID string `json:"patchId,omitempty"`
}

// commitFormat is NUL-separated so a subject containing anything still parses.
const commitFormat = "--format=%H%x00%ct%x00%an%x00%s"

// Resolve is the commit a ref names. A cache keyed by a branch name goes
// stale when the branch moves; keyed by this it cannot.
func Resolve(ctx context.Context, dir, ref string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", ref, err)
	}
	sha := strings.TrimSpace(out)
	if sha == "" {
		return "", fmt.Errorf("resolve %s: no such commit", ref)
	}
	return sha, nil
}

// Commits lists what head has and base does not, newest first, each with its
// patch-id. Two calls whatever the range.
func Commits(ctx context.Context, dir, base, head string) ([]Rev, error) {
	if base == "" || head == "" {
		return nil, fmt.Errorf("commits need a base and a head")
	}
	out, err := run(ctx, dir, "log", commitFormat, base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("list %s..%s: %w", base, head, err)
	}
	commits := parseCommits(out)
	if len(commits) == 0 {
		return nil, nil
	}

	ids, err := PatchIDs(ctx, dir, base, head)
	if err != nil {
		return nil, err
	}
	for i := range commits {
		commits[i].PatchID = ids[commits[i].SHA]
	}
	return commits, nil
}

// PatchIDs keys every commit of base..head by its diff in one pass: patch-id
// answers a line per commit of the stream, so the log carries the SHA.
func PatchIDs(ctx context.Context, dir, base, head string) (map[string]string, error) {
	log, err := run(ctx, dir, "log", "-p", "--no-color", "--no-ext-diff",
		"--format=commit %H", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("read patches of %s..%s: %w", base, head, err)
	}
	if strings.TrimSpace(log) == "" {
		return map[string]string{}, nil
	}
	out, err := runStdin(ctx, dir, log, "patch-id", "--stable")
	if err != nil {
		return nil, fmt.Errorf("patch-id %s..%s: %w", base, head, err)
	}

	ids := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		id, sha, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && id != "" {
			ids[strings.TrimSpace(sha)] = id
		}
	}
	return ids, nil
}

func parseCommits(out string) []Rev {
	var commits []Rev
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 4 {
			continue
		}
		c := Rev{SHA: parts[0], Author: parts[2], Subject: parts[3]}
		if secs, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			c.At = time.Unix(secs, 0).UTC()
		}
		commits = append(commits, c)
	}
	return commits
}

// StartMerge begins a merge and says what is left conflicted. No conflicts and
// no error means it landed; an error means nothing was started.
func StartMerge(ctx context.Context, dir, branch string) ([]string, error) {
	if _, err := run(ctx, dir, "merge", "--no-edit", "--no-ff", branch); err == nil {
		return nil, nil
	}
	stuck, err := ConflictedFiles(ctx, dir)
	if err != nil || len(stuck) == 0 {
		// Not a conflict: something else refused, and a half-started merge is
		// worse than the refusal.
		_ = AbortMerge(ctx, dir)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("merge %s failed", branch)
	}
	return stuck, nil
}

// ConflictedFiles is what the merge could not decide, as repo-relative paths.
func ConflictedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, fmt.Errorf("list conflicts: %w", err)
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// AbortMerge puts the tree back as it was.
func AbortMerge(ctx context.Context, dir string) error {
	if _, err := run(ctx, dir, "merge", "--abort"); err != nil {
		return fmt.Errorf("git merge --abort: %w", err)
	}
	return nil
}

// FinishMerge commits a merge whose conflicts have all been staged.
func FinishMerge(ctx context.Context, dir, message string) error {
	if _, err := run(ctx, dir, "commit", "--no-verify", "-m", message); err != nil {
		return fmt.Errorf("commit the merge: %w", err)
	}
	return nil
}

// MergeInto is the whole of a merge that is expected to apply cleanly: it
// undoes the attempt when it does not, so no conflict is ever left behind.
func MergeInto(ctx context.Context, dir, branch string) error {
	stuck, err := StartMerge(ctx, dir, branch)
	if err != nil {
		return err
	}
	if len(stuck) == 0 {
		return nil
	}
	if abortErr := AbortMerge(ctx, dir); abortErr != nil {
		return fmt.Errorf("%s conflicts and the merge could not be undone: %w", branch, abortErr)
	}
	return fmt.Errorf("%s conflicts in %s; nothing was merged", branch, strings.Join(stuck, ", "))
}

// LandedIn is whether the branch's work is already on the base under other
// SHAs. Both shapes a forge leaves: commit by commit, or folded into one.
func LandedIn(ctx context.Context, dir, base, branch string) (bool, error) {
	// `git cherry` marks `-` a commit whose patch is upstream, `+` one that is not.
	out, err := run(ctx, dir, "cherry", base, branch)
	if err != nil {
		return false, fmt.Errorf("compare %s with %s: %w", branch, base, err)
	}
	loose := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "+") {
			loose = true
			break
		}
	}
	if !loose {
		return true, nil
	}
	return squashedInto(ctx, dir, base, branch)
}

// squashedInto is the other shape: several commits folded into one, whose
// patch is the branch's whole diff and so matches none of them on its own.
func squashedInto(ctx context.Context, dir, base, branch string) (bool, error) {
	fork, err := run(ctx, dir, "merge-base", base, branch)
	if err != nil {
		return false, nil
	}
	fork = strings.TrimSpace(fork)
	whole, err := run(ctx, dir, "diff", "--no-color", "--no-ext-diff", fork, branch)
	if err != nil || strings.TrimSpace(whole) == "" {
		return false, nil
	}
	mine, err := runStdin(ctx, dir, whole, "patch-id", "--stable")
	if err != nil {
		return false, nil
	}
	id, _, _ := strings.Cut(strings.TrimSpace(mine), " ")
	if id == "" {
		return false, nil
	}
	ids, err := PatchIDs(ctx, dir, fork, base)
	if err != nil {
		return false, nil
	}
	for _, theirs := range ids {
		if theirs == id {
			return true, nil
		}
	}
	return false, nil
}

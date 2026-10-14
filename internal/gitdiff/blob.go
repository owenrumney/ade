package gitdiff

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sides of a diff: what the file was, and what it is now.
const (
	SideLeft  = "left"
	SideRight = "right"
)

// ErrNotInDiff is a path that cannot be read as part of this diff: outside the
// repository, or not a side it has.
var ErrNotInDiff = errors.New("that is not a file in this diff")

// Blob is one file as one side of the current diff shows it. A side a file
// does not have reads as empty, which is what a diff viewer wants.
func Blob(ctx context.Context, root string, opts Options, path, side string) ([]byte, error) {
	clean, err := inside(path)
	if err != nil {
		return nil, err
	}
	left, right, err := refs(ctx, root, opts)
	if err != nil {
		return nil, err
	}
	ref := left
	if side == SideRight {
		ref = right
	}
	// An empty ref is the working tree, which is the right side of every
	// scope but committed.
	if ref == "" {
		return worktreeFile(root, clean)
	}
	out, err := run(ctx, root, "show", ref+":"+clean)
	if err != nil {
		// Only git saying the path is not in that commit reads as an empty
		// side; a bad ref or a cancelled context is a failure to report.
		if missingInRef(err) {
			return nil, nil
		}
		return nil, err
	}
	return []byte(out), nil
}

// missingInRef is git's way of saying the path is not in that commit, which is
// how an added file reads on the left.
func missingInRef(err error) bool {
	said := strings.ToLower(err.Error())
	return strings.Contains(said, "does not exist in") ||
		strings.Contains(said, "exists on disk, but not in")
}

// refs are the two ends of the diff a scope produces. An empty right means the
// working tree rather than a commit.
func refs(ctx context.Context, root string, opts Options) (left, right string, err error) {
	_, mode, base, _, err := resolveSpec(ctx, root, opts)
	if err != nil {
		return "", "", err
	}
	switch mode {
	case ModeCommitted:
		return base, "HEAD", nil
	case ModeUncommitted:
		return "HEAD", "", nil
	case ModeRev:
		// A range names both ends: `git diff A..B` compares two commits, and
		// the working tree is no side of it.
		if from, to, ok := rangeEnds(ctx, root, opts.Rev); ok {
			return from, to, nil
		}
		return opts.Rev, "", nil
	default:
		return base, "", nil
	}
}

// rangeEnds splits a rev range into the two commits it compares. `A...B` is
// the merge base against B, which is what git itself diffs.
func rangeEnds(ctx context.Context, root, rev string) (from, to string, ok bool) {
	if a, b, found := strings.Cut(rev, "..."); found && a != "" && b != "" {
		base, err := run(ctx, root, "merge-base", a, b)
		if err != nil {
			return a, b, true
		}
		return strings.TrimSpace(base), b, true
	}
	if a, b, found := strings.Cut(rev, ".."); found && a != "" && b != "" {
		return a, b, true
	}
	return "", "", false
}

func worktreeFile(root, path string) ([]byte, error) {
	full := filepath.Join(root, path)
	// inside() is lexical, so a symlink in the checkout still names a file
	// outside it. os.ReadFile would follow it.
	if !under(root, full) {
		return nil, ErrNotInDiff
	}
	data, err := os.ReadFile(full) // #nosec G304 -- under() pinned it inside root
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// under resolves the symlinks on a path and reports whether it is still in the
// repository. A path that does not exist resolves to nothing; the read says so.
func under(root, full string) bool {
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return true
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		base = root
	}
	rel, err := filepath.Rel(base, resolved)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// inside is the path as git names it, refused unless it stays in the
// repository. A diff's own paths always do; a request's may not.
func inside(path string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if clean == "" || clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", ErrNotInDiff
	}
	return clean, nil
}

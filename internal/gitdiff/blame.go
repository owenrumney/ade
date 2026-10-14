package gitdiff

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// uncommittedSHA is what blame reports for a line that is not committed yet.
const uncommittedSHA = "0000000000000000000000000000000000000000"

// Blame is the commit a line last came from.
type Blame struct {
	SHA     string
	Short   string
	Summary string
	Author  string
	At      time.Time
}

// Committed reports whether the line is in a commit at all, as opposed to
// still being in the worktree or the index.
func (b Blame) Committed() bool {
	return b.SHA != "" && b.SHA != uncommittedSHA
}

// BlameLine is the commit that last touched one line of a file.
func BlameLine(ctx context.Context, dir, file string, line int) (Blame, error) {
	spec := fmt.Sprintf("%d,%d", line, line)
	out, err := run(ctx, dir, "blame", "--porcelain", "-L", spec, "--", file)
	if err != nil {
		return Blame{}, fmt.Errorf("blame %s:%d: %w", file, line, err)
	}
	return parseBlame(out), nil
}

// AddedLines is the content of the lines a commit added, keyed by file. A
// merge commit shows no diff of its own, so it adds nothing here.
func AddedLines(ctx context.Context, dir, sha string) (map[string][]string, error) {
	out, err := run(ctx, dir, "show", "--format=", "--no-color", "--no-ext-diff",
		"--find-renames", "-U0", sha)
	if err != nil {
		return nil, fmt.Errorf("read commit %s: %w", sha, err)
	}

	added := map[string][]string{}
	for _, file := range Parse(out) {
		for _, hunk := range file.Hunks {
			for _, row := range hunk.Rows {
				if row.Right != nil && row.Right.Kind == KindAdd {
					added[file.Path] = append(added[file.Path], row.Right.Content)
				}
			}
		}
	}
	return added, nil
}

// ConfigValue reads one git config key, empty when it is unset.
func ConfigValue(ctx context.Context, dir, key string) string {
	out, err := run(ctx, dir, "config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// LocalConfigValue reads a key from this repository's own config only. A key
// that must be decided per repository cannot be answered by ~/.gitconfig.
func LocalConfigValue(ctx context.Context, dir, key string) string {
	out, err := run(ctx, dir, "config", "--local", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func parseBlame(out string) Blame {
	var b Blame
	for i, line := range strings.Split(out, "\n") {
		switch {
		case i == 0:
			b.SHA = strings.Fields(line + " ")[0]
			if len(b.SHA) >= 7 {
				b.Short = b.SHA[:7]
			}
		case strings.HasPrefix(line, "author "):
			b.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "summary "):
			b.Summary = strings.TrimPrefix(line, "summary ")
		case strings.HasPrefix(line, "author-time "):
			if secs, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
				b.At = time.Unix(secs, 0)
			}
		}
	}
	return b
}

// PatchID keys a commit by its diff with line numbers ignored, so a clean
// rebase does not change it. Empty for a merge, which has no diff.
func PatchID(ctx context.Context, dir, sha string) (string, error) {
	show, err := run(ctx, dir, "show", "--format=", "--no-color", "--no-ext-diff", sha)
	if err != nil {
		return "", fmt.Errorf("read commit %s: %w", sha, err)
	}
	if strings.TrimSpace(show) == "" {
		return "", nil
	}
	out, err := runStdin(ctx, dir, show, "patch-id", "--stable")
	if err != nil {
		return "", fmt.Errorf("patch-id %s: %w", sha, err)
	}
	id, _, _ := strings.Cut(strings.TrimSpace(out), " ")
	return id, nil
}

// CommitSubject is a commit's first message line.
func CommitSubject(ctx context.Context, dir, sha string) string {
	out, err := run(ctx, dir, "show", "--no-patch", "--format=%s", sha)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

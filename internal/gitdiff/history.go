package gitdiff

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Past is one commit that touched a file, under the name it had then. A
// rename makes Path differ from the one asked about; ShowAt needs this one.
type Past struct {
	Rev
	Path string `json:"path"`
}

// historyCap is how far back a file's list goes. The pick is for finding the
// change you half-remember, not for reading the whole life of a file.
const historyCap = 200

// History is the commits that touched one file, newest first, following it
// through renames.
func History(ctx context.Context, dir, path string, limit int) ([]Past, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("history needs a file")
	}
	if limit <= 0 || limit > historyCap {
		limit = historyCap
	}
	// --follow takes exactly one path, which is what makes Path per-commit
	// answerable at all.
	out, err := run(ctx, dir, "log", "--follow", commitFormat, "--name-only",
		"-n", strconv.Itoa(limit), "--", path)
	if err != nil {
		return nil, fmt.Errorf("history of %s: %w", path, err)
	}
	past := parseHistory(out)
	// A merge names no files of its own, so it carries no path to read at.
	for i := range past {
		if past[i].Path == "" {
			past[i].Path = path
		}
	}
	return past, nil
}

// ShowAt is a file's content as of one commit, under the name it had then.
func ShowAt(ctx context.Context, dir, sha, path string) (string, error) {
	out, err := run(ctx, dir, "show", "--no-textconv", sha+":"+path)
	if err != nil {
		return "", fmt.Errorf("read %s at %s: %w", path, sha, err)
	}
	return out, nil
}

// parseHistory reads --format then --name-only: a header line carries NULs, a
// bare line after it is the path the file had at that commit.
func parseHistory(out string) []Past {
	var past []Past
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, "\x00") {
			if n := len(past); n > 0 && past[n-1].Path == "" {
				past[n-1].Path = line
			}
			continue
		}
		parts := strings.Split(line, "\x00")
		if len(parts) < 4 {
			continue
		}
		rev := Rev{SHA: parts[0], Author: parts[2], Subject: parts[3]}
		if secs, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			rev.At = time.Unix(secs, 0).UTC()
		}
		past = append(past, Past{Rev: rev})
	}
	return past
}

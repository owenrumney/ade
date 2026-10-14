package lineage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Span is a run of lines in the after text.
type Span struct {
	Start int
	Lines int
}

// hunkHeader picks the after-side range out of a unified diff hunk header. With
// -U0 the range is exactly the changed lines, with no context around them.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// Changed is the lines of after that differ from before. Deletions produce no
// span: there is no line left to attribute.
func Changed(ctx context.Context, before, after string) ([]Span, error) {
	if before == after {
		return nil, nil
	}

	dir, err := os.MkdirTemp("", "ade-lineage-")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	a, b := filepath.Join(dir, "before"), filepath.Join(dir, "after")
	if err := os.WriteFile(a, []byte(before), 0o600); err != nil {
		return nil, fmt.Errorf("write before: %w", err)
	}
	if err := os.WriteFile(b, []byte(after), 0o600); err != nil {
		return nil, fmt.Errorf("write after: %w", err)
	}

	// git diff exits 1 when the files differ, which is the whole point of
	// calling it, so only a missing diff is an error.
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--no-color", "-U0", "--", a, b)
	out, err := cmd.Output()
	if len(out) == 0 && err != nil {
		return nil, fmt.Errorf("diff for lineage: %w", err)
	}
	return parseSpans(string(out)), nil
}

func parseSpans(diff string) []Span {
	var out []Span
	for _, line := range strings.Split(diff, "\n") {
		m := hunkHeader.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		lines := 1
		if m[2] != "" {
			lines, _ = strconv.Atoi(m[2])
		}
		if lines == 0 {
			continue
		}
		out = append(out, Span{Start: start, Lines: lines})
	}
	return out
}

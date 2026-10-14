// Package resolve turns review threads into an agent instruction and reads back
// what the agent did with each one.
package resolve

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/prompt"
)

// Outcome statuses the agent may report for a thread.
const (
	StatusFixed    = "fixed"
	StatusDeclined = "declined"
	StatusUnclear  = "unclear"
)

// maxHunkRows caps how much of a hunk is quoted per thread so a large review
// still fits in one turn.
const maxHunkRows = 40

var fenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*(\\[.*?\\])\\s*```")

// Outcome is the agent's verdict on one thread. Only StatusFixed resolves it;
// anything else leaves the thread open for the user to reply to.
type Outcome struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

// Resolves reports whether this outcome should close its thread.
func (o Outcome) Resolves() bool { return o.Status == StatusFixed }

// Tools is what applying a comment needs: read the code, change the code.
// Not Bash and not WebFetch. See the design doc.
func Tools() []string {
	return []string{"Read", "Grep", "Glob", "Edit", "Write", "MultiEdit"}
}

// BuildPrompt renders one instruction covering every thread.
func BuildPrompt(diff *gitdiff.Diff, threads []comments.Thread) string {
	var b strings.Builder

	b.WriteString(`You are applying review comments left on a diff of the current working tree.

For each comment below, either make the change in the working tree or say why you
disagree. Rules:

- Edit files in place. Do NOT run git commit, git add, or git stash, and do not
  create branches. The user commits their own work.
- Where a comment is wrong or you disagree, leave the code alone and say so. A
  declined comment is a useful answer; a silent non-change is not.
- Keep changes to what each comment asks for. Do not refactor around them.
- Comments marked "removed line" refer to code deleted by this diff, so there is
  no line to edit; treat them as questions about the removal.

` + prompt.Untrusted + `

Comments:

`)

	for i, thread := range threads {
		b.WriteString(renderThread(diff, i+1, thread))
		b.WriteString("\n")
	}

	b.WriteString(`When you are done, end your reply with a single fenced JSON block reporting one
entry per comment id, and nothing after it:

` + "```json" + `
[{"id": "<id>", "status": "fixed|declined|unclear", "note": "one sentence"}]
` + "```" + `

Use "fixed" only if you actually changed the code. Use "declined" if you
disagree, and "unclear" if you need the reviewer to clarify before acting.
`)

	return b.String()
}

// ParseOutcomes reads the agent's trailing JSON block. It tolerates the block
// being absent, since a turn can still have done useful work.
func ParseOutcomes(text string) ([]Outcome, error) {
	matches := fenceRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no JSON outcome block in the agent reply")
	}

	// The report is specified to come last, so prefer the final block.
	raw := matches[len(matches)-1][1]
	var outcomes []Outcome
	if err := json.Unmarshal([]byte(raw), &outcomes); err != nil {
		return nil, fmt.Errorf("parse outcome block: %w", err)
	}

	for i := range outcomes {
		outcomes[i].Status = strings.ToLower(strings.TrimSpace(outcomes[i].Status))
		switch outcomes[i].Status {
		case StatusFixed, StatusDeclined, StatusUnclear:
		default:
			outcomes[i].Status = StatusUnclear
		}
	}
	return outcomes, nil
}

func renderThread(diff *gitdiff.Diff, n int, thread comments.Thread) string {
	var b strings.Builder

	location := fmt.Sprintf("%s line %d", thread.File, thread.Line)
	switch {
	case thread.Scope == comments.ScopeRepo:
		location = "the change as a whole (not tied to a line)"
	case thread.Side == comments.SideLeft:
		location = fmt.Sprintf("%s removed line (was line %d before this change)", thread.File, thread.Line)
	}
	fmt.Fprintf(&b, "--- comment %d (id %s) on %s ---\n", n, thread.ID, location)

	if thread.Orphaned {
		fmt.Fprintf(&b, "NOTE: the code this was pinned to has changed. It used to read:\n    %s\n", thread.Anchor.Content)
	}
	if hunk := quoteHunk(diff, thread); hunk != "" {
		b.WriteString(hunk)
	}

	b.WriteString("\nThread:\n")
	for _, comment := range thread.Comments {
		fmt.Fprintf(&b, "%s\n", prompt.Comment(comment.Author, comment.Body))
	}
	return b.String()
}

// quoteHunk renders the hunk containing the thread's line, marking that line.
func quoteHunk(diff *gitdiff.Diff, thread comments.Thread) string {
	file := findFile(diff, thread.File)
	if file == nil {
		return ""
	}
	for _, hunk := range file.Hunks {
		if !hunkCovers(hunk, thread) {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "\n%s %s\n", hunk.Header, hunk.Heading)
		for i, row := range hunk.Rows {
			if i >= maxHunkRows {
				b.WriteString("    … hunk truncated\n")
				break
			}
			b.WriteString(renderRow(row, thread))
		}
		return b.String()
	}
	return ""
}

// renderRow flattens a side-by-side row back to unified lines. A changed row
// carries both a deletion and an addition, so it renders as two lines.
func renderRow(row gitdiff.Row, thread comments.Thread) string {
	if c := row.Left; c != nil && c.Kind == gitdiff.KindContext {
		// Context is identical on both sides, so it appears once.
		return diffLine(" ", c.Content, isAnchor(row, thread))
	}

	var b strings.Builder
	if c := row.Left; c != nil {
		b.WriteString(diffLine("-", c.Content, thread.Side == comments.SideLeft && c.Line == thread.Line))
	}
	if c := row.Right; c != nil {
		b.WriteString(diffLine("+", c.Content, thread.Side == comments.SideRight && c.Line == thread.Line))
	}
	return b.String()
}

func diffLine(sign, content string, anchored bool) string {
	marker := "    "
	if anchored {
		marker = ">>> "
	}
	return fmt.Sprintf("%s%s %s\n", marker, sign, content)
}

// isAnchor reports whether the row holds the line the thread is pinned to.
func isAnchor(row gitdiff.Row, thread comments.Thread) bool {
	cell := row.Right
	if thread.Side == comments.SideLeft {
		cell = row.Left
	}
	return cell != nil && cell.Line == thread.Line
}

func hunkCovers(hunk gitdiff.Hunk, thread comments.Thread) bool {
	for _, row := range hunk.Rows {
		if isAnchor(row, thread) {
			return true
		}
	}
	return false
}

func findFile(diff *gitdiff.Diff, path string) *gitdiff.File {
	if diff == nil {
		return nil
	}
	for i := range diff.Files {
		if diff.Files[i].Path == path {
			return &diff.Files[i]
		}
	}
	return nil
}

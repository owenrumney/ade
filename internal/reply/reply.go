// Package reply drafts an answer to a reviewer. It never sends one.
package reply

import (
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/prompt"
)

// maxHunkRows caps how much of the surrounding hunk is quoted.
const maxHunkRows = 30

// Prompt asks for what you would say back to a reviewer, not a change to the
// code. Resolve is the action that edits; this one is for words, and the two
// are kept apart because a comment that deserves an argument should not quietly
// become a commit.
func Prompt(diff *gitdiff.Diff, thread comments.Thread) string {
	var b strings.Builder

	b.WriteString(`Draft a reply to a review comment on this pull request.

You are writing what the author would say back, in their voice: first person,
plain, no greeting and no sign-off.

**Under 60 words.** Usually one or two sentences. A reviewer reads this in a
thread next to the code; they do not need the code explained back to them.

**Never open by agreeing, praising or amplifying.** Start with the answer.
Banned outright, in any position:
  "You're right" / "Good catch" / "Great point" / "Fair enough"
  "Absolutely" / "I appreciate" / "Thanks for"
  anything of the form "and it's worse than that" / "it was worse than that"

- Read the code the comment points at before answering. If the reviewer is
  right, say what you will change. That is the whole reply.
- If they are wrong, say why in one sentence, with the specific reason.
- If the comment is ambiguous enough that you would be guessing, ask the question
  instead of answering it.
- Do not restate the comment, summarise the thread, or list what you checked.
- Courteous means plain and direct. It does not mean warm, and it never means
  flattering the reviewer before disagreeing with them.
- Do NOT edit any file. This turn produces a reply and nothing else; making the
  change is a separate action the author takes deliberately.

` + prompt.Untrusted + `

`)

	fmt.Fprintf(&b, "The comment is on %s", thread.File)
	if thread.Line > 0 {
		fmt.Fprintf(&b, " line %d", thread.Line)
	}
	b.WriteString(":\n\n")

	for _, comment := range thread.Comments {
		fmt.Fprintf(&b, "%s\n\n", prompt.Comment(comment.Author, comment.Body))
	}

	if quoted := hunkAround(diff, thread); quoted != "" {
		fmt.Fprintf(&b, "The code it is about:\n\n<code>\n%s</code>\n\n", quoted)
	}

	b.WriteString("Reply with the message text alone.")
	return b.String()
}

// hunkAround quotes the hunk the comment sits in, so the agent answers about the
// change rather than about the file as it stands now.
func hunkAround(diff *gitdiff.Diff, thread comments.Thread) string {
	if diff == nil || thread.File == "" {
		return ""
	}
	for _, file := range diff.Files {
		if file.Path != thread.File {
			continue
		}
		for _, hunk := range file.Hunks {
			if !covers(hunk, thread) {
				continue
			}
			return render(hunk)
		}
	}
	return ""
}

func covers(hunk gitdiff.Hunk, thread comments.Thread) bool {
	for _, row := range hunk.Rows {
		if cell := side(row, thread.Side); cell != nil && cell.Line == thread.Line {
			return true
		}
	}
	return false
}

func side(row gitdiff.Row, want string) *gitdiff.Cell {
	if want == comments.SideLeft {
		return row.Left
	}
	return row.Right
}

func render(hunk gitdiff.Hunk) string {
	var b strings.Builder
	for i, row := range hunk.Rows {
		if i >= maxHunkRows {
			b.WriteString("…\n")
			break
		}
		switch {
		case row.Left != nil && row.Left.Kind == gitdiff.KindContext:
			fmt.Fprintf(&b, "  %s\n", row.Left.Content)
		case row.Left != nil:
			fmt.Fprintf(&b, "- %s\n", row.Left.Content)
			if row.Right != nil {
				fmt.Fprintf(&b, "+ %s\n", row.Right.Content)
			}
		case row.Right != nil:
			fmt.Fprintf(&b, "+ %s\n", row.Right.Content)
		}
	}
	return b.String()
}

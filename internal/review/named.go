package review

import (
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/prompt"
)

// FilingBlock is how a named reviewer files what it finds, as it finds it.
const FilingBlock = "\nFile each point as you find it: one fenced ade-finding block per finding, and it appears as a comment on that line of the diff. Do not wait until the end, and do not hand them to another agent.\n\n" +
	"```ade-finding\n" +
	`{"file": "internal/cart/cart.go", "line": 24, "category": "correctness", "severity": "high", "headline": "The cart total ignores the discount when the basket is empty.", "summary": "one sentence", "failure_scenario": "concrete inputs then wrong result"}` +
	"\n```\n\n" +
	"file is repository-relative; line is the new-file line shown in the left column. category is one of correctness, security, design, performance, test-coverage or style. severity is critical, high, medium or low. A point with no file and line cannot be filed — say it in one line instead, and say what you found in your reply as well.\n\n" +
	HeadlineRule

// NoArtifacts keeps a read-only agent read-only in fact.
const NoArtifacts = "Never write a build artifact into the checkout: no `go test -c`, no `go build -o`, no `npm run build`. `go build ./...` and `go vet ./...` compile without writing anything, and `go test ./...` cleans up after itself.\n"

// DiffReviewPrompt asks a named agent to review the rendered diff.
func DiffReviewPrompt(name, diffText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `You are @%s, called into a conversation in ade, where a user and an agent are working on this repository together. Your reply is shown in that conversation to both of them, so address it to the user.

Review the change below adversarially: what is wrong, what is missing, what is riskier than it looks. Anchor each point to a file and the new-file line number shown in the left column, and rank each one critical, high, medium or low so the reader knows where to start. Say nothing about what is fine. Keep it as short as it can be while saying what matters.
`, name)
	b.WriteString("\nDo not edit anything, and do not run git. " + NoArtifacts)
	b.WriteString(FilingBlock)
	b.WriteString("\n")
	b.WriteString(prompt.Fence("diff", diffText))
	return b.String()
}

// Claim is one finding put to a reviewer in a rebuttal round, by short ref.
type Claim struct {
	Ref      string
	By       string
	File     string
	Line     int
	Category string
	Text     string
}

// RebuttalPrompt asks one reviewer to judge the others' findings.
func RebuttalPrompt(name string, claims []Claim, diffText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `You are @%s. You have just reviewed the change below. Other reviewers reviewed the same change, and their findings are listed after it.

Judge each one. You are not reviewing the change again: no new findings, and nothing about the findings you filed yourself.

For each finding return exactly one fenced block, and nothing else about it:

`+"```"+`%s
{"ref": "F1", "verdict": "agree"}
`+"```"+`

`+"```"+`%s
{"ref": "F2", "verdict": "dispute", "reason": "one or two sentences saying why it is wrong"}
`+"```"+`

`+"```"+`%s
{"ref": "F3", "verdict": "duplicate-of", "of": "F1"}
`+"```"+`

- agree — the finding is right, or right enough to leave standing. Use this when unsure: a finding you cannot fault stays.
- dispute — the finding is wrong. The reason is filed as a reply on the comment, under your name, next to the claim it answers, so write it for the user to read. A dispute with no reason is discarded.
- duplicate-of — the same point as an earlier finding, by ref. Both stay on screen; the later one is folded into the first.

Read the code where you need to. Do not edit anything, and do not run git. Prose outside the blocks is ignored, so do not write any.
`, name, VerdictFence, VerdictFence, VerdictFence)

	b.WriteString("\n")
	b.WriteString(prompt.Fence("diff", diffText))
	b.WriteString("\n<findings>\n")
	for _, c := range claims {
		fmt.Fprintf(&b, "\n%s — @%s", c.Ref, c.By)
		if c.Category != "" {
			fmt.Fprintf(&b, " (%s)", c.Category)
		}
		fmt.Fprintf(&b, "\n%s:%d\n%s\n", c.File, c.Line, strings.TrimSpace(c.Text))
	}
	b.WriteString("</findings>\n")
	return b.String()
}

// ClusterPrompt asks which findings on one file are the same point.
func ClusterPrompt(file string, claims []Claim, diffText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `Several reviewers looked at the change below. These are the findings they reported about %s.

Say which of them are the same point in different words. Judge nothing else: no new findings, no opinion on whether any of them is right.

For each group of two or more, return exactly one fenced block:

`+"```"+`%s
{"same": ["F2", "F5"]}
`+"```"+`

- Put the clearest wording of the group first. It is the one the reader sees.
- A finding that is nobody else's point gets no block.
- Two findings on the same line that are about different things are two points. A null check and a slow loop on one line are not one point.
- Never put a finding in two groups.

Read the code where you need to. Do not edit anything, and do not run git. Prose outside the blocks is ignored, so do not write any.
`, file, ClusterFence)

	b.WriteString("\n")
	b.WriteString(prompt.Fence("diff", diffText))
	b.WriteString("\n<findings>\n")
	for _, c := range claims {
		fmt.Fprintf(&b, "\n%s — @%s", c.Ref, c.By)
		if c.Category != "" {
			fmt.Fprintf(&b, " (%s)", c.Category)
		}
		fmt.Fprintf(&b, "\n%s:%d\n%s\n", c.File, c.Line, strings.TrimSpace(c.Text))
	}
	b.WriteString("</findings>\n")
	return b.String()
}

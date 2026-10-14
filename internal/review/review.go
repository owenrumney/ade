// Package review runs a cold review agent and turns its findings into threads.
package review

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/prompt"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// reportTool is the tool Claude Code's code-review skill calls to file findings.
const reportTool = "ReportFindings"

// maxDiffChars caps the embedded diff so a huge branch still produces a prompt
// the agent will accept.
const maxDiffChars = 300000

// Effort levels accepted by the code-review skill.
var levels = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

var fenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*(\\[.*?\\]|\\{.*?\\})\\s*```")

// Finding mirrors one entry of the ReportFindings payload.
type Finding struct {
	File string `json:"file"`
	Line int    `json:"line"`

	// Headline is the finding in one plain sentence; the rest folds behind it.
	Headline string `json:"headline"`

	Summary         string `json:"summary"`
	ShortSummary    string `json:"short_summary"`
	FailureScenario string `json:"failure_scenario"`
	Category        string `json:"category"`
	Severity        string `json:"severity"`
	Verdict         string `json:"verdict"`
}

type report struct {
	Findings []Finding `json:"findings"`
	Level    string    `json:"level"`
}

// Prompt returns the instruction for a cold review run. Embedding the diff means
// the agent reviews exactly what the browser shows, and needs no git access.
func Prompt(level, diffText string) string {
	if !levels[level] {
		level = "medium"
	}
	return fmt.Sprintf(`/code-review %s

The diff under review is included below, already annotated with the line numbers
of the new file. Review it directly; do not run git. You may read other files in
the repository for context, but do not edit anything.

Report each finding against the new-file line number shown in the left column,
and give each one a severity of critical, high, medium or low, so the reader
knows what to look at first.

`+HeadlineRule+`

%s

As soon as you confirm a finding, emit it immediately in its own fenced block
tagged %s, one JSON object per block, before moving on to the next one:

`+"```"+`%s
{"file": "internal/cart/cart.go", "line": 24, "category": "correctness", "severity": "high", "headline": "The cart total ignores the discount when the basket is empty.", "summary": "one sentence", "failure_scenario": "concrete inputs then wrong result"}
`+"```"+`

Then output a single fenced JSON block as the very last thing in your reply,
repeating every finding and with nothing after it:

`+"```json"+`
[{"file": "internal/cart/cart.go", "line": 24, "category": "correctness", "severity": "high", "headline": "The cart total ignores the discount when the basket is empty.", "summary": "one sentence", "failure_scenario": "concrete inputs then wrong result"}]
`+"```"+`

Use paths exactly as they appear in the diff headers above, and an empty array if
there are no findings.`, level, prompt.Fence("diff", diffText), streamFence, streamFence)
}

// RenderDiff writes the diff with new-file line numbers in the left column, so a
// finding's reported line can be anchored without guesswork.
func RenderDiff(diff *gitdiff.Diff) string {
	if diff == nil {
		return ""
	}
	var b strings.Builder
	for _, file := range diff.Files {
		if b.Len() > maxDiffChars {
			fmt.Fprintf(&b, "\n=== %s and further files omitted: diff too large ===\n", file.Path)
			break
		}
		writeFile(&b, file)
	}
	return b.String()
}

// maxSummaryFiles caps the file list. A branch that touched a thousand files
// still has to leave room for the question being asked about it.
const maxSummaryFiles = 60

// RenderSummary is the shape of a change rather than the whole of it: what it
// is measured against, which files moved and by how much, and the command that
// reads any of them.
//
// A named agent is called into a conversation, not handed a review, so it gets
// this and a shell. The whole diff would cost every agent the same whether it
// read a line of it or not, and RenderDiff's cap is 300k characters.
func RenderSummary(diff *gitdiff.Diff) string {
	if diff == nil || len(diff.Files) == 0 {
		return ""
	}
	base := strings.TrimSpace(diff.Base)
	if base == "" {
		base = "HEAD"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%d file(s) changed against %s", len(diff.Files), base)
	if note := strings.TrimSpace(diff.Note); note != "" {
		fmt.Fprintf(&b, " (%s)", note)
	}
	b.WriteString(".\n")
	fmt.Fprintf(&b, "Read any of it with: git diff %s -- <path>\n\n", base)

	for i, file := range diff.Files {
		if i == maxSummaryFiles {
			fmt.Fprintf(&b, "... and %d more\n", len(diff.Files)-i)
			break
		}
		fmt.Fprintf(&b, "%s (%s, +%d -%d)\n", file.Path, file.Status, file.Additions, file.Deletions)
	}
	return b.String()
}

func writeFile(b *strings.Builder, file gitdiff.File) {
	fmt.Fprintf(b, "\n=== %s (%s, +%d -%d) ===\n", file.Path, file.Status, file.Additions, file.Deletions)
	if file.Binary {
		b.WriteString("binary file\n")
		return
	}
	for _, hunk := range file.Hunks {
		fmt.Fprintf(b, "%s %s\n", hunk.Header, hunk.Heading)
		for _, row := range hunk.Rows {
			writeRow(b, row)
		}
	}
}

func writeRow(b *strings.Builder, row gitdiff.Row) {
	if c := row.Left; c != nil && c.Kind == gitdiff.KindContext {
		fmt.Fprintf(b, "%6d   %s\n", row.Right.Line, c.Content)
		return
	}
	// Deletions have no new-file line, so their number column stays blank.
	if c := row.Left; c != nil {
		fmt.Fprintf(b, "%6s - %s\n", "", c.Content)
	}
	if c := row.Right; c != nil {
		fmt.Fprintf(b, "%6d + %s\n", c.Line, c.Content)
	}
}

// ReadOnlyTools lets a reviewer read code but never change it. --allowedTools is
// an allowlist of plain tool names, so omitting Bash leaves it unable to run.
func ReadOnlyTools() []string {
	return []string{"Read", "Grep", "Glob"}
}

// Relative rewrites absolute paths to repository-relative ones, since the agent
// reports either form depending on how it happened to read the file.
func Relative(findings []Finding, root string) []Finding {
	root = strings.TrimSuffix(root, "/") + "/"
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		f.File = strings.TrimPrefix(strings.TrimSpace(f.File), root)
		f.File = strings.TrimPrefix(f.File, "./")
		out = append(out, f)
	}
	return out
}

// ParseTurn pulls findings out of a finished review turn, preferring the tool
// call and falling back to a fenced JSON block in the reply text. The bool
// reports whether a structured payload was found at all, which is what tells a
// deliberate "no findings" apart from a reply we could not read.
func ParseTurn(turn *bridge.Turn) ([]Finding, bool, error) {
	if turn == nil {
		return nil, false, fmt.Errorf("no review turn")
	}
	// Opportunistic: a future Claude Code may surface the tool call to us, which
	// is better structured than the text block we otherwise have to parse.
	if input, ok := bridge.FindToolUse(turn.Events, reportTool); ok {
		if findings, err := decodeReport(input); err == nil {
			return valid(findings), true, nil
		}
	}
	if findings, err := parseText(turn.Text); err == nil {
		return valid(findings), true, nil
	}
	if strings.TrimSpace(turn.Text) == "" {
		return nil, false, fmt.Errorf("review produced no findings and no reply")
	}
	return nil, false, nil
}

// HeadlineRule is how the one line a reader sees gets written. ASD-STE100 is
// the aviation standard for text that must be read the same way by everyone.
const HeadlineRule = `headline is the finding in ASD-STE100 Simplified Technical English. It is the line shown on the diff, and the only part most readers will read.

- One sentence, twenty words at most, one idea in it.
- Active voice, present tense: "the handler drops the error", not "the error is dropped" and not "would be dropped".
- Name the thing. Never "it", "this" or "the above" — repeat the identifier.
- One word for one meaning, the same word every time. Do not call it a cache here and a store in the summary.
- At most three nouns in a row. "the disk usage panel cache reading" is not a phrase.
- Keep the articles: "the token", not "token".
- Plain words, and no hedging. Say what is wrong, not what "may potentially be problematic".

Say the defect. Not its consequence, not its fix, not how you found it. summary and failure_scenario carry the rest and are shown under the headline, so write those normally.`

// Body is a thread's opening comment: the headline alone, then the detail.
// Plain text — the thread view renders a body as text, not as markup.
func Body(f Finding) string {
	var b strings.Builder
	if f.Verdict == "PLAUSIBLE" {
		b.WriteString("(plausible) ")
	}
	head := strings.TrimSpace(f.Headline)
	if head == "" {
		// Nothing short to lead with, so nothing to fold: the shape as it was.
		b.WriteString(strings.TrimSpace(summaryOf(f)))
		if scenario := strings.TrimSpace(f.FailureScenario); scenario != "" {
			b.WriteString("\n\nFails when: ")
			b.WriteString(scenario)
		}
		return b.String()
	}
	b.WriteString(head)

	detail := detailOf(f, head)
	if detail == "" {
		return b.String()
	}
	b.WriteString("\n\n")
	b.WriteString(detail)
	return b.String()
}

// detailOf is what the headline does not say, without repeating it.
func detailOf(f Finding, head string) string {
	var parts []string
	if summary := strings.TrimSpace(summaryOf(f)); summary != "" && summary != head {
		parts = append(parts, summary)
	}
	if scenario := strings.TrimSpace(f.FailureScenario); scenario != "" {
		parts = append(parts, "Fails when: "+scenario)
	}
	return strings.Join(parts, "\n\n")
}

func decodeReport(input json.RawMessage) ([]Finding, error) {
	var r report
	if err := json.Unmarshal(input, &r); err == nil && len(r.Findings) > 0 {
		return r.Findings, nil
	}
	var bare []Finding
	if err := json.Unmarshal(input, &bare); err == nil {
		return bare, nil
	}
	return nil, fmt.Errorf("unrecognised %s payload", reportTool)
}

func parseText(text string) ([]Finding, error) {
	matches := fenceRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no JSON block in the review reply")
	}
	raw := json.RawMessage(matches[len(matches)-1][1])
	return decodeReport(raw)
}

// valid drops findings with nothing to anchor to or nothing to say.
func valid(findings []Finding) []Finding {
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		f.File = strings.TrimSpace(f.File)
		if f.File == "" || f.Line <= 0 {
			continue
		}
		if strings.TrimSpace(summaryOf(f)) == "" {
			continue
		}
		f.Severity = comments.Severity(f.Severity)
		out = append(out, f)
	}
	return out
}

func summaryOf(f Finding) string {
	if s := strings.TrimSpace(f.Summary); s != "" {
		return s
	}
	return f.ShortSummary
}

// Package prcreate asks an agent to open a draft pull request for the branch.
package prcreate

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// fence tags the block the agent reports the finished URL in.
const fence = "ade-pr"

var fenceRE = regexp.MustCompile("(?s)```" + fence + "\\s*\\n(.*?)```")

// urlRE matches a pull request URL on any GitHub host, including Enterprise.
var urlRE = regexp.MustCompile(`https://[\w.-]+/[\w.-]+/[\w.-]+/pull/\d+`)

// templatePaths are where a repository keeps its pull request template, in the
// order GitHub itself resolves them.
var templatePaths = []string{
	".github/pull_request_template.md",
	".github/PULL_REQUEST_TEMPLATE.md",
	"PULL_REQUEST_TEMPLATE.md",
	"pull_request_template.md",
	"docs/pull_request_template.md",
	"docs/PULL_REQUEST_TEMPLATE.md",
}

// Template reads the repository's pull request template, if it has one.
func Template(root string) (string, bool) {
	for _, name := range templatePaths {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))) // #nosec G304 -- a fixed set of names under the repo
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(string(body)); text != "" {
			return text, true
		}
	}
	return "", false
}

// riskPlaceholderRE is the line a template leaves for a risk score. The wording
// belongs to the /review skill, which upserts over the same line later.
var riskPlaceholderRE = regexp.MustCompile(`(?im)^\s*-\s*\[ \]\s*TBC:.*risk score.*$`)

// riskWrittenRE is the placeholder answered: ticked, and carrying a score.
// Ticked with the placeholder text still on it is the box ticked for nothing.
var riskWrittenRE = regexp.MustCompile(`(?im)^\s*-\s*\[[xX]\].*risk score of \d{1,2}/10`)

// NeedsRiskScore reports whether the template asks for one.
func NeedsRiskScore(template string) bool {
	return riskPlaceholderRE.MatchString(template)
}

// riskGuidance is the scale, taken from the /review skill so a score written
// here means the same thing as one written there.
const riskGuidance = `
This template asks for a risk score. Score the change out of 10, interpolating
between these anchors:

- 1 — trivial doc/typo fix
- 3 — removes a deprecated feature
- 4 — adds a new tested feature
- 8 — changes a public API
- 9 — schema migration
- 10 — introduces a security vulnerability

Score the change as it stands, not what it was meant to be: one that fixes the
reported problem and introduces another has not gone down in risk. The
justification is one sentence — a score needing more than that is the wrong
score.

Replace the "TBC" risk score line with a line of exactly this shape, keeping it
where the placeholder was:

- [x] ` + "`" + `/review` + "`" + ` risk score of N/10, estimated when this PR was opened. <one sentence>.

This is the one box you may tick, and only because you are the one filling it
in. Say "estimated when this PR was opened" and nothing stronger: no review has
run yet, and a later ` + "`" + `/review` + "`" + ` replaces this line with its own.
`

// Tools lets the agent read the repository and run git and gh, which is what
// pushing the branch and opening the PR needs. Write is included because the
// skill writes the body to a file and passes it with --body-file rather than
// putting multiline markdown on a command line; without it the agent has to
// improvise with a heredoc, which is the fragile path the skill exists to avoid.
func Tools() []string {
	return []string{"Read", "Grep", "Glob", "Write", "Bash"}
}

// Prompt runs the pr-create skill and adds a machine-readable URL at the end.
//
// The template is read by ade and passed in rather than left for the skill to
// go and find. The skill does cover templates, but a checklist-only one was
// dropped silently in practice, and locating a file is not worth leaving to
// chance when we can just do it.
func Prompt(template string) string {
	var b strings.Builder

	b.WriteString("/pr-create\n\n" +
		"Follow the skill as written. If a pull request already exists for this branch,\n" +
		"report its URL rather than opening another one.\n")

	if template = strings.TrimSpace(template); template != "" {
		b.WriteString("\nThis repository has a pull request template, included below. The body you\n" +
			"submit must contain it.\n\n" +
			"- Where it has a description or summary section, put your prose there and\n" +
			"  remove the placeholder guidance.\n" +
			"- Where it is only a checklist, put your prose first and keep every line of\n" +
			"  the template below it, unchanged.\n" +
			"- Preserve its headings, trailers and checkboxes exactly, and never tick a\n" +
			"  box on the author's behalf.\n\n" +
			"<template>\n" + template + "\n</template>\n")
		if NeedsRiskScore(template) {
			b.WriteString(riskGuidance)
		}
	}

	fmt.Fprintf(&b, "\nThen, after whatever the skill asks you to report, end your reply with the\n"+
		"pull request URL in a single fenced block tagged %s, on its own line:\n\n"+
		"```%s\nhttps://github.com/owner/repo/pull/123\n```\n", fence, fence)

	return b.String()
}

// Kept reports whether a submitted body actually carries the template. Headings
// and checklist items are what is checked: prose inside a template is guidance
// the skill is meant to replace, not text to preserve.
func Kept(template, body string) bool {
	for _, line := range strings.Split(template, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "- [") {
			continue
		}
		// The risk placeholder is meant to be written over, but only by a line
		// that carries a score: a bare tick is the box ticked for nothing.
		if riskPlaceholderRE.MatchString(line) && riskWrittenRE.MatchString(body) {
			continue
		}
		if !strings.Contains(body, line) {
			return false
		}
	}
	return true
}

// ParseURL pulls the pull request URL out of the agent's reply, preferring the
// fenced block and falling back to the last URL mentioned anywhere.
func ParseURL(text string) (string, error) {
	if matches := fenceRE.FindAllStringSubmatch(text, -1); len(matches) > 0 {
		if url := urlRE.FindString(matches[len(matches)-1][1]); url != "" {
			return url, nil
		}
	}
	if found := urlRE.FindAllString(text, -1); len(found) > 0 {
		return found[len(found)-1], nil
	}
	return "", fmt.Errorf("no pull request URL in the agent reply")
}

// Number extracts the PR number from a URL, for display alongside the link.
func Number(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return ""
}

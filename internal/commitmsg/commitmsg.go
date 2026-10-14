// Package commitmsg asks an agent for a commit message. It never commits.
package commitmsg

import (
	"fmt"
	"regexp"
	"strings"
)

// fence is a distinct tag so a code fence inside the body cannot be mistaken for
// the end of the message.
const fence = "ade-commit"

var fenceRE = regexp.MustCompile("(?s)```" + fence + "\\s*\\n(.*?)```")

// titleRE matches a conventional-commit subject, which is the only thing safe to
// treat as a title when the fenced block is missing.
var titleRE = regexp.MustCompile(`^(feat|fix|refactor|docs|test|chore|perf|ci|build|style|revert)(\([^)]*\))?!?: .+`)

// skillRE is the CLI answering that /ctb is not installed here.
var skillRE = regexp.MustCompile(`(?i)unknown command:\s*/ctb`)

// MissingSkill is the CLI refusing the skill, not an agent writing badly.
func MissingSkill(text string) bool { return skillRE.MatchString(text) }

// Message is a conventional-commit title with an optional body.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Prompt wraps the ctb skill with an output contract we can parse. The skill
// reads the staged diff itself, so no diff is passed in.
func Prompt() string {
	return fmt.Sprintf(`/ctb

Then repeat the finished message as the very last thing in your reply, inside a
single fenced block tagged %s, with the title on the first line, a blank line,
and the body after it:

`+"```"+`%s
type(scope): imperative summary [KEY-123]

Body paragraph explaining what was wrong and what this changes.
`+"```"+`

Put nothing after that block. Do not run git commit, git add, or git stash, and
do not edit any files.`, fence, fence)
}

// Fallback is Prompt without the skill, for a machine that has no /ctb.
func Fallback() string {
	return fmt.Sprintf(`Read the staged diff with `+"`git diff --cached`"+` and write the commit message for it.

- A conventional-commit subject: type(scope): imperative summary. type is one of feat, fix, refactor, docs, test, chore, perf, ci, build, style or revert.
- Under 72 characters, no full stop, imperative mood: "add", not "added" or "adds".
- If the branch name carries a ticket key like KEY-123, put it at the end of the subject in square brackets.
- A body only when the subject cannot carry it: what was wrong, and what this changes. Wrap at 72 columns. No bullet list of files — the diff is the list of files.

Then repeat the finished message as the very last thing in your reply, inside a
single fenced block tagged %s, with the title on the first line, a blank line,
and the body after it:

`+"```"+`%s
type(scope): imperative summary [KEY-123]

Body paragraph explaining what was wrong and what this changes.
`+"```"+`

Put nothing after that block. Do not run git commit, git add, or git stash, and
do not edit any files.`, fence, fence)
}

// Tools lets the agent read the repository and run git, but not change anything.
// Plain Bash is required: the skill derives the message from the staged diff.
func Tools() []string {
	return []string{"Read", "Grep", "Glob", "Bash"}
}

// Parse pulls the message out of the agent's reply. Without the fenced block it
// only accepts a line that looks like a conventional-commit subject, since
// guessing at prose would put the agent's preamble in the commit title.
func Parse(text string) (Message, error) {
	if matches := fenceRE.FindAllStringSubmatch(text, -1); len(matches) > 0 {
		if msg, ok := split(matches[len(matches)-1][1]); ok {
			return msg, nil
		}
	}
	if msg, ok := findConventional(stripCodeFences(text)); ok {
		return msg, nil
	}
	return Message{}, fmt.Errorf("no commit message in the agent reply")
}

// findConventional locates the first conventional-commit subject line and treats
// what follows as the body.
func findConventional(text string) (Message, bool) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !titleRE.MatchString(trimmed) {
			continue
		}
		return Message{
			Title: trimmed,
			Body:  strings.TrimSpace(strings.Join(lines[i+1:], "\n")),
		}, true
	}
	return Message{}, false
}

// split takes the first non-empty line as the title and the rest as the body.
func split(block string) (Message, bool) {
	lines := strings.Split(strings.ReplaceAll(block, "\r\n", "\n"), "\n")

	title, rest := "", 0
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			title, rest = strings.TrimSpace(line), i+1
			break
		}
	}
	if title == "" {
		return Message{}, false
	}
	return Message{
		Title: title,
		Body:  strings.TrimSpace(strings.Join(lines[rest:], "\n")),
	}, true
}

// stripCodeFences drops fenced blocks so a fallback parse does not pick up a
// shell command the agent offered alongside the message.
func stripCodeFences(text string) string {
	var out []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

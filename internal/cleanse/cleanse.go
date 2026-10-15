// Package cleanse rewrites a reply short with a cheap model, after the turn:
// sharpen pointed the other way. The original is kept; every failure is no change.
package cleanse

import (
	"context"
	_ "embed"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Model is the cheap model that rewrites; the task is rewriting, not reasoning.
const Model = "claude-haiku-4-5-20251001"

// Timeout covers a cold claude start, in a container under a sandbox: nobody is
// waiting on the answer, and a late one is dropped.
const Timeout = 30 * time.Second

const floorRunes = 240

//go:embed role.md
var role string

// Role is the instruction the model runs under.
func Role() string { return strings.TrimSpace(role) }

// Runner sends one prompt to a model with no tools. Tests inject one.
type Runner func(ctx context.Context, system, prompt string) (string, error)

var (
	pathRE     = regexp.MustCompile(`^(?:[-*+]|\d+[.)])?\s*` + "`?" + `(?:(?:[\w.@-]+/)+[\w.@-]*|[\w-]+\.[A-Za-z]\w{0,5})(?::\d+(?::\d+)?)?` + "`?" + `\s*$`)
	preambleRE = regexp.MustCompile(`(?i)\b(rewrit\w*|concise|version|cleans\w*|shorten\w*|shorter|condensed)\b`)
	// keepRE is what a rewrite must carry verbatim: a code span, a url, a path, a file:line or a number.
	keepRE  = regexp.MustCompile("`[^`\n]+`" + `|https?://[^\s)>\]]+|(?:[\w.@~-]+/)+[\w.@-]*(?::\d+)*|\b[\w-]+\.[A-Za-z]\w{0,5}(?::\d+)+|\d+(?:[.,]\d+)*`)
	closeRE = regexp.MustCompile(`(?i)<\s*/\s*reply\s*>`)
)

// Wanted is the gate: long prose that is not mostly a fence, a table or a path list.
func Wanted(text string) bool {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) < floorRunes {
		return false
	}
	prose, structured, fenced := 0, 0, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n := utf8.RuneCountInString(line)
		switch {
		case fence(line):
			fenced = !fenced
			structured += n
		case fenced, strings.HasPrefix(line, "|"), pathRE.MatchString(line):
			structured += n
		default:
			prose += n
		}
	}
	return prose > structured
}

// Rewrite returns body rewritten short, or "" when the answer is not worth
// swapping in. A failure or a late answer is an error; either way, no change.
func Rewrite(ctx context.Context, run Runner, body string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	// A closing tag quoted in the body would end the wrapper, and what follows would read as the user.
	wrapped := closeRE.ReplaceAllString(strings.TrimSpace(body), "&lt;/reply>")
	prompt := "Rewrite this reply:\n\n<reply>\n" + wrapped + "\n</reply>\n"
	text, err := run(ctx, Role(), prompt)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	short := Parse(text)
	if !Faithful(body, short) {
		return "", nil
	}
	return short, nil
}

// Parse is the model's reply less a preamble, a fence round it all or echoed tags.
func Parse(text string) string {
	text = unfence(strings.TrimSpace(text))
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "<reply>"), "</reply>"))
	if first, rest, ok := strings.Cut(text, "\n"); ok && preamble(first) {
		text = strings.TrimSpace(rest)
	}
	return text
}

// Faithful is whether short can stand in for body: shorter, with every fenced
// block, code span, url, path and number of body in it verbatim.
func Faithful(body, short string) bool {
	if short == "" || utf8.RuneCountInString(short) >= utf8.RuneCountInString(body) {
		return false
	}
	for _, block := range blocks(body) {
		if !strings.Contains(short, block) {
			return false
		}
	}
	for _, token := range keep(body) {
		if !strings.Contains(short, token) {
			return false
		}
	}
	return true
}

func preamble(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasSuffix(line, ":") || utf8.RuneCountInString(line) > 80 {
		return false
	}
	return preambleRE.MatchString(line)
}

// unfence strips one fence round the whole answer and leaves fences inside it.
func unfence(text string) string {
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(lines) < 2 || !fence(lines[0]) || (last != "```" && last != "~~~") {
		return text
	}
	for _, line := range lines[1 : len(lines)-1] {
		if fence(line) {
			return text
		}
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func fence(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

// keep is every keepRE match of text, less the backticks round a span or the
// punctuation a sentence ends a path with.
func keep(text string) []string {
	var out []string
	for _, m := range keepRE.FindAllString(text, -1) {
		if strings.HasPrefix(m, "`") {
			m = strings.Trim(m, "`")
		} else {
			m = strings.TrimRight(m, ".,;:")
		}
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// blocks is the inside of every fenced block, in order.
func blocks(text string) []string {
	var out, current []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case fence(line):
			if fenced {
				out = append(out, strings.Join(current, "\n"))
				current = nil
			}
			fenced = !fenced
		case fenced:
			current = append(current, line)
		}
	}
	return out
}

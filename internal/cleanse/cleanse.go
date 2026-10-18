// Package cleanse rewrites a reply short with a cheap model, after the turn:
// sharpen pointed the other way. The original is kept; every failure is no change.
package cleanse

import (
	"context"
	_ "embed"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Model is the cheap model that rewrites; the task is rewriting, not reasoning.
const Model = "claude-haiku-4-5-20251001"

// Timeout: measured 31s, 33s and 44s for one real rewrite on 2026-10-17, so
// 30s killed every one. Nobody waits on this; only a hung run needs dropping.
const Timeout = 3 * time.Minute

// slack is a percent of the original: plain words often run longer than a name.
const slack = 125

// Why a rewrite was dropped, one word each so a week of them counts.
const (
	WhyFailed     = "failed"
	WhyTimeout    = "timeout"
	WhyEmpty      = "empty"
	WhyLonger     = "longer"
	WhyUnfaithful = "unfaithful"
)

// Drop is a rewrite that was not used, and which gate turned it down.
type Drop struct {
	Why  string
	Lost string // what an unfaithful rewrite dropped
	Err  error
}

func (d *Drop) Error() string {
	switch {
	case d.Lost != "":
		return d.Why + ": lost " + strconv.Quote(d.Lost)
	case d.Err != nil:
		return d.Why + ": " + d.Err.Error()
	}
	return d.Why
}

func (d *Drop) Unwrap() error { return d.Err }

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
	nameRE  = regexp.MustCompile(`^[A-Za-z_][\w.]*$`)
	fileRE  = regexp.MustCompile(`\.[A-Za-z]\w{0,5}$`)
)

// Wanted is prose not mostly a fence, table or path list. No length floor:
// a short reply can be the least readable thing here.
func Wanted(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
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

// Rewrite returns body rewritten short, or a *Drop naming the gate it failed.
func Rewrite(ctx context.Context, run Runner, body string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	// A closing tag quoted in the body would end the wrapper, and what follows would read as the user.
	wrapped := closeRE.ReplaceAllString(strings.TrimSpace(body), "&lt;/reply>")
	prompt := "Rewrite this reply:\n\n<reply>\n" + wrapped + "\n</reply>\n"
	text, err := run(ctx, Role(), prompt)
	if err != nil {
		return "", &Drop{Why: why(ctx, err), Err: err}
	}
	if err := ctx.Err(); err != nil {
		return "", &Drop{Why: WhyTimeout, Err: err}
	}
	switch short := Parse(text); {
	case short == "":
		return "", &Drop{Why: WhyEmpty}
	case tooLong(body, short):
		return "", &Drop{Why: WhyLonger}
	default:
		if lost := Lost(body, short); lost != "" {
			return "", &Drop{Why: WhyUnfaithful, Lost: lost}
		}
		return short, nil
	}
}

func tooLong(body, short string) bool {
	return utf8.RuneCountInString(short)*100 > utf8.RuneCountInString(body)*slack
}

// why separates a run that failed from one the deadline ended under it.
func why(ctx context.Context, err error) string {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return WhyTimeout
	}
	return WhyFailed
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
	if short == "" || tooLong(body, short) {
		return false
	}
	return Lost(body, short) == ""
}

// Lost is the first fenced block, span, url, path or number of body short drops.
func Lost(body, short string) string {
	for _, block := range blocks(body) {
		if !strings.Contains(short, block) {
			line, _, _ := strings.Cut(strings.TrimSpace(block), "\n")
			return line
		}
	}
	for _, token := range keep(body) {
		if !strings.Contains(short, token) {
			return token
		}
	}
	return ""
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

func keep(text string) []string {
	var out []string
	for _, m := range keepRE.FindAllString(text, -1) {
		if strings.HasPrefix(m, "`") {
			m = strings.Trim(m, "`")
			if Name(m) {
				continue
			}
		} else {
			m = strings.TrimRight(m, ".,;:")
		}
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// Name is a code span labelling a thing, not quoting a fact: plain words may stand in.
func Name(span string) bool {
	return nameRE.MatchString(span) && !fileRE.MatchString(span)
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

// Package weld asks a cheap model which of a run of queued prompts belong
// with the first, so the ones that continue a thought go to the agent as one turn.
package weld

import (
	"context"
	_ "embed"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Model is the cheap model that reads the prompts.
const Model = "claude-haiku-4-5-20251001"

// Timeout bounds the decider, which is on the path to starting a turn. A
// claude -p answers in 4-5s wall clock, measured; 2s killed every one.
const Timeout = 8 * time.Second

const (
	// MaxEntries is the most prompts one turn is made of.
	MaxEntries = 3
	// MaxBytes is the longest joined message worth welding.
	MaxBytes = 16 << 10
	// Lookahead is how far behind the first the model reads.
	Lookahead = 5
)

//go:embed role.md
var role string

// Role is the instruction the model runs under.
func Role() string { return strings.TrimSpace(role) }

// Runner sends one prompt to a model with no tools. Tests inject one.
type Runner func(ctx context.Context, system, prompt string) (string, error)

// Entry is a queued turn as the gate sees it.
type Entry struct {
	Chat    bool
	At      time.Time
	Message string
	File    string
}

// Candidates is the gate, before any model is asked: which entries behind
// first could weld to it. since is the start of the run that just ended.
func Candidates(first Entry, behind []Entry, since time.Time) []int {
	if since.IsZero() || !fits(first, since) || isCommand(first) || len(first.Message) > MaxBytes {
		return nil
	}
	// The file the turn is about is the first selection seen, the first's or a candidate's.
	file := first.File
	var out []int
	for i, e := range behind {
		// Another kind of turn, or a command, is an order the user built.
		if !e.Chat || isCommand(e) || len(out) == Lookahead {
			break
		}
		if !fits(e, since) || len(e.Message) > MaxBytes {
			continue
		}
		if e.File != "" && file != "" && e.File != file {
			continue
		}
		if file == "" {
			file = e.File
		}
		out = append(out, i)
	}
	return out
}

func fits(e Entry, since time.Time) bool {
	return e.Chat && e.At.After(since) && strings.TrimSpace(e.Message) != ""
}

func isCommand(e Entry) bool {
	return strings.HasPrefix(strings.TrimSpace(e.Message), "/")
}

// Fit trims a chosen set to what one turn holds: the first MaxEntries-1 of
// them whose messages keep the joined turn under MaxBytes.
func Fit(first string, behind []string, chosen []int) []int {
	size := len(first)
	var out []int
	for _, i := range chosen {
		if len(out) == MaxEntries-1 {
			break
		}
		if size += len("\n\n") + len(behind[i]); size > MaxBytes {
			break
		}
		out = append(out, i)
	}
	return out
}

// Linked reports which of the entries behind the first belong with it, as
// sorted indexes into behind. Every failure is none.
func Linked(ctx context.Context, run Runner, first string, behind []string) []int {
	if len(behind) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var b strings.Builder
	b.WriteString("The first prompt:\n\n<first>\n" + block(first) + "\n</first>\n")
	b.WriteString("\nThe prompts typed after it, in order:\n")
	for i, next := range behind {
		fmt.Fprintf(&b, "\n<next n=\"%d\">\n%s\n</next>\n", i+1, block(next))
	}
	text, err := run(ctx, Role(), b.String())
	if err != nil || ctx.Err() != nil {
		return nil
	}
	var out []int
	for _, n := range Parse(text) {
		if n >= 1 && n <= len(behind) {
			out = append(out, n-1)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// closers are the markers a pasted prompt could use to end its own block early.
var closers = strings.NewReplacer("</first>", "", "</next>", "")

func block(text string) string { return closers.Replace(strings.TrimSpace(text)) }

var replyRE = regexp.MustCompile("^`?(none|0|\\d{1,3}(?:\\s*[,\\s]\\s*\\d{1,3})*)`?\\.?$")

// Parse reads the model's reply as the numbers it named. Anything else is none.
func Parse(text string) []int {
	m := replyRE.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil || m[1] == "none" || m[1] == "0" {
		return nil
	}
	var out []int
	for _, field := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

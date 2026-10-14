// Package sharpen asks a cheap model which reading of a prompt was meant,
// before an expensive turn is spent on the wrong one.
package sharpen

import (
	"context"
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Model is the cheap model that reads the prompt.
const Model = "claude-haiku-4-5-20251001"

// Timeout is how long an answer is worth waiting for: a late one is dropped.
const Timeout = 3 * time.Second

const (
	longPrompt   = 200
	maxQuestions = 3
	maxOptions   = 3
	minOptions   = 2
)

//go:embed role.md
var role string

// Role is the instruction the model runs under.
func Role() string { return strings.TrimSpace(role) }

// Question is one ambiguity, as a choice.
type Question struct {
	Ask     string   `json:"ask"`
	Options []string `json:"options"`
}

// Runner sends one prompt to a model with no tools. Tests inject one.
type Runner func(ctx context.Context, system, prompt string) (string, error)

var (
	fileRE   = regexp.MustCompile(`[\w-]+/[\w.-]+|[\w-]{2,}\.[A-Za-z][A-Za-z0-9]{0,5}\b`)
	symbolRE = regexp.MustCompile("`[^`\n]+`|\\b[a-z]+[A-Z]\\w*|\\b[A-Z][a-z0-9]+[A-Z]\\w*|\\b[A-Za-z0-9]+_\\w+|\\w+\\(")
	vagueRE  = regexp.MustCompile(`(?i)\b(all|everything|properly|better|clean up|refactor\w*|make it work)\b`)
)

// Wanted is the gate: true when the prompt is worth a look.
func Wanted(prompt string) bool {
	prompt = strings.TrimSpace(prompt)
	// A slash command is an instruction to the harness, not a turn to aim.
	if prompt == "" || strings.HasPrefix(prompt, "/") {
		return false
	}
	if utf8.RuneCountInString(prompt) > longPrompt {
		return true
	}
	if !fileRE.MatchString(prompt) && !symbolRE.MatchString(prompt) {
		return true
	}
	return vagueRE.MatchString(prompt)
}

// Ask returns the questions the model has about prompt. doc is the design
// document's open questions, or empty. No questions is the usual answer.
func Ask(ctx context.Context, run Runner, prompt, doc string) ([]Question, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	var b strings.Builder
	b.WriteString("The prompt:\n\n<prompt>\n" + strings.TrimSpace(prompt) + "\n</prompt>\n")
	if doc = strings.TrimSpace(doc); doc != "" {
		b.WriteString("\nThe design document already lists these open questions:\n\n<open-questions>\n" + doc + "\n</open-questions>\n")
	}
	text, err := run(ctx, Role(), b.String())
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return Parse(text), nil
}

// Parse reads the model's reply. A question without a choice is dropped, and
// a reply that is not the format is no questions.
func Parse(text string) []Question {
	start, end := strings.Index(text, "["), strings.LastIndex(text, "]")
	if start < 0 || end < start {
		return nil
	}
	var raw []Question
	if json.Unmarshal([]byte(text[start:end+1]), &raw) != nil {
		return nil
	}
	var out []Question
	for _, q := range raw {
		q.Ask = strings.TrimSpace(q.Ask)
		q.Options = options(q.Options)
		if q.Ask == "" || len(q.Options) < minOptions {
			continue
		}
		if out = append(out, q); len(out) == maxQuestions {
			break
		}
	}
	return out
}

func options(in []string) []string {
	var out []string
	for _, option := range in {
		if option = strings.TrimSpace(option); option != "" && len(out) < maxOptions {
			out = append(out, option)
		}
	}
	return out
}

var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)

// OpenQuestions is the body of a document's "Open questions" section.
func OpenQuestions(doc string) string {
	var out []string
	level, fenced := 0, false
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
		}
		if m := headingRE.FindStringSubmatch(line); m != nil && !fenced {
			if level > 0 && len(m[1]) <= level {
				break
			}
			if level == 0 && strings.EqualFold(m[2], "open questions") {
				level = len(m[1])
				continue
			}
		}
		if level > 0 {
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

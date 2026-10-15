// Package quiz asks an agent to write multiple-choice questions about a diff,
// so the person raising the pull request finds out what they have not read.
package quiz

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/prompt"
)

// Question is one multiple-choice question, anchored to a hunk in the diff.
type Question struct {
	Ref  string `json:"ref"`
	File string `json:"file"`
	Line int    `json:"line"`

	// About is an optional second link, to anything in the repository: the
	// question most worth asking is often about what the branch hits.
	About string `json:"about,omitempty"`

	Question string   `json:"question"`
	Options  []string `json:"options"`
	Answer   string   `json:"answer"`
	Why      string   `json:"why"`
}

// Fence tags the blocks the prompt asks for, one per question.
const Fence = "ade-quiz"

const (
	// MinQuestions is below which there is no confidence check to take.
	MinQuestions = 3
	// MaxQuestions is what the prompt asks for; the note asked for short.
	MaxQuestions = 5
	// minOptions is what a multiple choice needs to be one.
	minOptions = 3
)

//go:embed quiz.md
var contract string

var fenceRE = regexp.MustCompile("(?s)```" + Fence + "\\s*\\n(.*?)```")

// Prompt is the question contract with the rendered diff under it.
func Prompt(diffText string) string {
	return strings.TrimSpace(contract) + "\n\n" + prompt.Fence("diff", diffText) + "\n"
}

// Tools let the agent read for the context a hunk does not carry — the caller,
// the test, the interface. No Bash: the diff is handed over rendered.
func Tools() []string { return []string{"Read", "Grep", "Glob"} }

// Parse keeps the questions that can be drawn and marked, dropping a block
// that will not read. inDiff says whether a path is one the diff shows.
func Parse(text string, inDiff func(string) bool) []Question {
	var out []Question
	seen := map[string]bool{}
	for _, match := range fenceRE.FindAllStringSubmatch(text, -1) {
		var q Question
		if err := json.Unmarshal([]byte(match[1]), &q); err != nil {
			continue
		}
		if !q.tidy() || seen[q.Ref] || !inDiff(q.File) {
			continue
		}
		seen[q.Ref] = true
		out = append(out, q)
		if len(out) == MaxQuestions {
			break
		}
	}
	return out
}

// Shuffle reorders the options and moves the answer letter with them, so a
// model that favours the first slot does not hand you the quiz.
func Shuffle(all []Question, rnd *rand.Rand) []Question {
	out := make([]Question, 0, len(all))
	for _, q := range all {
		right := q.Options[index(q.Answer)]
		options := append([]string(nil), q.Options...)
		rnd.Shuffle(len(options), func(i, j int) {
			options[i], options[j] = options[j], options[i]
		})
		for i, option := range options {
			if option == right {
				q.Answer = letter(i)
				break
			}
		}
		q.Options = options
		out = append(out, q)
	}
	return out
}

// tidy trims the fields and reports whether what is left is a question that
// can be drawn, asked and marked.
func (q *Question) tidy() bool {
	q.Ref = strings.ToUpper(strings.TrimSpace(q.Ref))
	q.File = strings.TrimSpace(q.File)
	q.About = strings.TrimSpace(q.About)
	q.Question = strings.TrimSpace(q.Question)
	q.Why = strings.TrimSpace(q.Why)
	q.Answer = strings.ToUpper(strings.TrimSpace(q.Answer))

	options := make([]string, 0, len(q.Options))
	unique := map[string]bool{}
	for _, option := range q.Options {
		option = strings.TrimSpace(option)
		if option == "" || unique[option] {
			// A duplicate option makes a letter ambiguous to mark against.
			return false
		}
		unique[option] = true
		options = append(options, option)
	}
	q.Options = options

	at := index(q.Answer)
	return q.Ref != "" && q.File != "" && q.Question != "" &&
		len(options) >= minOptions && at >= 0 && at < len(options)
}

// index is the option a letter names, or -1 for anything that is not one.
func index(answer string) int {
	if len(answer) != 1 || answer[0] < 'A' || answer[0] > 'Z' {
		return -1
	}
	return int(answer[0] - 'A')
}

func letter(i int) string { return string(rune('A' + i)) }

// Package drift is the design checklist and the last pass over it, shared by
// the server that scores it and the agent that reads it back.
package drift

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/delegate"
	"github.com/owenrumney/ade/internal/design"
)

// State is the design read as a checklist, with the last pass's verdicts.
type State struct {
	File  string    `json:"file"`
	At    time.Time `json:"at"`
	Items []Item    `json:"items"`
}

// Item is one requirement and its score.
type Item struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Line    int    `json:"line"`
	Hash    string `json:"hash"`
	Verdict string `json:"verdict"`
	Note    string `json:"note"`
	// File and FileLine are set only on a contradiction.
	File      string `json:"file"`
	FileLine  int    `json:"fileLine"`
	Dismissed bool   `json:"dismissed"`
}

// Ask reads the checklist: the pass says only its count in the conversation.
func Ask(ctx context.Context) (State, error) {
	var out State
	err := delegate.Get(ctx, "the design checklist", "/api/internal/delegate/drift", nil, &out)
	return out, err
}

// Format is what is not done, plus the done ones when all. A dismissed item is
// an answer already given, so it stays out either way.
func Format(state State, all bool) string {
	if state.File == "" {
		return "No design document is open, so there is no checklist.\n"
	}
	var out strings.Builder
	when := "no pass has run yet"
	if !state.At.IsZero() {
		when = "last pass " + state.At.Format(time.RFC3339)
	}
	fmt.Fprintf(&out, "%s — %d requirement(s), %s\n\n", state.File, len(state.Items), when)

	shown := 0
	for _, item := range state.Items {
		if item.Dismissed || (!all && item.Verdict == design.DriftDone) {
			continue
		}
		shown++
		fmt.Fprintf(&out, "--- %s  line %d\n%s\n", verdict(item.Verdict), item.Line, item.Text)
		if item.Note != "" {
			fmt.Fprintf(&out, "    %s\n", item.Note)
		}
		if item.File != "" {
			fmt.Fprintf(&out, "    contradicted at %s:%d\n", item.File, item.FileLine)
		}
		out.WriteString("\n")
	}
	if shown == 0 {
		out.WriteString("Nothing outstanding.\n")
	}
	return out.String()
}

// verdict names an unscored item: not one a pass could not find.
func verdict(v string) string {
	if v == "" {
		return "unscored"
	}
	return v
}

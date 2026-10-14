package drift

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/design"
)

func state() State {
	return State{
		File: "cart.md",
		At:   time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Items: []Item{
			{Text: "Cache the cart in Redis.", Line: 10, Verdict: design.DriftDone},
			{Text: "Expire it after an hour.", Line: 11, Verdict: design.DriftAbsent, Note: "no TTL anywhere"},
			{Text: "Read it on every request.", Line: 14, Verdict: design.DriftContradicted,
				Note: "read once at boot", File: "cart.go", FileLine: 42},
			{Text: "Dismissed one.", Line: 15, Verdict: design.DriftAbsent, Dismissed: true},
		},
	}
}

func TestFormatShowsWhatIsOutstanding(t *testing.T) {
	out := Format(state(), false)

	assert.Contains(t, out, "cart.md — 4 requirement(s), last pass 2026-09-16T12:00:00Z")
	assert.Contains(t, out, "Expire it after an hour.")
	assert.Contains(t, out, "no TTL anywhere")
	assert.Contains(t, out, "contradicted at cart.go:42")
	// The done one is the one the agent has nothing to do about, and a
	// dismissed one was answered already.
	assert.NotContains(t, out, "Cache the cart in Redis.")
	assert.NotContains(t, out, "Dismissed one.")
}

func TestFormatShowsTheDoneOnesWhenAsked(t *testing.T) {
	out := Format(state(), true)
	assert.Contains(t, out, "Cache the cart in Redis.")
	// Dismissed is the answer already given, so it stays out either way.
	assert.NotContains(t, out, "Dismissed one.")
}

func TestFormatSaysSoWhenThereIsNothingToSay(t *testing.T) {
	assert.Contains(t, Format(State{}, false), "No design document is open")

	none := State{File: "cart.md", Items: []Item{{Text: "One.", Verdict: design.DriftDone}}}
	out := Format(none, false)
	assert.Contains(t, out, "no pass has run yet")
	assert.Contains(t, out, "Nothing outstanding.")
}

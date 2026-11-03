package docnotes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const doc = "# Plan\n\nThe cart recomputes on every keystroke.\n\n## Approach\n\nDebounce it.\n"

func TestFirstAnchorsToTheOpeningOccurrence(t *testing.T) {
	a, ok := First(doc, "Debounce it.")
	require.True(t, ok)
	assert.Equal(t, "Debounce it.", a.Quote)
	assert.Equal(t, 0, a.Nth)
	assert.NotEmpty(t, a.ContextHash)
}

func TestAQuoteTheDocumentDoesNotContainCannotBeAnchored(t *testing.T) {
	_, ok := First(doc, "cache it at the edge")
	assert.False(t, ok)

	_, ok = First(doc, "")
	assert.False(t, ok)
}

func TestLocatePinsTheOccurrenceAtTheOffsetGiven(t *testing.T) {
	body := "debounce it. debounce it."
	a, ok := Locate(body, "debounce it.", 13)
	require.True(t, ok)
	assert.Equal(t, 1, a.Nth)

	// An offset the quote is not actually at anchors nothing, rather than
	// quietly anchoring somewhere else.
	_, ok = Locate(body, "debounce it.", 5)
	assert.False(t, ok)
}

func TestAnAnchorSurvivesAnEditElsewhereInTheDocument(t *testing.T) {
	a, ok := First(doc, "Debounce it.")
	require.True(t, ok)

	edited := "# Plan\n\nA rewritten problem section entirely.\n\n## Approach\n\nDebounce it.\n"
	at, _, ok := Find(edited, a)
	require.True(t, ok)
	assert.Equal(t, "Debounce it.", edited[at:at+len(a.Quote)])
	assert.NotEqual(t, a.At, at)
}

func TestTheOccurrenceWithTheSameSurroundingsWins(t *testing.T) {
	body := "# Plan\n\nThe cart recomputes on every keystroke, so the totals follow it.\n\nshared text.\n\nAn approach paragraph, long enough that each quote has its own neighbours.\n\nshared text.\n\nEnd.\n"
	a, ok := Locate(body, "shared text.", strings.LastIndex(body, "shared text."))
	require.True(t, ok)
	require.Equal(t, 1, a.Nth)

	// A paragraph inserted above moves both occurrences down far enough that
	// the first now sits nearer to where the note was than the second does.
	// The surroundings are what tell them apart, and they have to win.
	moved := "# Plan\n\n" + strings.Repeat("An opening paragraph nobody asked for. ", 3) + "\n" + body[8:]
	first, second := strings.Index(moved, "shared text."), strings.LastIndex(moved, "shared text.")
	require.Less(t, abs(first-a.At), abs(second-a.At), "nearest would have picked the wrong one")

	at, nth, ok := Find(moved, a)
	require.True(t, ok)
	assert.Equal(t, 1, nth)
	assert.Equal(t, second, at)
}

func TestAQuoteNearTheTopIsFoundAgainAfterAnInsertAboveIt(t *testing.T) {
	// Its context window is short — there is not 40 characters of document in
	// front of it — so the hash misses and the nearest match has to carry it.
	a, ok := First(doc, "The cart recomputes")
	require.True(t, ok)

	moved := "---\ntitle: Plan\n---\n\n" + doc
	at, _, ok := Find(moved, a)
	require.True(t, ok)
	assert.Equal(t, strings.Index(moved, "The cart recomputes"), at)
}

func TestAQuoteThatIsGoneOrphansTheAnchor(t *testing.T) {
	a, ok := First(doc, "The cart recomputes on every keystroke.")
	require.True(t, ok)

	_, _, ok = Find("# Plan\n\nIt is slow.\n", a)
	assert.False(t, ok)
}

func TestARepeatedQuoteIsCountedWithoutOverlapping(t *testing.T) {
	assert.Equal(t, []int{0, 2}, occurrences("aaaa", "aa"))
	assert.Empty(t, occurrences("abc", "z"))
}

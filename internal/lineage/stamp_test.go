package lineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func storeWith(t *testing.T, entries ...Entry) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), "work")
	require.NoError(t, err)
	for _, e := range entries {
		require.NoError(t, s.Add(e))
	}
	return s
}

func TestStampMarksTheRangesACommitCarried(t *testing.T) {
	lines := []string{"alpha", "beta", "gamma", "delta"}
	diff := diffOf(t, "a.go", lines)
	store := storeWith(t, Entry{
		Session: "s", Summary: "make it alpha",
		Ranges: []Range{rangeAt(t, diff, "a.go", 1, 2), rangeAt(t, diff, "a.go", 3, 2)},
	})

	n, err := store.Stamp(diff, "abc123", map[string][]string{"a.go": {"alpha", "beta"}})
	require.NoError(t, err)
	assert.Equal(t, 1, n, "only the range the commit carried")

	got := store.List()[0].Ranges
	assert.Equal(t, "abc123", got[0].Commit)
	assert.Empty(t, got[1].Commit, "the staged hunk is not in this commit")
}

func TestStampLeavesARangeWithTheCommitThatFirstCarriedIt(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"alpha", "beta"})
	store := storeWith(t, Entry{Session: "s", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 2)}})

	added := map[string][]string{"a.go": {"alpha", "beta"}}
	_, err := store.Stamp(diff, "first", added)
	require.NoError(t, err)
	n, err := store.Stamp(diff, "second", added)
	require.NoError(t, err)

	assert.Zero(t, n)
	assert.Equal(t, "first", store.List()[0].Ranges[0].Commit)
}

func TestStampSkipsARangeTheCommitOnlyPartlyAdded(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"alpha", "beta"})
	store := storeWith(t, Entry{Session: "s", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 2)}})

	n, err := store.Stamp(diff, "abc123", map[string][]string{"a.go": {"alpha"}})
	require.NoError(t, err)
	assert.Zero(t, n, "half a range is not this commit's")
}

func TestStampIgnoresAnotherFilesLines(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"alpha"})
	store := storeWith(t, Entry{Session: "s", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 1)}})

	n, err := store.Stamp(diff, "abc123", map[string][]string{"b.go": {"alpha"}})
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestStampDoesNotClaimABlankRange(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"", ""})
	store := storeWith(t, Entry{Session: "s", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 2)}})

	n, err := store.Stamp(diff, "abc123", map[string][]string{"a.go": {""}})
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestRestampMovesAnAmendedCommitsRanges(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"alpha"})
	store := storeWith(t, Entry{Session: "s", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 1)}})
	_, err := store.Stamp(diff, "old", map[string][]string{"a.go": {"alpha"}})
	require.NoError(t, err)

	n, err := store.Restamp("old", "new")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, "new", store.List()[0].Ranges[0].Commit)
}

func TestForCommitFindsTheTurnsBehindASha(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"alpha", "beta"})
	store := storeWith(t,
		Entry{Session: "s1", Summary: "wrote alpha", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 1)}},
		Entry{Session: "s2", Summary: "wrote beta", Ranges: []Range{rangeAt(t, diff, "a.go", 2, 1)}},
	)
	_, err := store.Stamp(diff, "abc123", map[string][]string{"a.go": {"alpha", "beta"}})
	require.NoError(t, err)

	turns := store.ForCommit("abc123", "a.go")
	require.Len(t, turns, 2)
	assert.Equal(t, "wrote beta", turns[0].Summary, "newest first")
	assert.Empty(t, store.ForCommit("abc123", "b.go"))
	assert.Empty(t, store.ForCommit("nope", ""))
}

func TestAStampSurvivesReopeningTheStore(t *testing.T) {
	diff := diffOf(t, "a.go", []string{"alpha"})
	dir := t.TempDir()
	store, err := Open(dir, "work")
	require.NoError(t, err)
	require.NoError(t, store.Add(Entry{Session: "s", Ranges: []Range{rangeAt(t, diff, "a.go", 1, 1)}}))
	_, err = store.Stamp(diff, "abc123", map[string][]string{"a.go": {"alpha"}})
	require.NoError(t, err)

	again, err := Open(dir, "work")
	require.NoError(t, err)
	assert.Equal(t, "abc123", again.List()[0].Ranges[0].Commit)
}

package lineage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diffOf builds a diff where every line of body is an addition, which is what a
// new file looks like and is enough to anchor against.
func diffOf(t *testing.T, path string, lines []string) *gitdiff.Diff {
	t.Helper()
	out := "diff --git a/" + path + " b/" + path + "\n" +
		"--- /dev/null\n+++ b/" + path + "\n" +
		"@@ -0,0 +1," + itoa(len(lines)) + " @@\n"
	for _, l := range lines {
		out += "+" + l + "\n"
	}
	return gitdiff.FromText("HEAD", "worktree", out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func rangeAt(t *testing.T, diff *gitdiff.Diff, file string, start, lines int) Range {
	t.Helper()
	anchor, ok := comments.Locate(diff, file, comments.SideRight, start)
	require.True(t, ok, "line %d of %s should be anchorable", start, file)
	return Range{File: file, Start: start, Lines: lines, Anchor: anchor}
}

func TestChangedSpansTheAfterSide(t *testing.T) {
	tests := []struct {
		name          string
		before, after string
		want          []Span
	}{
		{
			name:   "identical text has no lineage",
			before: "a\nb\nc\n",
			after:  "a\nb\nc\n",
		},
		{
			name:   "one changed line",
			before: "a\nb\nc\n",
			after:  "a\nB\nc\n",
			want:   []Span{{Start: 2, Lines: 1}},
		},
		{
			name:   "an insertion is credited, not the lines it moved",
			before: "a\nb\n",
			after:  "a\nnew\nb\n",
			want:   []Span{{Start: 2, Lines: 1}},
		},
		{
			name:   "a deletion leaves no line to attribute",
			before: "a\ngone\nb\n",
			after:  "a\nb\n",
		},
		{
			name:   "two separate edits are two spans",
			before: "a\nb\nc\nd\ne\nf\ng\nh\n",
			after:  "A\nb\nc\nd\ne\nf\ng\nH\n",
			want:   []Span{{Start: 1, Lines: 1}, {Start: 8, Lines: 1}},
		},
		{
			name:   "a new file is all of it",
			before: "",
			after:  "one\ntwo\nthree\n",
			want:   []Span{{Start: 1, Lines: 3}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Changed(context.Background(), tc.before, tc.after)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAddAndRead(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, "feature/one")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "ade", "feature-one.lineage.json"), store.Path())

	lines := []string{"package a", "", "func one() {}", "func two() {}"}
	diff := diffOf(t, "a.go", lines)

	require.NoError(t, store.Add(Entry{
		Session:   "s1",
		MessageID: "0001",
		Summary:   "add one",
		Ranges:    []Range{rangeAt(t, diff, "a.go", 3, 1)},
	}))
	require.NoError(t, store.Add(Entry{
		Session:   "s1",
		MessageID: "0002",
		Summary:   "add two",
		Ranges:    []Range{rangeAt(t, diff, "a.go", 4, 1)},
	}))

	t.Run("a line reports the turn that wrote it", func(t *testing.T) {
		turns := store.At(diff, "a.go", 3)
		require.Len(t, turns, 1)
		assert.Equal(t, "0001", turns[0].MessageID)
		assert.Equal(t, "add one", turns[0].Summary)
		assert.False(t, turns[0].At.IsZero())
	})

	t.Run("newest first", func(t *testing.T) {
		both := Entry{Session: "s1", MessageID: "0003", Summary: "touch three", Ranges: []Range{rangeAt(t, diff, "a.go", 3, 1)}}
		require.NoError(t, store.Add(both))
		turns := store.At(diff, "a.go", 3)
		require.Len(t, turns, 2)
		assert.Equal(t, "0003", turns[0].MessageID)
		assert.Equal(t, "0001", turns[1].MessageID)
	})

	t.Run("an untouched line has no lineage", func(t *testing.T) {
		assert.Empty(t, store.At(diff, "a.go", 1))
	})

	t.Run("another file has no lineage", func(t *testing.T) {
		assert.Empty(t, store.At(diff, "b.go", 3))
	})

	t.Run("marks name every covered line once", func(t *testing.T) {
		marks := store.Marks(diff)
		assert.Equal(t, map[string][]int{"a.go": {3, 4}}, marks)
	})

	t.Run("it survives reopening", func(t *testing.T) {
		again, err := Open(dir, "feature/one")
		require.NoError(t, err)
		assert.Len(t, again.List(), 3)
		assert.Len(t, again.At(diff, "a.go", 4), 1)
	})
}

func TestAnchorsFollowLinesThatMove(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, "main")
	require.NoError(t, err)

	before := diffOf(t, "a.go", []string{"one", "two", "three"})
	require.NoError(t, store.Add(Entry{
		Session:   "s",
		MessageID: "0001",
		Summary:   "wrote three",
		Ranges:    []Range{rangeAt(t, before, "a.go", 3, 1)},
	}))

	// Two lines land above it, so "three" is now line 5.
	after := diffOf(t, "a.go", []string{"one", "inserted", "also", "two", "three"})

	assert.Len(t, store.At(after, "a.go", 5), 1, "the line should be found where it moved to")
	assert.Empty(t, store.At(after, "a.go", 3), "and not where it used to be")
	assert.Equal(t, map[string][]int{"a.go": {5}}, store.Marks(after))
}

func TestContentThatHasGoneIsNotGuessedAt(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, "main")
	require.NoError(t, err)

	before := diffOf(t, "a.go", []string{"one", "two", "three"})
	require.NoError(t, store.Add(Entry{
		Session: "s", MessageID: "0001", Summary: "wrote two",
		Ranges: []Range{rangeAt(t, before, "a.go", 2, 1)},
	}))

	after := diffOf(t, "a.go", []string{"one", "utterly different", "three"})
	assert.Empty(t, store.At(after, "a.go", 2))
	assert.Empty(t, store.Marks(after)["a.go"])
}

func TestAddIgnoresATurnThatWroteNothing(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, "main")
	require.NoError(t, err)

	require.NoError(t, store.Add(Entry{Session: "s", MessageID: "0001"}))
	assert.Empty(t, store.List())
	assert.NoFileExists(t, store.Path())
}

func TestPruneDropsFilesGoneFromTheDiff(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir, "main")
	require.NoError(t, err)

	a := diffOf(t, "a.go", []string{"one", "two"})
	b := diffOf(t, "b.go", []string{"one", "two"})
	require.NoError(t, store.Add(Entry{Session: "s", MessageID: "0001", Ranges: []Range{rangeAt(t, a, "a.go", 1, 1)}}))
	require.NoError(t, store.Add(Entry{Session: "s", MessageID: "0002", Ranges: []Range{rangeAt(t, b, "b.go", 1, 1)}}))

	require.NoError(t, store.Prune(a))
	entries := store.List()
	require.Len(t, entries, 1)
	assert.Equal(t, "a.go", entries[0].Ranges[0].File)
}

func TestOpenRejectsRubbish(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "ade"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ade", "main.lineage.json"), []byte("not json"), 0o600))

	_, err := Open(dir, "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse lineage")
}

func TestNilStoreIsUsable(t *testing.T) {
	var store *Store
	assert.NoError(t, store.Add(Entry{}))
	assert.Empty(t, store.List())
	assert.Empty(t, store.At(nil, "a.go", 1))
	assert.Empty(t, store.Marks(nil))
	assert.NoError(t, store.Prune(nil))
	assert.Empty(t, store.Path())
}

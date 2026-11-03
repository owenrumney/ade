package docnotes

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func store(t *testing.T) (*Store, string) {
	t.Helper()
	gitDir := t.TempDir()
	s, err := Open(gitDir, "improve-design.md")
	require.NoError(t, err)
	return s, gitDir
}

func filed(t *testing.T, s *Store, quote, body, author string) Note {
	t.Helper()
	anchor, ok := First(doc, quote)
	require.True(t, ok)
	n, err := s.Add(Note{Anchor: anchor, Comments: []Comment{{Author: author, Body: body}}})
	require.NoError(t, err)
	return n
}

func TestANoteIsReadBackAfterARestart(t *testing.T) {
	s, gitDir := store(t)
	filed(t, s, "Debounce it.", "where does the delay come from?", "architect")

	again, err := Open(gitDir, "improve-design.md")
	require.NoError(t, err)
	notes := again.List()
	require.Len(t, notes, 1)
	assert.Equal(t, "architect", notes[0].Comments[0].Author)
	assert.Equal(t, "Debounce it.", notes[0].Anchor.Quote)
}

func TestANoteNeedsAQuoteAndSomethingToSay(t *testing.T) {
	s, _ := store(t)

	_, err := s.Add(Note{Comments: []Comment{{Body: "no quote"}}})
	assert.Error(t, err)

	anchor, ok := First(doc, "Debounce it.")
	require.True(t, ok)
	_, err = s.Add(Note{Anchor: anchor})
	assert.Error(t, err)

	_, err = s.Add(Note{Anchor: anchor, Comments: []Comment{{Body: "   "}}})
	assert.Error(t, err)
}

func TestAnUnsignedCommentIsYours(t *testing.T) {
	s, _ := store(t)
	n := filed(t, s, "Debounce it.", "how long?", "")
	assert.Equal(t, AuthorYou, n.Comments[0].Author)
}

func TestReplyingOpensAResolvedNoteAgain(t *testing.T) {
	s, _ := store(t)
	n := filed(t, s, "Debounce it.", "how long?", "architect")

	_, err := s.SetResolved(n.ID, true)
	require.NoError(t, err)
	assert.Empty(t, s.Unresolved())

	again, err := s.Reply(n.ID, "", "300ms, and say so in the document")
	require.NoError(t, err)
	assert.False(t, again.Resolved)
	assert.Len(t, again.Comments, 2)
	assert.Len(t, s.Unresolved(), 1)
}

func TestAnEmptyReplyIsRefused(t *testing.T) {
	s, _ := store(t)
	n := filed(t, s, "Debounce it.", "how long?", "architect")
	_, err := s.Reply(n.ID, "you", "  ")
	assert.Error(t, err)
}

func TestAnUnknownNoteIsNotFound(t *testing.T) {
	s, _ := store(t)
	_, err := s.Get("nope")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = s.Reply("nope", "you", "hello")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = s.SetResolved("nope", true)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, s.Delete("nope"), ErrNotFound)
}

func TestDeletingTheLastNoteTakesTheFileWithIt(t *testing.T) {
	s, _ := store(t)
	n := filed(t, s, "Debounce it.", "how long?", "architect")
	require.NoError(t, s.Delete(n.ID))

	assert.Empty(t, s.List())
	_, err := os.Stat(s.Path())
	assert.True(t, os.IsNotExist(err))
}

func TestANoteFollowsItsQuoteWhenTheDocumentIsRewritten(t *testing.T) {
	s, _ := store(t)
	n := filed(t, s, "Debounce it.", "how long?", "architect")

	moved := "# Plan\n\nA much shorter problem.\n\n## Approach\n\nDebounce it.\n"
	after := s.Reanchor(moved)
	require.Len(t, after, 1)
	assert.False(t, after[0].Orphaned)
	assert.NotEqual(t, n.Anchor.At, after[0].Anchor.At)
}

func TestANoteWhoseQuoteIsDeletedIsOrphanedAndKept(t *testing.T) {
	s, _ := store(t)
	filed(t, s, "Debounce it.", "how long?", "architect")

	after := s.Reanchor("# Plan\n\nNothing of the old document survives.\n")
	require.Len(t, after, 1)
	assert.True(t, after[0].Orphaned)
	require.NotNil(t, after[0].OrphanedAt)

	// The clock is set once: a second pass that still cannot find it is not a
	// fresh orphaning.
	when := *after[0].OrphanedAt
	again := s.Reanchor("# Plan\n")
	assert.Equal(t, when, *again[0].OrphanedAt)
}

func TestAQuoteThatComesBackClearsTheOrphan(t *testing.T) {
	s, _ := store(t)
	filed(t, s, "Debounce it.", "how long?", "architect")

	require.True(t, s.Reanchor("# Plan\n")[0].Orphaned)

	back := s.Reanchor(doc)
	require.Len(t, back, 1)
	assert.False(t, back[0].Orphaned)
	assert.Nil(t, back[0].OrphanedAt)
}

func TestADocumentNameBecomesASafeFilename(t *testing.T) {
	assert.Equal(t, "enhancements-12-notes.md", Slug("enhancements/12-notes.md"))
	assert.Equal(t, "document", Slug("  /  "))
	assert.Len(t, Slug(string(make([]byte, 0, 200))+"aaaaaaaaaa"), 10)
}

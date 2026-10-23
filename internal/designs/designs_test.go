package designs

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontMatterNamesTheRepositoriesAWholeSessionSpans(t *testing.T) {
	raw := []byte("---\nrepos: [infracost/ci, infracost/infra]\nticket: FIX-623\n---\n# retry\n\nbody\n")

	meta, body, err := Split(raw)

	require.NoError(t, err)
	assert.Equal(t, []string{"infracost/ci", "infracost/infra"}, meta.Repos)
	assert.Equal(t, "FIX-623", meta.Ticket)
	assert.Equal(t, "# retry\n\nbody\n", string(body))
}

func TestADocumentWithNoFrontMatterIsAllBody(t *testing.T) {
	meta, body, err := Split([]byte("# just a doc\n"))

	require.NoError(t, err)
	assert.Empty(t, meta.Repos)
	assert.Equal(t, "# just a doc\n", string(body))
}

func TestFrontMatterOnlyCountsWhenItOpensTheFile(t *testing.T) {
	raw := []byte("# a doc\n\n---\nrepos: [x/y]\n---\n")

	meta, body, err := Split(raw)

	require.NoError(t, err)
	assert.Empty(t, meta.Repos, "a horizontal rule mid-document is not front matter")
	assert.Equal(t, string(raw), string(body))
}

func TestFrontMatterThatDoesNotParseIsSaidRatherThanIgnored(t *testing.T) {
	_, _, err := Split([]byte("---\nrepos: [unclosed\n---\n# x\n"))
	require.Error(t, err)
}

func TestWhatGoesInComesBackOut(t *testing.T) {
	meta := Meta{Repos: []string{"a/b"}, Ticket: "T-1", Title: "thing"}

	raw, err := Join(meta, []byte("# thing\n"))
	require.NoError(t, err)
	back, body, err := Split(raw)

	require.NoError(t, err)
	assert.Equal(t, meta, back)
	assert.Equal(t, "# thing\n", string(body))
}

func TestNothingToSayWritesNoFrontMatterBlock(t *testing.T) {
	raw, err := Join(Meta{}, []byte("# bare\n"))
	require.NoError(t, err)
	assert.Equal(t, "# bare\n", string(raw))
}

func TestADesignIsADirectoryWithOneDocumentInIt(t *testing.T) {
	root := t.TempDir()

	got, err := Write(root, "One Window", Meta{Repos: []string{"owenrumney/ade"}}, nil)

	require.NoError(t, err)
	assert.Equal(t, "One-Window", got.Name)
	assert.Equal(t, filepath.Join(root, "One-Window", File), got.File)
	raw, err := os.ReadFile(got.File)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "repos:")
	assert.Contains(t, string(raw), "# One-Window")
}

func TestWritingOverADesignIsRefused(t *testing.T) {
	root := t.TempDir()
	_, err := Write(root, "one", Meta{}, nil)
	require.NoError(t, err)

	_, err = Write(root, "one", Meta{}, nil)

	require.ErrorContains(t, err, "already in the store")
}

func TestListIsNewestFirstAndSkipsWhatIsNotADesign(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"old", "new"} {
		_, err := Write(root, name, Meta{}, nil)
		require.NoError(t, err)
	}
	old := filepath.Join(root, "old", File)
	require.NoError(t, os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)))
	// The store's own git directory, and a stray folder with no document.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o755))

	got, err := List(root)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"new", "old"}, []string{got[0].Name, got[1].Name})
}

func TestAStoreThatIsNotThereYetListsNothingRatherThanFailing(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestANameThatIsAllPunctuationIsNotAName(t *testing.T) {
	_, err := Write(t.TempDir(), "///", Meta{}, nil)
	require.Error(t, err)
}

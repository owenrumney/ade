package designs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func docs(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	return dir
}

func TestImportAddsTheRepositoryToEachDocument(t *testing.T) {
	root := t.TempDir()
	from := docs(t, map[string]string{
		"one-window.md": "# One window\n\nbody\n",
		"notes.txt":     "not a design",
	})

	got, err := Import(root, from, "owenrumney/ade")

	require.NoError(t, err)
	require.Len(t, got, 1, "only markdown is a design")
	assert.True(t, got[0].Took())

	design, err := Read(root, "one-window")
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade"}, design.Meta.Repos)
	assert.Equal(t, "One window", design.Meta.Title, "the first heading is what it calls itself")
}

func TestImportKeepsFrontMatterAlreadyThereAndDoesNotRepeatTheRepo(t *testing.T) {
	root := t.TempDir()
	from := docs(t, map[string]string{
		"x.md": "---\nrepos: [owenrumney/ade]\nticket: FIX-1\n---\n# X\n",
	})

	_, err := Import(root, from, "owenrumney/ade")
	require.NoError(t, err)

	design, err := Read(root, "x")
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade"}, design.Meta.Repos)
	assert.Equal(t, "FIX-1", design.Meta.Ticket)
}

func TestADocumentAlreadyInTheStoreIsLeftBehindRatherThanOverwritten(t *testing.T) {
	root := t.TempDir()
	_, err := Write(root, "x", Meta{Ticket: "mine"}, []byte("# mine\n"))
	require.NoError(t, err)
	from := docs(t, map[string]string{"x.md": "# theirs\n"})

	got, err := Import(root, from, "a/b")

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Took())
	design, err := Read(root, "x")
	require.NoError(t, err)
	assert.Equal(t, "mine", design.Meta.Ticket, "what was there is what stays")
}

func TestTheOriginalIsLeftWhereItWas(t *testing.T) {
	root := t.TempDir()
	from := docs(t, map[string]string{"x.md": "# X\n"})

	_, err := Import(root, from, "a/b")

	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(from, "x.md"))
}

func TestImportingFromNowhereIsNotAnError(t *testing.T) {
	got, err := Import(t.TempDir(), filepath.Join(t.TempDir(), "nope"), "a/b")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestSummarySaysBothNumbers(t *testing.T) {
	assert.Equal(t, "1 imported, 1 left behind",
		Summary([]Imported{{Name: "a"}, {Name: "b", Why: "already there"}}))
}

package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitDirWithDoc is a common git dir and a docs directory holding name.
func gitDirWithDoc(t *testing.T, name string) (string, string) {
	t.Helper()
	common := filepath.Join(t.TempDir(), ".git")
	docsDir := filepath.Join(common, "ade", "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	if name != "" {
		require.NoError(t, os.WriteFile(filepath.Join(docsDir, name), []byte("# d\n"), 0o600))
	}
	return common, docsDir
}

func TestNothingIsRecordedUntilSomethingIsOpened(t *testing.T) {
	common, _ := gitDirWithDoc(t, "")

	file, ok := Opened(common, "main", "s1")
	assert.False(t, ok, "no record, so match by name")
	assert.Empty(t, file)
}

func TestAClosedDocumentIsRecordedApartFromAnUnknownOne(t *testing.T) {
	// The whole reason Opened has two return values: "closed" and "never
	// opened" want opposite behaviour from Find.
	common, _ := gitDirWithDoc(t, "")
	require.NoError(t, SetOpened(common, "main", "s1", ""))

	file, ok := Opened(common, "main", "s1")
	assert.True(t, ok, "recorded")
	assert.Empty(t, file, "and what is recorded is that nothing is open")
}

func TestARecordedDocumentIsFoundWhateverItIsCalled(t *testing.T) {
	// The point of the record. project-memory.md matches nothing about main,
	// so before this it was a file the Design tab never opened again.
	common, docsDir := gitDirWithDoc(t, "project-memory.md")
	require.NoError(t, SetOpened(common, "main", "s1", "project-memory.md"))

	dir, file, ok := Find([]string{docsDir}, common, "main", "s1")
	require.True(t, ok)
	assert.Equal(t, docsDir, dir)
	assert.Equal(t, "project-memory.md", file)
}

func TestARecordedCloseStopsFindMatchingByName(t *testing.T) {
	// main.md would match the branch by name. Closing it has to outlast the
	// process, or the next start opens it straight back up.
	common, docsDir := gitDirWithDoc(t, "main.md")
	require.NoError(t, SetOpened(common, "main", "s1", ""))

	_, _, ok := Find([]string{docsDir}, common, "main", "s1")
	assert.False(t, ok)
}

func TestARecordedDocumentThatHasGoneFallsBackToTheNameMatch(t *testing.T) {
	// Deleted by hand between runs. The branch still has a document by name,
	// and an empty tab would be the wrong answer.
	common, docsDir := gitDirWithDoc(t, "FIX-623.md")
	require.NoError(t, SetOpened(common, "fix-623", "s1", "gone.md"))

	_, file, ok := Find([]string{docsDir}, common, "fix-623", "s1")
	require.True(t, ok)
	assert.Equal(t, "FIX-623.md", file)
}

func TestEachBranchIsRememberedSeparately(t *testing.T) {
	common, docsDir := gitDirWithDoc(t, "project-memory.md")
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "sandboxing.md"), []byte("# s\n"), 0o600))
	require.NoError(t, SetOpened(common, "main", "s1", "project-memory.md"))
	require.NoError(t, SetOpened(common, "feat/sandbox", "s1", "sandboxing.md"))

	_, file, ok := Find([]string{docsDir}, common, "main", "s1")
	require.True(t, ok)
	assert.Equal(t, "project-memory.md", file)

	_, file, ok = Find([]string{docsDir}, common, "feat/sandbox", "s1")
	require.True(t, ok)
	assert.Equal(t, "sandboxing.md", file)
}

func TestReopeningReplacesWhatWasRecorded(t *testing.T) {
	common, docsDir := gitDirWithDoc(t, "second.md")
	require.NoError(t, SetOpened(common, "main", "s1", "first.md"))
	require.NoError(t, SetOpened(common, "main", "s1", "second.md"))

	_, file, ok := Find([]string{docsDir}, common, "main", "s1")
	require.True(t, ok)
	assert.Equal(t, "second.md", file)
}

func TestWithoutAGitDirThereIsNowhereToRecord(t *testing.T) {
	// The launcher and a snapshot serve no checkout. Recording is a no-op
	// rather than an error, and Find still matches by name.
	require.NoError(t, SetOpened("", "main", "s1", "x.md"))
	_, ok := Opened("", "main", "s1")
	assert.False(t, ok)
}

// The bug this pins: a new conversation on the same branch used to open the
// last one's document, and the agent read it as its brief.
func TestANewSessionDoesNotInheritTheLastOnesDocument(t *testing.T) {
	common, docsDir := gitDirWithDoc(t, "files-tab.md")
	require.NoError(t, SetOpened(common, "main", "first-session", "files-tab.md"))

	_, _, ok := Find([]string{docsDir}, common, "main", "second-session")
	assert.False(t, ok, "another session's design document is not this session's brief")

	_, ok = Opened(common, "main", "second-session")
	assert.False(t, ok, "and it does not read as a recorded close either, which would stop name matching")

	_, file, ok := Find([]string{docsDir}, common, "main", "first-session")
	require.True(t, ok, "the session that opened it still has it")
	assert.Equal(t, "files-tab.md", file)
}

// A conversation has no id until its first turn, and that is not a licence to
// adopt what a named session left open.
func TestASessionWithoutAnIdYetInheritsNothing(t *testing.T) {
	common, docsDir := gitDirWithDoc(t, "files-tab.md")
	require.NoError(t, SetOpened(common, "main", "first-session", "files-tab.md"))

	_, _, ok := Find([]string{docsDir}, common, "main", "")
	assert.False(t, ok, "no id yet, so no claim on another session's document")
}

// A record written before the session was part of it belongs to whoever is
// asking: an upgrade must not close the document the user had open.
func TestARecordFromBeforeTheUpgradeStillOpens(t *testing.T) {
	common, docsDir := gitDirWithDoc(t, "project-memory.md")
	require.NoError(t, os.WriteFile(filepath.Join(common, "ade", "open-docs.json"),
		[]byte(`{"main":"project-memory.md"}`), 0o600))

	_, file, ok := Find([]string{docsDir}, common, "main", "any-session")
	require.True(t, ok)
	assert.Equal(t, "project-memory.md", file)
}

// The session that opened it is written down, so the next start of the same
// session finds it and another session does not.
func TestTheSessionIsWrittenDownWithTheDocument(t *testing.T) {
	common, _ := gitDirWithDoc(t, "cart.md")
	require.NoError(t, SetOpened(common, "main", "s1", "cart.md"))

	body, err := os.ReadFile(filepath.Join(common, "ade", "open-docs.json"))
	require.NoError(t, err)
	assert.Contains(t, string(body), `"session": "s1"`)
}

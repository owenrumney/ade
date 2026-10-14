package todo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func storeIn(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), File), "")
	require.NoError(t, err)
	return s
}

func TestACaptureNeedsOnlyWords(t *testing.T) {
	s := storeIn(t)

	item, err := s.Add(Item{Text: "  make the bell ring  "})

	require.NoError(t, err)
	assert.Equal(t, "make the bell ring", item.Text)
	assert.Equal(t, FromYou, item.From)
	assert.NotEmpty(t, item.ID)
	assert.False(t, item.Done())
}

func TestAnEmptyCaptureIsRefused(t *testing.T) {
	s := storeIn(t)

	_, err := s.Add(Item{Text: "   "})

	require.Error(t, err)
	assert.NoFileExists(t, s.Path(), "and nothing is written for it")
}

func TestWhatWasFiledIsThereOnTheNextOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	first, err := Open(path, "")
	require.NoError(t, err)
	_, err = first.Add(Item{Text: "ship the todo list", From: FromAgent, Session: "s1"})
	require.NoError(t, err)

	again, err := Open(path, "ade")
	require.NoError(t, err)

	list := again.List()
	require.Len(t, list, 1)
	assert.Equal(t, "ship the todo list", list[0].Text)
	assert.Equal(t, FromAgent, list[0].From)
	assert.Equal(t, "s1", list[0].Session)
	assert.Equal(t, "ade", list[0].Repo, "the store says which checkout answered")
}

func TestTheRepoLabelIsNotWrittenToDisk(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, File), "ade")
	require.NoError(t, err)
	_, err = s.Add(Item{Text: "a repo item"})
	require.NoError(t, err)

	body, err := os.ReadFile(filepath.Join(dir, File))

	require.NoError(t, err)
	assert.NotContains(t, string(body), `"repo"`, "the file it lives in is what scopes it")
}

func TestTheListReadsOpenFirstAndSoonestFirst(t *testing.T) {
	s := storeIn(t)
	soon := time.Now().Add(time.Hour)
	later := time.Now().Add(48 * time.Hour)
	_, err := s.Add(Item{Text: "undated"})
	require.NoError(t, err)
	_, err = s.Add(Item{Text: "later", Due: &later})
	require.NoError(t, err)
	done, err := s.Add(Item{Text: "finished"})
	require.NoError(t, err)
	_, err = s.Add(Item{Text: "soon", Due: &soon})
	require.NoError(t, err)
	_, err = s.Update(done.ID, Patch{Done: new(true)})
	require.NoError(t, err)

	var order []string
	for _, item := range s.List() {
		order = append(order, item.Text)
	}

	assert.Equal(t, []string{"soon", "later", "undated", "finished"}, order)
}

func TestADateIsSetAndTakenOffAgain(t *testing.T) {
	s := storeIn(t)
	item, err := s.Add(Item{Text: "set a due date"})
	require.NoError(t, err)
	when := time.Now().Add(time.Hour)

	dated, err := s.Update(item.ID, Patch{Due: &when})
	require.NoError(t, err)
	require.NotNil(t, dated.Due)

	cleared, err := s.Update(item.ID, Patch{Due: new(time.Time)})
	require.NoError(t, err)
	assert.Nil(t, cleared.Due, "a zero time is how a date is removed")
}

func TestAnUnsetFieldLeavesWhatIsStoredAlone(t *testing.T) {
	s := storeIn(t)
	when := time.Now().Add(time.Hour)
	item, err := s.Add(Item{Text: "keep my note", Note: "web.go:42", Due: &when})
	require.NoError(t, err)

	updated, err := s.Update(item.ID, Patch{Started: new("s7")})

	require.NoError(t, err)
	assert.Equal(t, "web.go:42", updated.Note)
	assert.Equal(t, "keep my note", updated.Text)
	require.NotNil(t, updated.Due)
	assert.Equal(t, "s7", updated.Started)
}

func TestDoneIsRecordedAndUndone(t *testing.T) {
	s := storeIn(t)
	item, err := s.Add(Item{Text: "finish the store"})
	require.NoError(t, err)

	done, err := s.Update(item.ID, Patch{Done: new(true)})
	require.NoError(t, err)
	assert.True(t, done.Done())

	reopened, err := s.Update(item.ID, Patch{Done: new(false)})
	require.NoError(t, err)
	assert.False(t, reopened.Done())
}

func TestOverdueCountsOnlyWhatIsStillOpen(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	now := time.Now()

	assert.True(t, Item{Due: &past}.Overdue(now))
	assert.False(t, Item{Due: &past, DoneAt: &past}.Overdue(now), "done is never late")
	assert.False(t, Item{}.Overdue(now), "and undated is never late")
}

func TestTheLastOneLeavingRemovesTheFile(t *testing.T) {
	s := storeIn(t)
	item, err := s.Add(Item{Text: "the only one"})
	require.NoError(t, err)
	require.FileExists(t, s.Path())

	require.NoError(t, s.Delete(item.ID))

	assert.NoFileExists(t, s.Path())
}

func TestAnIdFromAnotherStoreIsNotFound(t *testing.T) {
	s := storeIn(t)

	_, err := s.Update("nothing", Patch{Done: new(true)})
	assert.ErrorIs(t, err, ErrNotFound)
	assert.ErrorIs(t, s.Delete("nothing"), ErrNotFound)
}

func TestARepoStoreSitsBesideTheDesignsAndTheComments(t *testing.T) {
	git := t.TempDir()

	assert.Equal(t, filepath.Join(git, "ade", File), RepoPath(git))
}

package commitrec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/wiki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWiki is the shared wiki reduced to what publishing touches: a draft
// directory to write into, and the pages a previous submit left behind.
type fakeWiki struct {
	root      string
	repo      string
	off       bool
	pages     map[string]string
	submitted int
	reject    string
}

// A key is a patch-id, so a test key is hex: anything else is refused.
const (
	key1  = "1111111111111111111111111111111111111111"
	key2  = "2222222222222222222222222222222222222222"
	key3  = "3333333333333333333333333333333333333333"
	merge = "9999999999999999999999999999999999999999"
)

func newFakeWiki(t *testing.T) *fakeWiki {
	t.Helper()
	return &fakeWiki{root: t.TempDir(), repo: "owenrumney/ade", pages: map[string]string{}}
}

func (f *fakeWiki) Enabled() bool { return !f.off }

func (f *fakeWiki) Draft(_ context.Context, session, _ string) (wiki.Draft, error) {
	return wiki.Draft{Session: session, Root: f.root, Repo: f.repo}, nil
}

func (f *fakeWiki) Submit(_ context.Context, _ wiki.Draft, _ string) (wiki.Submission, error) {
	if f.reject != "" {
		return wiki.Submission{State: "rejected", Error: f.reject}, nil
	}
	f.submitted++
	return wiki.Submission{State: "published"}, filepath.WalkDir(f.root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(f.root, path)
		data, err := os.ReadFile(path) // #nosec G304 -- the fake draft's own root
		if err != nil {
			return err
		}
		f.pages[filepath.ToSlash(rel)] = string(data)
		return nil
	})
}

func (f *fakeWiki) Page(path string) ([]byte, error) {
	body, ok := f.pages[path]
	if !ok {
		return nil, errors.New("no such page")
	}
	return []byte(body), nil
}

func TestARecordIsPublishedAndReadBackByItsKey(t *testing.T) {
	w := newFakeWiki(t)
	record := Record{Repo: w.repo, Keys: []string{key1}, Commit: "abc", Subject: "feat: a"}

	require.NoError(t, Save(context.Background(), w, t.TempDir(), w.repo, []Record{record}, nil))

	got, ok := Load(w, w.repo, key1)
	require.True(t, ok)
	assert.Equal(t, "feat: a", got.Subject)
	assert.Equal(t, "abc", got.Commit)
}

func TestACommitWithNoDiffOfItsOwnIsNotFiled(t *testing.T) {
	w := newFakeWiki(t)

	require.NoError(t, Save(context.Background(), w, t.TempDir(), w.repo, []Record{{Repo: w.repo, Commit: "merge"}}, nil))

	assert.Empty(t, w.pages, "a merge commit has no patch-id and nothing it uniquely carries")
}

func TestTheRecordsAndTheIndexLandInOneSubmit(t *testing.T) {
	w := newFakeWiki(t)
	idx := Index{}.Put(Entry{Merge: merge, PR: 7, Keys: []string{key1}})

	require.NoError(t, Save(context.Background(), w, t.TempDir(), w.repo, []Record{{Repo: w.repo, Keys: []string{key1}}}, idx))

	assert.Equal(t, 1, w.submitted, "a record and the merge it belongs to cannot land half-applied")
	assert.Equal(t, []string{key1}, LoadIndex(w, w.repo).KeysFor(merge))
}

func TestARecordIsNotFiledUnderAKeyItsOwnFileDoesNotClaim(t *testing.T) {
	w := newFakeWiki(t)
	// What anyone who can push to the wiki may write: a file naming another path.
	w.pages[Path(w.repo, key1)] = Render(Record{Keys: []string{"../../../../../escaped"}})

	_, ok := Load(w, w.repo, key1)
	assert.False(t, ok, "a key that is not a patch-id is not a key")
}

func TestARecordCannotBePublishedIntoAnotherRepositorysDraft(t *testing.T) {
	w := newFakeWiki(t)
	w.repo = "someone/else"

	err := Save(context.Background(), w, t.TempDir(), "owenrumney/ade", []Record{{Keys: []string{key1}}}, nil)

	require.Error(t, err)
	assert.Empty(t, w.pages)
}

func TestARejectedSubmitIsReportedRatherThanSwallowed(t *testing.T) {
	w := newFakeWiki(t)
	w.reject = "suspected secret (aws key)"

	err := Save(context.Background(), w, t.TempDir(), w.repo, []Record{{Repo: w.repo, Keys: []string{key1}}}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "suspected secret")
}

func TestWithNoWikiThereIsNowhereToPublish(t *testing.T) {
	w := newFakeWiki(t)
	w.off = true

	err := Save(context.Background(), w, t.TempDir(), w.repo, []Record{{Repo: w.repo, Keys: []string{key1}}}, nil)
	require.Error(t, err)
}

func TestAMissingRecordIsAnAnswerNotAFailure(t *testing.T) {
	w := newFakeWiki(t)
	_, ok := Load(w, w.repo, key3)
	assert.False(t, ok)
	assert.Nil(t, LoadIndex(w, w.repo))
}

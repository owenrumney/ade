package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPruneRemovesOnlyACleanAbandonedDraft(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	repo := checkoutWithOrigin(t)
	clean, err := store.Draft(context.Background(), "clean-one", repo)
	require.NoError(t, err)
	changed, err := store.Draft(context.Background(), "changed-one", repo)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(changed.Root, "general", "new.md"), []byte("kept\n"), 0o600))
	age(t, clean.Root)
	age(t, changed.Root)

	report, err := store.Prune(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"clean-one"}, report.Removed)
	require.Len(t, report.Retained, 1)
	assert.Equal(t, "changed-one", report.Retained[0].Session)
	assert.NoDirExists(t, clean.Root)
	assert.DirExists(t, changed.Root)
}

func TestPruneKeepsADraftASessionIsStillUsing(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	draft, err := store.Draft(context.Background(), "live-one", checkoutWithOrigin(t))
	require.NoError(t, err)

	report, err := store.Prune(context.Background())
	require.NoError(t, err)
	assert.Empty(t, report.Removed)
	assert.DirExists(t, draft.Root)
}

func TestPruneExpiresAnUnclaimedPublishClone(t *testing.T) {
	state := t.TempDir()
	store := New(state, newRemote(t))
	orphan := filepath.Join(state, "wiki-publish", "gone-1234")
	require.NoError(t, os.MkdirAll(orphan, 0o700))

	report, err := store.Prune(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"gone-1234"}, report.Clones)
	assert.NoDirExists(t, orphan)
}

func TestDiscardTakesTheRetainedPublishCloneWithIt(t *testing.T) {
	state := t.TempDir()
	store := New(state, newRemote(t))
	draft, err := store.Draft(context.Background(), "conflicted-one", checkoutWithOrigin(t))
	require.NoError(t, err)
	clone := filepath.Join(state, "wiki-publish", "conflicted-one-abcdef")
	require.NoError(t, os.MkdirAll(clone, 0o700))
	store.retainPublish(draft.Session, clone)

	require.NoError(t, store.Discard(draft.Session))
	assert.NoDirExists(t, draft.Root)
	assert.NoDirExists(t, clone)
}

// age backdates a draft past the settle window, which is what tells prune the
// session behind it has gone.
func age(t *testing.T, root string) {
	t.Helper()
	when := time.Now().Add(-2 * settleWindow)
	require.NoError(t, filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, when, when)
	}))
}

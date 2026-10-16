package wiki

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADesignIsPublishedUnderTheRepositorysDesigns(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)

	out, err := store.PublishDesign(context.Background(), distillRepo, "purge-design-docs", "# Purge design docs\n\nArchive distils.")
	require.NoError(t, err)
	assert.Equal(t, "published", out.State)
	assert.Equal(t, "# Purge design docs\n\nArchive distils.\n", remotePage(t, remote, "repos/owner/repo/designs/purge-design-docs.md"))
	assert.NoDirExists(t, store.DraftRoot(archiveSession(distillRepo)))
}

func TestPublishingADesignTwiceIsUnchanged(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	page := "# Same\n\nNothing new."

	_, err := store.PublishDesign(context.Background(), distillRepo, "same", page)
	require.NoError(t, err)
	again, err := store.PublishDesign(context.Background(), distillRepo, "same", page)
	require.NoError(t, err)
	assert.Equal(t, "unchanged", again.State)
}

func TestWhatPublishADesignRefuses(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	for name, call := range map[string]func() error{
		"no repository": func() error {
			_, err := store.PublishDesign(context.Background(), "owner", "x", "# X")
			return err
		},
		"a name with nothing in it": func() error {
			_, err := store.PublishDesign(context.Background(), distillRepo, "../..", "# X")
			return err
		},
		"an empty page": func() error {
			_, err := store.PublishDesign(context.Background(), distillRepo, "x", "   ")
			return err
		},
		"a page over the cap": func() error {
			_, err := store.PublishDesign(context.Background(), distillRepo, "x", strings.Repeat("x", MaxPage+1))
			return err
		},
	} {
		t.Run(name, func(t *testing.T) { assert.Error(t, call()) })
	}

	t.Run("a wiki that is disabled", func(t *testing.T) {
		off := New(t.TempDir(), "")
		_, err := off.PublishDesign(context.Background(), distillRepo, "x", "# X")
		assert.Error(t, err)
	})
}

func TestADesignPathIsMadeOfTheNameAndNothingElse(t *testing.T) {
	assert.Equal(t, "repos/owner/repo/designs/purge-design-docs.md", DesignPath(distillRepo, "purge-design-docs"))
	assert.Equal(t, "repos/owner/repo/designs/dev-82.md", DesignPath(distillRepo, "../dev-82"))
	assert.Empty(t, DesignPath(distillRepo, "../.."))
}

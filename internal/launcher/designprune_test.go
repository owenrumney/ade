package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/archive"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/jobs"
)

// originCheckout is a repository with an origin, which is what puts it in the
// list the design-archive job sweeps.
func originCheckout(t *testing.T, dir, repo string) string {
	t.Helper()
	gitCheckout(t, dir)
	git(t, dir, "remote", "add", "origin", "https://github.com/"+repo+".git")
	return dir
}

func prunableOf(t *testing.T, s *Server, repo string) prunable {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/designs/prunable?repo="+repo, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got prunable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

func TestTheReposAPruneSpansAreTheOnesTheJobSweeps(t *testing.T) {
	m := newFixture(t)
	originCheckout(t, filepath.Join(m.code, "ade"), "owenrumney/ade")
	gitCheckout(t, filepath.Join(m.code, "no-origin"))

	rec := do(t, newLauncherOn(t, m), http.MethodGet, "/api/designs/repos", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Repos []designRepo `json:"repos"`
		Wiki  wikiState    `json:"wiki"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	require.Len(t, got.Repos, 1, "a checkout with no origin has no wiki scope to archive into")
	assert.Equal(t, "owenrumney/ade", got.Repos[0].Repo)
	assert.False(t, got.Wiki.Enabled)
	assert.Contains(t, got.Wiki.Why, "ade wiki init")
}

func TestTheSweepBecomesThreeGroupsWithTheRestOfTheDocuments(t *testing.T) {
	m := newFixture(t)
	dir := originCheckout(t, filepath.Join(m.code, "ade"), "owenrumney/ade")
	docs := filepath.Join(dir, ".git", "ade", "docs")
	require.NoError(t, os.MkdirAll(docs, 0o750))
	for _, name := range []string{"merged-thing.md", "dropped-thing.md", "old-notes.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(docs, name), []byte("# x\n"), 0o600))
	}

	s := newLauncherOn(t, m)
	s.look = func(_ context.Context, target jobs.ArchiveTarget) (archive.State, error) {
		assert.Equal(t, "owenrumney/ade", target.Repo)
		return archive.State{
			Docs: []archive.Doc{
				{Dir: docs, File: "merged-thing.md"},
				{Dir: docs, File: "dropped-thing.md"},
				{Dir: docs, File: "old-notes.md"},
			},
			Branches: []string{"main"},
			Heads: []ghpr.Head{
				{Number: 11, Branch: "merged-thing", State: "MERGED", URL: "u11"},
				{Number: 12, Branch: "dropped-thing", State: "CLOSED", URL: "u12"},
			},
		}, nil
	}

	got := prunableOf(t, s, "owenrumney/ade")
	require.Len(t, got.Merged, 1)
	assert.Equal(t, "merged-thing.md", got.Merged[0].File)
	assert.Equal(t, 11, got.Merged[0].Number)
	require.Len(t, got.Abandoned, 1)
	assert.Equal(t, "dropped-thing.md", got.Abandoned[0].File)
	require.Len(t, got.Unmatched, 1, "the pile the sweep can never see is most of what accumulates")
	assert.Equal(t, "old-notes.md", got.Unmatched[0].File)
	assert.False(t, got.Unmatched[0].Touched.IsZero(), "an old one has to stand out")
	assert.Empty(t, got.Error)
}

// One `gh` without auth would otherwise hide every other repository.
func TestARepositoryThatCannotBeSweptIsARowAndNotADeadCall(t *testing.T) {
	m := newFixture(t)
	originCheckout(t, filepath.Join(m.code, "ade"), "owenrumney/ade")

	s := newLauncherOn(t, m)
	s.look = func(context.Context, jobs.ArchiveTarget) (archive.State, error) {
		return archive.State{}, errors.New("gh: not authenticated")
	}

	got := prunableOf(t, s, "owenrumney/ade")
	assert.Equal(t, "gh: not authenticated", got.Error)
	assert.Empty(t, got.Merged)
	assert.Empty(t, got.Abandoned)
	assert.Empty(t, got.Unmatched)
}

func TestAPruneIsOnlyOfferedForARepositoryThisMachineHolds(t *testing.T) {
	s := newLauncherOn(t, newFixture(t))

	rec := do(t, s, http.MethodGet, "/api/designs/prunable?repo=someone/else", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = do(t, s, http.MethodGet, "/api/designs/prunable", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, http.MethodPost, "/api/designs/archive", `{"repo":"someone/else","file":"x.md"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = do(t, s, http.MethodPost, "/api/designs/archive", `{"repo":"","file":""}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Said once, before a document is distilled, rather than as one identical row
// failure per document.
func TestArchivingWithoutAWikiIsRefusedBeforeAnyModelRuns(t *testing.T) {
	m := newFixture(t)
	dir := originCheckout(t, filepath.Join(m.code, "ade"), "owenrumney/ade")
	docs := filepath.Join(dir, ".git", "ade", "docs")
	require.NoError(t, os.MkdirAll(docs, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docs, "merged-thing.md"), []byte("# x\n"), 0o600))

	s := newLauncherOn(t, m)
	s.jobs.archive = func(context.Context, string) (string, error) {
		t.Fatal("no model may run with nowhere to publish to")
		return "", nil
	}

	rec := do(t, s, http.MethodPost, "/api/designs/archive", `{"repo":"owenrumney/ade","file":"merged-thing.md"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "ade wiki init")
	assert.FileExists(t, filepath.Join(docs, "merged-thing.md"))
}

func TestTheArchiveGateHoldsOneArchiverAtATime(t *testing.T) {
	s := newLauncherOn(t, newFixture(t))

	release, err := s.holdArchive(context.Background())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = s.holdArchive(ctx)
	assert.ErrorIs(t, err, errArchiveBusy, "the job runs on a schedule you do not control")

	release()
	again, err := s.holdArchive(context.Background())
	require.NoError(t, err)
	again()
}

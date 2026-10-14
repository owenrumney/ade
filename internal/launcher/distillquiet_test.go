package launcher

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/wiki"
)

// quietLauncher is a launcher with distillation turned on and a checkout
// whose origin names a repository.
func quietLauncher(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	remote := wikiRemote(t)
	require.NoError(t, settings.Save(settings.Settings{WikiRemote: remote, WikiDistill: true}))
	s := newTestLauncher(t)
	s.wiki = wiki.New(filepath.Join(home, ".ade"), remote)
	t.Cleanup(s.stopDistilling)

	checkout := filepath.Join(home, "code", "repo")
	require.NoError(t, os.MkdirAll(checkout, 0o700))
	gitRun(t, checkout, "init", "--initial-branch", "main")
	gitRun(t, checkout, "remote", "add", "origin", "git@github.com:owner/repo.git")
	return s, checkout
}

// The wait lives here because a session exiting is the case that most needs
// distilling and the one least able to do it.
func TestAQuietCheckoutArmsADistillation(t *testing.T) {
	s, checkout := quietLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/quiet", `{"dir":"`+checkout+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	assert.Contains(t, s.quiet, "owner/repo")
}

func TestTwoSessionsInARepositoryShareTheOneWait(t *testing.T) {
	s, checkout := quietLauncher(t)

	for range 3 {
		rec := do(t, s, http.MethodPost, "/api/quiet", `{"dir":"`+checkout+`"}`)
		require.Equal(t, http.StatusOK, rec.Code)
	}

	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	assert.Len(t, s.quiet, 1, "a repository is distilled once, however many sessions went quiet")
}

func TestACheckoutWithNoOriginArmsNothing(t *testing.T) {
	s, _ := quietLauncher(t)
	bare := t.TempDir()
	gitRun(t, bare, "init", "--initial-branch", "main")

	rec := do(t, s, http.MethodPost, "/api/quiet", `{"dir":"`+bare+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, "nothing the session can act on")

	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	assert.Empty(t, s.quiet, "there is no wiki scope to write to")
}

func TestDistillationOffArmsNothing(t *testing.T) {
	s, checkout := quietLauncher(t)
	require.NoError(t, settings.Save(settings.Settings{WikiRemote: wikiRemote(t)}))

	rec := do(t, s, http.MethodPost, "/api/quiet", `{"dir":"`+checkout+`"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	assert.Empty(t, s.quiet, "publishing from transcripts is a choice")
}

// A launcher going down must not leave a timer holding a model call.
func TestShuttingDownDropsThePendingFires(t *testing.T) {
	s, checkout := quietLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/quiet", `{"dir":"`+checkout+`"}`).Code)

	s.stopDistilling()

	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	assert.Empty(t, s.quiet)
}

func TestTheWaitOutlastsTheQuietPeriod(t *testing.T) {
	assert.Greater(t, distillAfter, wiki.DistillQuiet,
		"a fire inside it reads nothing: unread refuses a transcript that fresh")
	assert.Less(t, distillAfter, wiki.DistillQuiet+5*time.Minute)
}

func TestTheFirstFireOnlyMarksWhereTheTranscriptsEnd(t *testing.T) {
	s, _ := quietLauncher(t)

	// No marks yet: what was said before distillation was turned on is never
	// published, and writing the marks calls no model.
	first := s.distillOne(context.Background(), "owner/repo")
	assert.Equal(t, "started", first.State)
	assert.Empty(t, first.Error)

	// And a repository no checkout here belongs to is not an error.
	out := s.distillOne(context.Background(), "nobody/nothing")
	assert.Empty(t, out.Error)
	assert.Empty(t, out.State)
}

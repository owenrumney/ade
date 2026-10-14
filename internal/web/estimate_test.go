package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/estimate"
	"github.com/owenrumney/ade/internal/settings"
)

func getEstimate(t *testing.T, s *Server) BuildEstimate {
	t.Helper()
	rec := do(t, s, "GET", "/api/design/estimate?provider=claude", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body BuildEstimate
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestAThinHistoryGivesNoEstimate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")

	assert.Nil(t, getEstimate(t, s).Estimate, "a made-up number is worse than none")
}

func TestNoOpenDesignGivesNoEstimate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.setDoc("")

	assert.Nil(t, getEstimate(t, s).Estimate)
}

func TestAFinishedBuildIsRecordedAndAStoppedOneMarked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")

	target := s.buildTarget("opus")
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	s.recordBuild(target, buildFrom{}, at, at.Add(40*time.Minute), false)
	s.recordBuild(target, buildFrom{}, at.Add(time.Hour), at.Add(time.Hour+4*time.Minute), true)

	dir, err := settings.Dir()
	require.NoError(t, err)
	runs, err := estimate.Recorded(dir)
	require.NoError(t, err)
	require.Len(t, runs, 2)
	assert.Equal(t, target.Repo, runs[0].Repo)
	assert.Equal(t, "opus", runs[0].Model)
	assert.Equal(t, 40*time.Minute, runs[0].Duration())
	assert.False(t, runs[0].Failed)
	assert.True(t, runs[1].Failed)
}

func TestPastBuildsOfTheOpenDesignGiveARange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")

	target := s.buildTarget("")
	dir, err := settings.Dir()
	require.NoError(t, err)
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for _, mins := range []int{30, 40, 50} {
		require.NoError(t, estimate.Record(dir, estimate.Run{
			Kind: estimate.KindBuild, Repo: target.Repo, DocWords: target.DocWords,
			ChecklistItems: target.ChecklistItems, Started: at, Finished: at.Add(time.Duration(mins) * time.Minute),
		}))
		at = at.Add(24 * time.Hour)
	}

	got := getEstimate(t, s).Estimate
	require.NotNil(t, got)
	assert.Equal(t, 35*60, got.LowSeconds)
	assert.Equal(t, 45*60, got.HighSeconds)
	assert.Equal(t, 3, got.Runs)
	assert.Nil(t, got.Window, "no plan reading, no share of it")
}

package web

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/sharpen"
)

// sharpenAnswers stands in for claude and counts how often it was asked.
func sharpenAnswers(t *testing.T, answer func(bridge.OneShotOpts) (*bridge.Turn, error)) *int {
	t.Helper()
	calls := 0
	old := runOneShot
	runOneShot = func(_ context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
		calls++
		return answer(opts)
	}
	t.Cleanup(func() { runOneShot = old })
	return &calls
}

func TestAVaguePromptComesBackWithChoices(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(opts bridge.OneShotOpts) (*bridge.Turn, error) {
		assert.Equal(t, sharpen.Model, opts.Model)
		assert.Equal(t, sharpen.Role(), opts.SystemPrompt)
		assert.Equal(t, []string{}, opts.Tools, "a sharpener with tools is a planner")
		assert.True(t, opts.NoMCP)
		assert.True(t, opts.NoSessionPersistence)
		assert.Contains(t, opts.Prompt, "make the login faster")
		return &bridge.Turn{Text: `[{"ask":"Which login?","options":["Web","CLI"]}]`}, nil
	})

	rec := do(t, s, "POST", "/api/sharpen", `{"prompt":"make the login faster"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"questions":[{"ask":"Which login?","options":["Web","CLI"]}]}`, rec.Body.String())
	assert.Equal(t, 1, *calls)
}

func TestAPromptTheGatePassesNeverReachesAModel(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: `[{"ask":"Which?","options":["a","b"]}]`}, nil
	})

	rec := do(t, s, "POST", "/api/sharpen", `{"prompt":"fix the nil check at internal/web/web.go:412"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"questions":[]}`, rec.Body.String())
	assert.Zero(t, *calls)
}

func TestTheSharpenerTurnedOffAsksNothing(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Update(func(into *settings.Settings) error {
		into.SharpenOff = true
		return nil
	}))
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: `[{"ask":"Which?","options":["a","b"]}]`}, nil
	})

	rec := do(t, s, "POST", "/api/sharpen", `{"prompt":"make the login faster"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"questions":[]}`, rec.Body.String())
	assert.Zero(t, *calls)
}

func TestAFailedSharpenIsNoQuestionsAndNoError(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return nil, errors.New("claude is not logged in")
	})

	rec := do(t, s, "POST", "/api/sharpen", `{"prompt":"make the login faster"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"questions":[]}`, rec.Body.String())
}

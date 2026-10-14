package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/limits"
)

func TestTheLauncherServesPlanUsage(t *testing.T) {
	// Readings are shared through ~/.ade, so a test without a home of its own
	// reads whatever the real ade on this machine last wrote.
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	reset := time.Now().Add(47 * time.Minute).Round(time.Second)
	s.limits.Ask = func(context.Context) (limits.Limits, error) {
		return limits.Limits{
			Provider:  "claude",
			CheckedAt: time.Now(),
			Windows:   []limits.Window{{Name: "5h", Percent: 68, ResetsAt: reset}},
		}, nil
	}

	// The first reading is fetched in the background, so the first ask can
	// legitimately be empty; what matters is that it arrives.
	var state limits.State
	require.Eventually(t, func() bool {
		rec := do(t, s, "GET", "/api/limits", "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &state))
		return state.Claude != nil
	}, 2*time.Second, 10*time.Millisecond)

	require.Len(t, state.Claude.Windows, 1)
	assert.Equal(t, "5h", state.Claude.Windows[0].Name)
	assert.Equal(t, 68.0, state.Claude.Windows[0].Percent)
	assert.True(t, reset.Equal(state.Claude.Windows[0].ResetsAt))
}

func TestPlanUsageNeedsTheToken(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "GET", "/api/limits?t=wrong", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAnUnchangedReadingIsNotBroadcast(t *testing.T) {
	// The page re-fetches on the event, so a key that moved when nothing did
	// would have every launcher on the fixture asking the account again.
	at := time.Now().Round(time.Second)
	one := limits.State{Claude: &limits.Limits{
		Provider: "claude", CheckedAt: at,
		Windows: []limits.Window{{Name: "5h", Percent: 68, ResetsAt: at}},
	}}
	same := limits.State{Claude: &limits.Limits{
		Provider: "claude", CheckedAt: at.Add(time.Minute),
		Windows: []limits.Window{{Name: "5h", Percent: 68, ResetsAt: at}},
	}}
	moved := limits.State{Claude: &limits.Limits{
		Provider: "claude", CheckedAt: at,
		Windows: []limits.Window{{Name: "5h", Percent: 71, ResetsAt: at}},
	}}

	assert.Equal(t, limitsKey(one), limitsKey(same), "a re-read of the same numbers is not news")
	assert.NotEqual(t, limitsKey(one), limitsKey(moved))
	assert.Empty(t, limitsKey(limits.State{}))
}

func TestTheWindowBreakdownNeedsAWindowToSlice(t *testing.T) {
	// Without a plan reading there is no reset, so no window start. Guessing
	// one would name checkouts against a period nobody metered.
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	s.limits.Ask = func(context.Context) (limits.Limits, error) {
		return limits.Limits{}, errors.New("no login")
	}
	rec := do(t, s, "GET", "/api/limits/window", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{}`, rec.Body.String(), "no window means no fields, not a zero time the page has to read")
}

func TestTheWindowOpensFiveHoursBeforeItResets(t *testing.T) {
	reset := time.Date(2026, 9, 5, 16, 40, 0, 0, time.UTC)
	since, ok := shortWindowStart(limits.State{Claude: &limits.Limits{
		Windows: []limits.Window{
			{Name: "7d", Percent: 24, ResetsAt: reset.AddDate(0, 0, 3)},
			{Name: "5h", Percent: 68, ResetsAt: reset},
		},
	}})
	require.True(t, ok)
	assert.Equal(t, reset.Add(-5*time.Hour), since, "the long window is not the one sliced")
}

func TestAWindowWithNoResetIsNoWindow(t *testing.T) {
	// Codex reports a window with no reset time when it has not been told one.
	_, ok := shortWindowStart(limits.State{Codex: &limits.Limits{
		Windows: []limits.Window{{Name: "5h", Percent: 9}},
	}})
	assert.False(t, ok)
}

func TestTheLauncherEditsTheSameSettings(t *testing.T) {
	// One file, ~/.ade/settings.json, and the launcher is the page you reach
	// with no session open — so it should not be the page that cannot.
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, "PUT", "/api/settings", `{"scanDirs":[" ~/code "],"ignoredDirs":["node_modules"],"palette":"ayu"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = do(t, s, "GET", "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var view struct {
		ScanDirs []string `json:"scanDirs"`
		Palette  string   `json:"palette"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, []string{"~/code"}, view.ScanDirs)
	assert.Equal(t, "ayu", view.Palette)
}

func TestTheLauncherSettingsNeedTheToken(t *testing.T) {
	s := newTestLauncher(t)
	assert.Equal(t, http.StatusForbidden, do(t, s, "GET", "/api/settings?t=wrong", "").Code)
}

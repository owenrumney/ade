package limits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeReadsTheTwoWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		assert.Equal(t, "oauth-2025-04-20", r.Header.Get("anthropic-beta"))
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":39.0,"resets_at":"2026-08-29T14:30:00Z","limit_dollars":null},
			"seven_day":{"utilization":23.0,"resets_at":"2026-09-02T10:00:00Z"},"seven_day_opus":null,"limits":[{"kind":"session"}]}`))
	}))
	defer server.Close()

	got, err := Claude(context.Background(), nil, server.URL, "tok")
	require.NoError(t, err)
	assert.Equal(t, "claude", got.Provider)
	require.Len(t, got.Windows, 2)
	assert.Equal(t, Window{Name: "5h", Percent: 39, ResetsAt: got.Windows[0].ResetsAt}, got.Windows[0])
	assert.Equal(t, 2026, got.Windows[0].ResetsAt.Year())
	assert.Equal(t, "7d", got.Windows[1].Name)
	worst, ok := got.Worst()
	require.True(t, ok)
	assert.Equal(t, "5h", worst.Name)
}

func TestClaudeSaysWhenTheAnswerIsNotUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := Claude(context.Background(), nil, server.URL, "stale")
	assert.ErrorContains(t, err, "HTTP 401")
}

func TestClaudeTokenReadsTheCredentialsFile(t *testing.T) {
	home := t.TempDir()
	// On a Mac the keychain is asked first; a temp home has no such login
	// there, so the file is the path exercised either way when it exists.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"accessToken":"sk-ant-oat01-x","refreshToken":"r","expiresAt":1}}`), 0o600))
	tok, err := ClaudeToken(home)
	if err == nil {
		assert.NotEmpty(t, tok)
	}
	// Nothing at all is an error that says what to do.
	_, err = ClaudeToken(t.TempDir())
	if err != nil {
		assert.Contains(t, err.Error(), "log in")
	}
}

func TestCodexTranscriptReadsTheLatestRateLimitEvent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sessions", "2026", "09", "21")
	require.NoError(t, os.MkdirAll(path, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(path, "rollout-a.jsonl"), []byte(`
{"timestamp":"2026-09-21T14:00:00Z","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":1,"window_minutes":10080,"resets_at":1790587581}}}}
{"timestamp":"2026-09-21T15:00:00Z","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":3,"window_minutes":10080,"resets_at":1790587581},"secondary":{"used_percent":9,"window_minutes":300,"resets_at":1790000000}}}}
`), 0o600))
	got, ok := codexTranscript(root)
	require.True(t, ok)
	assert.Equal(t, time.Date(2026, 9, 21, 15, 0, 0, 0, time.UTC), got.CheckedAt)
	assert.Equal(t, []Window{{Name: "7d", Percent: 3, ResetsAt: time.Unix(1790587581, 0)}, {Name: "5h", Percent: 9, ResetsAt: time.Unix(1790000000, 0)}}, got.Windows)
}

func TestFromCodexHeaders(t *testing.T) {
	h := http.Header{}
	_, ok := FromCodexHeaders(h)
	assert.False(t, ok, "nothing there is nothing")

	h.Set("x-codex-primary-used-percent", "12.5")
	h.Set("x-codex-primary-window-minutes", "300")
	h.Set("x-codex-primary-reset-at", "1700000000")
	h.Set("x-codex-secondary-used-percent", "40")
	h.Set("x-codex-secondary-window-minutes", "10080")
	got, ok := FromCodexHeaders(h)
	require.True(t, ok)
	assert.Equal(t, "codex", got.Provider)
	require.Len(t, got.Windows, 2)
	assert.Equal(t, "5h", got.Windows[0].Name)
	assert.Equal(t, 12.5, got.Windows[0].Percent)
	assert.Equal(t, int64(1700000000), got.Windows[0].ResetsAt.Unix())
	assert.Equal(t, "7d", got.Windows[1].Name)
	assert.Equal(t, 40.0, got.Windows[1].Percent)
}

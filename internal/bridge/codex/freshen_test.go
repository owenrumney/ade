package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jwt(t *testing.T, exp time.Time) string {
	t.Helper()
	payload, err := json.Marshal(map[string]int64{"exp": exp.Unix()})
	require.NoError(t, err)
	return "h." + base64.RawURLEncoding.EncodeToString(payload) + ".s"
}

func tokenEndpoint(t *testing.T, calls *atomic.Int64, tokens map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		require.NoError(t, json.NewEncoder(w).Encode(tokens))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAnExpiredIdTokenIsRefreshedBeforeTheRun(t *testing.T) {
	// The 401 this exists for: `workspace routing discovery unauthorized`,
	// with an access token good for another week.
	home := t.TempDir()
	path := authPath(home)
	dead := jwt(t, time.Now().Add(-time.Hour))
	live := jwt(t, time.Now().Add(time.Hour))
	writeAuth(t, path, `{"tokens":{"id_token":"`+dead+`","access_token":"a",`+
		`"refresh_token":"r1","account_id":"acc"}}`)
	var calls atomic.Int64
	server := tokenEndpoint(t, &calls, map[string]string{
		"access_token": "fresh", "refresh_token": "r2", "id_token": live})

	require.NoError(t, freshenWith(context.Background(), path, Opts{TokenURL: server.URL}))

	got, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, live, got.IDToken)
	assert.Equal(t, "fresh", got.AccessToken)
	assert.Equal(t, "r2", got.RefreshToken)
	assert.EqualValues(t, 1, calls.Load())
}

func TestALiveIdTokenIsLeftAlone(t *testing.T) {
	// A refresh rotates the pair, so doing it per run is not free.
	home := t.TempDir()
	path := authPath(home)
	live := jwt(t, time.Now().Add(time.Hour))
	writeAuth(t, path, `{"tokens":{"id_token":"`+live+`","access_token":"a",`+
		`"refresh_token":"r1","account_id":"acc"}}`)
	var calls atomic.Int64
	server := tokenEndpoint(t, &calls, map[string]string{"access_token": "fresh"})

	require.NoError(t, freshenWith(context.Background(), path, Opts{TokenURL: server.URL}))

	assert.EqualValues(t, 0, calls.Load())
	got, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, "a", got.AccessToken, "untouched")
}

func TestAProviderThatStopsSendingAnIdTokenDoesNotRefreshEveryRun(t *testing.T) {
	// Without the floor an id_token that never arrives is a refresh per turn,
	// each one rotating the refresh token away from any other reader.
	home := t.TempDir()
	path := authPath(home)
	writeAuth(t, path, `{"tokens":{"access_token":"a","refresh_token":"r1","account_id":"acc"},`+
		`"last_refresh":"`+time.Now().UTC().Format(time.RFC3339)+`"}`)
	var calls atomic.Int64
	server := tokenEndpoint(t, &calls, map[string]string{"access_token": "fresh"})

	require.NoError(t, freshenWith(context.Background(), path, Opts{TokenURL: server.URL}))

	assert.EqualValues(t, 0, calls.Load(), "refreshed a moment ago")
}

func TestAFailedRefreshLeavesTheStampForTheCLI(t *testing.T) {
	// Bumping last_refresh is what stops the CLI refreshing for itself. On a
	// failure ade must not take that away as well.
	home := t.TempDir()
	path := authPath(home)
	dead := jwt(t, time.Now().Add(-time.Hour))
	writeAuth(t, path, `{"tokens":{"id_token":"`+dead+`","access_token":"a",`+
		`"refresh_token":"r1","account_id":"acc"},"last_refresh":"2026-01-01T00:00:00Z"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	err := freshenWith(context.Background(), path, Opts{TokenURL: server.URL})
	require.Error(t, err)

	var auth map[string]any
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.NoError(t, json.Unmarshal(data, &auth))
	assert.Equal(t, "2026-01-01T00:00:00Z", auth["last_refresh"])
}

func TestAnUnreadableIdTokenCountsAsExpired(t *testing.T) {
	for _, token := range []string{"", "not-a-jwt", "h.!!!.s", "h." +
		base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`)) + ".s"} {
		assert.True(t, staleIDToken(token), token)
	}
}

package codex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeAuth puts a login on disk in the CLI's shape.
func writeAuth(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func TestARefreshIsWrittenToTheCLIsOwnFile(t *testing.T) {
	// Both sides of one login share a refresh chain, and a refresh rotates it.
	// ade used to keep the result in a file of its own, which left `codex`
	// holding a token the provider had already retired.
	home := t.TempDir()
	writeAuth(t, authPath(home), `{"tokens":{"access_token":"stale","refresh_token":"r1","account_id":"acc"}}`)

	require.NoError(t, SaveCredentials(home, Credentials{AccessToken: "fresh", RefreshToken: "r2", AccountID: "acc"}))

	got, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, "fresh", got.AccessToken)
	assert.Equal(t, "r2", got.RefreshToken)
	assert.Equal(t, "acc", got.AccountID)
}

func TestAnApiKeyLoginSurvivesTheWrite(t *testing.T) {
	// auth.json carries more than ade models. Writing ade's shape over it
	// would drop the API key and the id_token with it.
	home := t.TempDir()
	writeAuth(t, authPath(home), `{"OPENAI_API_KEY":"sk-key","auth_mode":"chatgpt","tokens":`+
		`{"id_token":"idt","access_token":"a","refresh_token":"r1","account_id":"acc"},`+
		`"last_refresh":"2026-01-01T00:00:00Z"}`)

	require.NoError(t, SaveCredentials(home, Credentials{AccessToken: "fresh", RefreshToken: "r2", AccountID: "acc"}))

	var auth map[string]any
	data, err := os.ReadFile(authPath(home))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &auth))
	assert.Equal(t, "sk-key", auth["OPENAI_API_KEY"])
	assert.Equal(t, "chatgpt", auth["auth_mode"])
	tokens := auth["tokens"].(map[string]any)
	assert.Equal(t, "idt", tokens["id_token"], "not ade's to drop")
	assert.Equal(t, "fresh", tokens["access_token"])
	assert.Equal(t, "r2", tokens["refresh_token"])
}

func TestARefreshedIdTokenReachesTheCLIsFile(t *testing.T) {
	// The CLI reads the account off tokens.id_token, which lives an hour, and
	// refreshes on last_refresh's age. Bumping the one while dropping the other
	// left codex trusting a day-old id_token: 401 on its first call out.
	home := t.TempDir()
	writeAuth(t, authPath(home), `{"tokens":{"id_token":"expired","access_token":"a",`+
		`"refresh_token":"r1","account_id":"acc"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "fresh", "refresh_token": "r2", "id_token": "newer"})
	}))
	defer server.Close()

	opts := Opts{Home: home, TokenURL: server.URL}
	held, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, "expired", held.IDToken, "read, not only written")
	opts.Credentials = held
	fill(&opts)
	require.NoError(t, refreshInto(context.Background(), &opts))

	got, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, "newer", got.IDToken)
	assert.Equal(t, "fresh", got.AccessToken)
}

func TestTheSupersededCopyIsTakenAway(t *testing.T) {
	home := t.TempDir()
	writeAuth(t, authPath(home), `{"tokens":{"access_token":"a","refresh_token":"r1","account_id":"acc"}}`)
	writeAuth(t, legacyPath(home), `{"tokens":{"access_token":"old","refresh_token":"r0","account_id":"acc"}}`)

	require.NoError(t, SaveCredentials(home, Credentials{AccessToken: "fresh", RefreshToken: "r2", AccountID: "acc"}))

	assert.NoFileExists(t, legacyPath(home), "one store, and r0 is void anyway")
}

func TestTwoWritersDoNotTearTheLogin(t *testing.T) {
	// Two ade worktrees refresh at once. Whoever lands last must leave a whole
	// file behind, with its API key intact.
	home := t.TempDir()
	writeAuth(t, authPath(home), `{"OPENAI_API_KEY":"sk-key","tokens":{"access_token":"a","refresh_token":"r0","account_id":"acc"}}`)

	var wg sync.WaitGroup
	for _, token := range []string{"one", "two", "three", "four"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, SaveCredentials(home, Credentials{AccessToken: token, RefreshToken: "r" + token, AccountID: "acc"}))
		}()
	}
	wg.Wait()

	got, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Contains(t, []string{"one", "two", "three", "four"}, got.AccessToken)
	assert.Equal(t, "r"+got.AccessToken, got.RefreshToken, "the pair that landed must be one writer's")

	var auth map[string]any
	data, err := os.ReadFile(authPath(home))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &auth))
	assert.Equal(t, "sk-key", auth["OPENAI_API_KEY"])
}

func TestNoCliLoginIsAnError(t *testing.T) {
	_, err := LoadCredentials(t.TempDir())
	assert.ErrorContains(t, err, "codex login", "ade has no login of its own to fall back to")
}

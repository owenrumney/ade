package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/settingsapi"
)

func TestSettingsAreEditedFromThePageWithoutShowingTheKey(t *testing.T) {
	s := newTestServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	rec := do(t, s, "GET", "/api/settings", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view settingsapi.View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.NotEmpty(t, view.ScanDirs, "the defaults, written out")
	assert.False(t, view.LinearKeySet)

	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":[" ~/code ","~/code","","~/work"],"ignoredDirs":["node_modules"],"linearApiKey":"lin_api_EXAMPLE_NOT_A_REAL_KEY_1234","homeAssistantUrl":"https://assistant.example","homeAssistantDesk":"light.desk","homeAssistantToken":"ha_EXAMPLE_NOT_A_REAL_TOKEN_5678","gitHubPackagesToken":"ghp_EXAMPLE_NOT_A_REAL_TOKEN_9012","palette":"ghostty","colors":{"bg":"#112233"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, []string{"~/code", "~/work"}, view.ScanDirs, "trimmed, deduplicated")
	assert.Equal(t, []string{"node_modules"}, view.IgnoredDirs)
	assert.True(t, view.LinearKeySet)
	assert.Equal(t, "1234", view.LinearKeyEnd)
	assert.Equal(t, "https://assistant.example", view.HomeAssistantURL)
	assert.Equal(t, "light.desk", view.HomeAssistantDesk)
	assert.True(t, view.HomeAssistantTokenSet)
	assert.Equal(t, "5678", view.HomeAssistantTokenEnd)
	assert.True(t, view.GitHubPackagesTokenSet)
	assert.Equal(t, "9012", view.GitHubPackagesTokenEnd)
	assert.Equal(t, "ghostty", view.Palette)
	assert.Equal(t, map[string]string{"bg": "#112233"}, view.Colors)
	assert.NotContains(t, rec.Body.String(), "lin_api_EXAMPLE", "the key never comes back")
	assert.NotContains(t, rec.Body.String(), "ha_EXAMPLE", "the Home Assistant token never comes back")
	assert.NotContains(t, rec.Body.String(), "ghp_EXAMPLE", "the GitHub Packages token never comes back")

	saved, err := os.ReadFile(filepath.Join(home, ".ade", "settings.json"))
	require.NoError(t, err)
	assert.Contains(t, string(saved), `"linear_api_key": "lin_api_EXAMPLE_NOT_A_REAL_KEY_1234"`)
	assert.Contains(t, string(saved), `"token": "ha_EXAMPLE_NOT_A_REAL_TOKEN_5678"`)
	assert.Contains(t, string(saved), `"github_packages_token": "ghp_EXAMPLE_NOT_A_REAL_TOKEN_9012"`)
	assert.Contains(t, string(saved), `"palette": "ghostty"`)
	assert.Contains(t, string(saved), `"colors": {`)

	// Saving the directories again leaves the key alone; clearing is explicit.
	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"ignoredDirs":[]}`)
	require.Equal(t, http.StatusOK, rec.Code)
	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, "lin_api_EXAMPLE_NOT_A_REAL_KEY_1234", cfg.LinearAPIKey)
	assert.Equal(t, "ha_EXAMPLE_NOT_A_REAL_TOKEN_5678", cfg.HomeAssistant.Token)
	assert.Equal(t, "ghp_EXAMPLE_NOT_A_REAL_TOKEN_9012", cfg.GitHubPackagesToken)
	assert.Equal(t, "ghostty", cfg.Palette)
	assert.Equal(t, map[string]string{"bg": "#112233"}, cfg.Colors)

	// Forgetting it is its own request, so it does not depend on the rest of
	// the form being valid.
	rec = do(t, s, "DELETE", "/api/settings/linear-key", "")
	require.Equal(t, http.StatusOK, rec.Code)
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.LinearAPIKey)
	assert.Contains(t, rec.Body.String(), `"linearKeySet":false`, "the view comes back with it gone")

	rec = do(t, s, "DELETE", "/api/settings/home-assistant-token", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.HomeAssistant.Token)
	assert.Contains(t, rec.Body.String(), `"homeAssistantTokenSet":false`, "the view comes back with it gone")

	rec = do(t, s, "DELETE", "/api/settings/github-packages-token", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.GitHubPackagesToken)
	assert.Contains(t, rec.Body.String(), `"gitHubPackagesTokenSet":false`, "the view comes back with it gone")

	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"ignoredDirs":[],"palette":"neon"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "only named palettes may be saved")
	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"ignoredDirs":[],"colors":{"bg":"blue"}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "only named colour tokens and hex values may be saved")
	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"ignoredDirs":[],"homeAssistantUrl":"https://assistant.example","homeAssistantDesk":"switch.desk"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "only light entities may be saved")

	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":[],"ignoredDirs":[]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "no roots means no launcher")
}

func TestLinearIssuesAreOnTheStateWhenAKeyIsSet(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	asked := ""
	s.board.AskLinear(func(_ context.Context, key string) ([]linear.Issue, error) {
		asked = key
		if key == "bad" {
			return nil, errors.New("Linear refused the API key")
		}
		return []linear.Issue{{Identifier: "FIX-1", Title: "a thing", Started: true}}, nil
	})

	// No key: nothing on the state, nothing asked.
	s.board.RefreshTickets(context.Background())
	rec := do(t, s, "GET", "/api/state", "")
	assert.NotContains(t, rec.Body.String(), `"linear"`)
	assert.Empty(t, asked)

	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{"~/code"}, LinearAPIKey: "good"}))
	s.board.RefreshTickets(context.Background())
	assert.Equal(t, "good", asked)
	rec = do(t, s, "GET", "/api/state", "")
	var st struct {
		Linear *dash.Tickets `json:"linear"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
	require.NotNil(t, st.Linear)
	require.Len(t, st.Linear.Issues, 1)
	assert.Equal(t, "FIX-1", st.Linear.Issues[0].Identifier)
	assert.Empty(t, st.Linear.Error)

	// A refused key is said, not hidden, and the list is empty rather than stale.
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{"~/code"}, LinearAPIKey: "bad"}))
	rec = do(t, s, "POST", "/api/linear/refresh", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "refused the API key")
	assert.Contains(t, rec.Body.String(), `"issues":[]`)

	// Clearing the key takes the block off the state.
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{"~/code"}}))
	s.board.RefreshTickets(context.Background())
	assert.Nil(t, s.board.Tickets())
}

func TestTheGlobalSandboxIsSavedAndItsPathsChecked(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	body := fmt.Sprintf(`{"scanDirs":["~/code"],"sandboxPaths":[{"path":%q,"rw":true}],"sandboxEnv":{"CI":"1"}}`, dir)
	rec := do(t, s, "PUT", "/api/settings", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view settingsapi.View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, []sandbox.Path{{Path: dir, RW: true}}, view.SandboxPaths)
	assert.Equal(t, map[string]string{"CI": "1"}, view.SandboxEnv)

	// A missing directory is refused now rather than failing the container
	// after the agent has been asked to work.
	rec = do(t, s, "PUT", "/api/settings",
		`{"scanDirs":["~/code"],"sandboxPaths":[{"path":"/nowhere/at/all"}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"sandboxEnv":{"not a name":"x"}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// The refused saves left the good one in place.
	rec = do(t, s, "GET", "/api/settings", "")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, []sandbox.Path{{Path: dir, RW: true}}, view.SandboxPaths)
}

func TestASandboxSecretIsNeverSentToThePage(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	body := `{"scanDirs":["~/code"],"sandboxEnv":{"NPM_TOKEN":"npm_real","CI":"1"}}`
	rec := do(t, s, "PUT", "/api/settings", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view settingsapi.View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, sandbox.Redacted, view.SandboxEnv["NPM_TOKEN"])
	assert.Equal(t, "1", view.SandboxEnv["CI"])

	marker := fmt.Sprintf(`{"scanDirs":["~/code"],"sandboxEnv":{"NPM_TOKEN":%q,"CI":"2"}}`, sandbox.Redacted)
	rec = do(t, s, "PUT", "/api/settings", marker)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, "npm_real", cfg.Sandbox.Env["NPM_TOKEN"])
	assert.Equal(t, "2", cfg.Sandbox.Env["CI"])

	rec = do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"sandboxEnv":{"NPM_TOKEN":"npm_new"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cfg, err = settings.Load()
	require.NoError(t, err)
	assert.Equal(t, "npm_new", cfg.Sandbox.Env["NPM_TOKEN"])
}

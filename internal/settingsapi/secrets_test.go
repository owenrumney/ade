package settingsapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func secretMux(api *API) *http.ServeMux {
	mux := http.NewServeMux()
	api.Mount(mux, func(next http.HandlerFunc) http.HandlerFunc { return next })
	return mux
}

func send(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestASecretIsSetOnItsOwn(t *testing.T) {
	tests := []struct {
		name string
		path string
		read func(settings.Settings) string
		set  func(View) (bool, string)
	}{
		{"linear", "/api/settings/linear-key",
			func(cfg settings.Settings) string { return cfg.LinearAPIKey },
			func(v View) (bool, string) { return v.LinearKeySet, v.LinearKeyEnd }},
		{"home assistant", "/api/settings/home-assistant-token",
			func(cfg settings.Settings) string { return cfg.HomeAssistant.Token },
			func(v View) (bool, string) { return v.HomeAssistantTokenSet, v.HomeAssistantTokenEnd }},
		{"github packages", "/api/settings/github-packages-token",
			func(cfg settings.Settings) string { return cfg.GitHubPackagesToken },
			func(v View) (bool, string) { return v.GitHubPackagesTokenSet, v.GitHubPackagesTokenEnd }},
		{"dashboard", "/api/settings/dashboard-token",
			func(cfg settings.Settings) string { return cfg.Dashboard.Token },
			func(v View) (bool, string) { return v.DashboardTokenSet, v.DashboardTokenEnd }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			api := &API{}
			mux := secretMux(api)
			require.Equal(t, http.StatusOK, put(t, api, `{"scanDirs":["~/code"],"instructions":"keep me"}`).Code)

			rec := send(t, mux, http.MethodPut, tt.path, `{"value":"  secret-a1b2  "}`)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.NotContains(t, rec.Body.String(), "secret-a1b2", "the view never carries a secret back")

			var view View
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
			isSet, end := tt.set(view)
			assert.True(t, isSet)
			assert.Equal(t, "a1b2", end)

			cfg, err := settings.Load()
			require.NoError(t, err)
			assert.Equal(t, "secret-a1b2", tt.read(cfg))
			assert.Equal(t, "keep me", cfg.Instructions, "setting one secret leaves the rest alone")

			// A cancelled input box must not wipe a working key.
			for _, body := range []string{`{"value":""}`, `{"value":"   "}`, `{}`} {
				assert.Equal(t, http.StatusBadRequest, send(t, mux, http.MethodPut, tt.path, body).Code, body)
			}
			cfg, err = settings.Load()
			require.NoError(t, err)
			assert.Equal(t, "secret-a1b2", tt.read(cfg))

			require.Equal(t, http.StatusOK, send(t, mux, http.MethodDelete, tt.path, "").Code)
			cfg, err = settings.Load()
			require.NoError(t, err)
			assert.Empty(t, tt.read(cfg))
		})
	}
}

func TestSettingTheLinearKeyRefreshesTheTickets(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	refreshed := make(chan struct{}, 2)
	mux := secretMux(&API{Tickets: func() { refreshed <- struct{}{} }})

	require.Equal(t, http.StatusBadRequest, send(t, mux, http.MethodPut, "/api/settings/linear-key", `{}`).Code)
	require.Equal(t, http.StatusOK, send(t, mux, http.MethodPut, "/api/settings/linear-key", `{"value":"lin_api_x"}`).Code)

	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("the tickets were never refreshed")
	}
	assert.Empty(t, refreshed, "the refused value refreshed nothing")
}

func TestOnlyANamedSettingCanBeWrittenOnItsOwn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mux := secretMux(&API{})

	for _, key := range []string{"instructions", "hosts", "LinearAPIKey", "linear_api_key"} {
		path := "/api/settings/" + key
		assert.Equal(t, http.StatusNotFound, send(t, mux, http.MethodPut, path, `{"value":"x"}`).Code, key)
		assert.Equal(t, http.StatusNotFound, send(t, mux, http.MethodDelete, path, "").Code, key)
	}
}

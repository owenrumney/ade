package settingsapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestHostsAreCleanedAndChecked(t *testing.T) {
	// A name that saves has to be a name that connects. Checking here rather
	// than at dial time means the complaint arrives while you are looking at
	// the field that caused it.
	tests := []struct {
		name string
		in   []settings.Host
		want []settings.Host
		err  string
	}{
		{
			name: "trimmed, and blanks dropped",
			in:   []settings.Host{{Name: "  pc  ", Label: "  under the desk "}, {Name: "   "}},
			want: []settings.Host{{Name: "pc", Label: "under the desk"}},
		},
		{
			name: "the first of a repeat wins",
			in:   []settings.Host{{Name: "pc", Label: "one"}, {Name: "pc", Label: "two"}},
			want: []settings.Host{{Name: "pc", Label: "one"}},
		},
		{name: "nothing at all", in: nil, want: []settings.Host{}},
		{name: "an ssh option", in: []settings.Host{{Name: "-oProxyCommand=x"}}, err: "not a host name"},
		{name: "a shell metacharacter", in: []settings.Host{{Name: "pc;reboot"}}, err: "not a host name"},
		{name: "a path separator", in: []settings.Host{{Name: "../etc"}}, err: "not a host name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cleanHosts(tc.in)
			if tc.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTheStandingInstructionIsSavedAndReadBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	api := &API{}

	rec := put(t, api, `{"scanDirs":["~/code"],"instructions":"Never push to a shared branch."}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var view View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, "Never push to a shared branch.", view.Instructions)
	assert.Equal(t, settings.MaxInstructions, view.InstructionsMaxLength)

	// And it is what every run will be given.
	assert.Equal(t, "Never push to a shared branch.", settings.Instructions())
}

func TestOmittingTheInstructionLeavesItAlone(t *testing.T) {
	// The pointer is the point: a page that does not send the field must not
	// clear an instruction someone set elsewhere.
	t.Setenv("HOME", t.TempDir())
	api := &API{}
	require.Equal(t, http.StatusOK, put(t, api, `{"scanDirs":["~/code"],"instructions":"keep me"}`).Code)
	require.Equal(t, http.StatusOK, put(t, api, `{"scanDirs":["~/code"]}`).Code)

	assert.Equal(t, "keep me", settings.Instructions())
}

func TestAnInstructionOverTheCapIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	api := &API{}
	long := strings.Repeat("x", settings.MaxInstructions+1)

	rec := put(t, api, `{"scanDirs":["~/code"],"instructions":"`+long+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, settings.Instructions())
}

func TestTheSettingsPanelSavesDashboardDetailsWithoutClearingOtherSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	api := &API{}
	require.Equal(t, http.StatusOK, put(t, api, `{"scanDirs":["~/code"],"instructions":"keep me","palette":"ayu"}`).Code)

	req := httptest.NewRequest(http.MethodPatch, "/api/settings", strings.NewReader(`{"scanDirs":["~/work"],"ignoredDirs":["vendor"],"instructions":"short","dashboardUrl":"http://pi.local:8080/","dashboardHost":" desk ","dashboardClaude":" personal ","dashboardCodex":" work "}`))
	rec := httptest.NewRecorder()
	api.HandlePatchSettings(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var view View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, "http://pi.local:8080", view.DashboardURL)
	assert.Equal(t, "desk", view.DashboardHost)
	assert.Equal(t, "personal", view.DashboardClaude)
	assert.Equal(t, "work", view.DashboardCodex)
	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Equal(t, "ayu", cfg.Palette, "the panel must preserve settings it does not edit")
	assert.Equal(t, "short", cfg.Instructions)
}

func TestRolesCanBeManagedAndSelected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	api := &API{}

	rec := roleRequest(t, api, http.MethodPost, "/api/roles", `{"name":"oss-dev","prompt":"Keep it small."}`)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = roleRequest(t, api, http.MethodPost, "/api/roles", `{"name":"oss-dev","prompt":"Overwrite it."}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = roleRequest(t, api, http.MethodPost, "/api/roles", `{"name":"oss-dev","existingName":"oss-dev","prompt":"Updated."}`)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = roleRequest(t, api, http.MethodPut, "/api/roles/default", `{"name":"oss-dev"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var view struct {
		Default string `json:"default"`
		Roles   []struct {
			Name string `json:"name"`
		} `json:"roles"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, "oss-dev", view.Default)
	assert.Equal(t, "oss-dev", view.Roles[0].Name)

	rec = roleRequest(t, api, http.MethodDelete, "/api/roles/oss-dev", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Empty(t, view.Default)
	assert.Empty(t, view.Roles)
}

func roleRequest(t *testing.T, api *API, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if method == http.MethodDelete {
		req.SetPathValue("name", "oss-dev")
	}
	rec := httptest.NewRecorder()
	switch method {
	case http.MethodPost:
		api.HandleSaveRole(rec, req)
	case http.MethodPut:
		api.HandleDefaultRole(rec, req)
	default:
		api.HandleDeleteRole(rec, req)
	}
	return rec
}

// put saves settings and hands back the response.
func put(t *testing.T, api *API, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	api.HandleSaveSettings(rec, req)
	return rec
}

func TestAUserAgentIsInstalledThenEdited(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mux := http.NewServeMux()
	(&API{}).Mount(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	var view struct {
		Installed bool `json:"installed"`
		Agents    []struct {
			Name, Source, Content string
		} `json:"agents"`
	}
	source := func(name string) string {
		for _, one := range view.Agents {
			if one.Name == name {
				return one.Source
			}
		}
		return ""
	}

	rec := do(http.MethodGet, "/api/settings/agents", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.False(t, view.Installed)
	assert.Equal(t, "default", source("tester"))

	body := `{"content":"---\nname: tester\n---\nmine\n"}`
	assert.Equal(t, http.StatusBadRequest, do(http.MethodPut, "/api/settings/agents/tester", body).Code)

	rec = do(http.MethodPost, "/api/settings/agents/tester/install", "")
	require.Equal(t, http.StatusOK, rec.Code)
	rec = do(http.MethodPut, "/api/settings/agents/tester", body)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.True(t, view.Installed)
	assert.Equal(t, "user", source("tester"))
	assert.Equal(t, "default", source("security"))
}

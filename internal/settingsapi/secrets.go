package settingsapi

import (
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/settings"
)

// keyed is one setting that can be written or cleared on its own, by name.
type keyed struct {
	set func(cfg *settings.Settings, value string)
	// tickets is set where the value decides what the Issues view can read.
	tickets bool
}

// keyedSettings is every name PUT and DELETE /api/settings/{key} will take. A
// table, not reflection: a name that is not here cannot be written this way.
var keyedSettings = map[string]keyed{ //nolint:gochecknoglobals // a table, not state
	"linear-key": {
		set:     func(cfg *settings.Settings, value string) { cfg.LinearAPIKey = value },
		tickets: true,
	},
	"home-assistant-token": {
		set: func(cfg *settings.Settings, value string) { cfg.HomeAssistant.Token = value },
	},
	"github-packages-token": {
		set: func(cfg *settings.Settings, value string) { cfg.GitHubPackagesToken = value },
	},
	"dashboard-token": {
		set: func(cfg *settings.Settings, value string) { cfg.Dashboard.Token = value },
	},
}

// HandleSetKeyed replaces one setting and nothing else. An empty value is
// refused rather than read as a clear: DELETE is the clear.
func (a *API) HandleSetKeyed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value string `json:"value"`
	}
	if !decode(w, r, &req) {
		return
	}
	value := strings.TrimSpace(req.Value)
	if value == "" {
		writeError(w, http.StatusBadRequest, "send a value to set; DELETE forgets it")
		return
	}
	a.writeKeyed(w, r.PathValue("key"), value)
}

// HandleForgetKeyed clears one setting. Its own act: a button, not a box
// ticked and then saved behind the rest of a form being valid.
func (a *API) HandleForgetKeyed(w http.ResponseWriter, r *http.Request) {
	a.writeKeyed(w, r.PathValue("key"), "")
}

func (a *API) writeKeyed(w http.ResponseWriter, key, value string) {
	known, ok := keyedSettings[key]
	if !ok {
		writeError(w, http.StatusNotFound, "there is no setting called "+key+" to write on its own")
		return
	}
	cfg, err := forget(func(cfg *settings.Settings) { known.set(cfg, value) })
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if known.tickets {
		a.refreshTickets()
	}
	path, _ := settings.Path()
	writeJSON(w, http.StatusOK, ViewOf(cfg, path))
}

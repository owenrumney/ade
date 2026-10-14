// Package settingsapi serves ~/.ade/settings.json, which the launcher and
// every checkout's server edit alike — so the handlers belong to neither.
package settingsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/homeassistant"
	"github.com/owenrumney/ade/internal/models"
	"github.com/owenrumney/ade/internal/preview"
	"github.com/owenrumney/ade/internal/roles"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/settings"
)

// maxBody caps a settings request; the form is small and nothing here is bulk.
const maxBody = 1 << 20

// errRefused aborts an Update without saving. The status and the sentence the
// page gets are held beside it, since Update takes neither.
var errRefused = errors.New("settings refused")

// targetsWait is how long Home Assistant gets to list what it can notify.
const targetsWait = 10 * time.Second

// API is the settings routes. Changed is called after a save, so whichever
// server mounted this can re-read the lists a new rule or key changes.
type API struct {
	Changed func()
	Tickets func()
	// Synced is called after the model catalogue is fetched, so a launcher can
	// hand the new list to the workers it is connected to.
	Synced func()
}

// Mount adds the routes under the caller's own guard.
func (a *API) Mount(mux *http.ServeMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/settings", guard(a.HandleSettings))
	mux.HandleFunc("PUT /api/settings", guard(a.HandleSaveSettings))
	mux.HandleFunc("PATCH /api/settings", guard(a.HandlePatchSettings))
	mux.HandleFunc("PUT /api/settings/{key}", guard(a.HandleSetKeyed))
	mux.HandleFunc("DELETE /api/settings/{key}", guard(a.HandleForgetKeyed))
	mux.HandleFunc("GET /api/settings/notify-targets", guard(a.HandleNotifyTargets))
	mux.HandleFunc("GET /api/models", guard(a.HandleModels))
	mux.HandleFunc("POST /api/models/sync", guard(a.HandleSyncModels))
	mux.HandleFunc("GET /api/roles", guard(a.HandleRoles))
	mux.HandleFunc("POST /api/roles", guard(a.HandleSaveRole))
	mux.HandleFunc("PUT /api/roles/default", guard(a.HandleDefaultRole))
	mux.HandleFunc("DELETE /api/roles/{name}", guard(a.HandleDeleteRole))
	mux.HandleFunc("GET /api/settings/agents", guard(a.HandleUserAgents))
	mux.HandleFunc("PUT /api/settings/agents/{name}", guard(a.HandleSaveUserAgent))
	mux.HandleFunc("POST /api/settings/agents/{name}/install", guard(a.HandleInstallUserAgent))
}

func (a *API) changed() {
	if a.Changed != nil {
		go a.Changed()
	}
}

func (a *API) refreshTickets() {
	if a.Tickets != nil {
		go a.Tickets()
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		_ = err
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return false
	}
	return true
}

// View is the settings as the page edits them. A key stays on disk: the page
// sees only that one is set and its tail, and sends one to replace it.
type View struct {
	ScanDirs               []string `json:"scanDirs"`
	IgnoredDirs            []string `json:"ignoredDirs"`
	LinearKeySet           bool     `json:"linearKeySet"`
	LinearKeyEnd           string   `json:"linearKeyEnd,omitempty"`
	HomeAssistantURL       string   `json:"homeAssistantUrl"`
	HomeAssistantDesk      string   `json:"homeAssistantDesk"`
	HomeAssistantNotify    string   `json:"homeAssistantNotify"`
	HomeAssistantTokenSet  bool     `json:"homeAssistantTokenSet"`
	HomeAssistantTokenEnd  string   `json:"homeAssistantTokenEnd,omitempty"`
	GitHubPackagesTokenSet bool     `json:"gitHubPackagesTokenSet"`
	GitHubPackagesTokenEnd string   `json:"gitHubPackagesTokenEnd,omitempty"`
	DashboardURL           string   `json:"dashboardUrl"`
	DashboardHost          string   `json:"dashboardHost"`
	DashboardClaude        string   `json:"dashboardClaude"`
	DashboardCodex         string   `json:"dashboardCodex"`
	DashboardTokenSet      bool     `json:"dashboardTokenSet"`
	DashboardTokenEnd      string   `json:"dashboardTokenEnd,omitempty"`
	Path                   string   `json:"path"`

	// Instructions go in the system prompt of every session, and MaxLength is
	// the cap the page counts against.
	Instructions          string `json:"instructions"`
	InstructionsMaxLength int    `json:"instructionsMaxLength"`

	// What never to show on the dashboard. Sent whole, unlike the Linear key:
	// there is nothing secret in a list of names.
	IgnoreAuthors   []string `json:"ignoreAuthors"`
	IgnoreRepos     []string `json:"ignoreRepos"`
	IgnoreReasons   []string `json:"ignoreReasons"`
	IgnoreOlderDays int      `json:"ignoreOlderDays"`

	// Hosts are the machines ade will run on over ssh. Names, not credentials.
	Hosts []settings.Host `json:"hosts"`

	// SandboxPaths and SandboxEnv are what every container gets. Sent whole,
	// like the per-repository sandbox: a variable here is not a secret store.
	SandboxPaths []sandbox.Path    `json:"sandboxPaths"`
	SandboxEnv   map[string]string `json:"sandboxEnv"`

	// Where ade looks for a running app to capture after visual work, and the
	// switch that turns capture off for every repository.
	PreviewURL string `json:"previewUrl"`
	PreviewOff bool   `json:"previewOff"`

	// SharpenOff stops the pre-send questions about a vague prompt.
	SharpenOff bool `json:"sharpenOff"`
	// MergeQueuedOff stops prompts queued during one run being sent as one turn.
	MergeQueuedOff bool `json:"mergeQueuedOff"`

	Palette        string            `json:"palette"`
	DeepBackground bool              `json:"deepBackground"`
	Colors         map[string]string `json:"colors"`
}

// forget clears one field under settings.Update and hands back what was
// written, so clearing a key does not carry back the rest of a stale read.
func forget(clear func(*settings.Settings)) (settings.Settings, error) {
	var saved settings.Settings
	err := settings.Update(func(cfg *settings.Settings) error {
		clear(cfg)
		saved = *cfg
		return nil
	})
	return saved, err
}

func (a *API) HandleSettings(w http.ResponseWriter, _ *http.Request) {
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	path, _ := settings.Path()
	writeJSON(w, http.StatusOK, ViewOf(cfg, path))
}

// HandleSaveSettings writes the file. A key is replaced only when one is sent,
// so saving the directories does not touch it.
func (a *API) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScanDirs            []string `json:"scanDirs"`
		IgnoredDirs         []string `json:"ignoredDirs"`
		LinearAPIKey        string   `json:"linearApiKey"`
		HomeAssistantURL    string   `json:"homeAssistantUrl"`
		HomeAssistantDesk   string   `json:"homeAssistantDesk"`
		HomeAssistantNotify string   `json:"homeAssistantNotify"`
		HomeAssistantToken  string   `json:"homeAssistantToken"`
		GitHubPackagesToken string   `json:"gitHubPackagesToken"`

		IgnoreAuthors   []string `json:"ignoreAuthors"`
		IgnoreRepos     []string `json:"ignoreRepos"`
		IgnoreReasons   []string `json:"ignoreReasons"`
		IgnoreOlderDays int      `json:"ignoreOlderDays"`

		Hosts          []settings.Host    `json:"hosts"`
		Palette        *string            `json:"palette"`
		DeepBackground *bool              `json:"deepBackground"`
		Colors         *map[string]string `json:"colors"`

		SandboxPaths []sandbox.Path    `json:"sandboxPaths"`
		SandboxEnv   map[string]string `json:"sandboxEnv"`

		PreviewURL     string `json:"previewUrl"`
		PreviewOff     bool   `json:"previewOff"`
		SharpenOff     bool   `json:"sharpenOff"`
		MergeQueuedOff bool   `json:"mergeQueuedOff"`

		// A pointer, so a page that does not send the field leaves it alone
		// rather than clearing it.
		Instructions *string `json:"instructions"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Under settings.Update, so a host added from another handler between the
	// read and the write is not lost. A refusal aborts before the save.
	var cfg settings.Settings
	var code int
	var said string
	refuse := func(status int, msg string) error {
		code, said = status, msg
		return errRefused
	}
	err := settings.Update(func(into *settings.Settings) error {
		if req.Instructions != nil {
			if len(*req.Instructions) > settings.MaxInstructions {
				return refuse(http.StatusBadRequest, fmt.Sprintf(
					"instructions are %d bytes, over the %d byte cap", len(*req.Instructions), settings.MaxInstructions))
			}
			into.Instructions = strings.TrimSpace(*req.Instructions)
		}
		into.ScanDirs = cleanList(req.ScanDirs)
		into.IgnoredDirs = cleanList(req.IgnoredDirs)
		if len(into.ScanDirs) == 0 {
			return refuse(http.StatusBadRequest, "name at least one directory to look for repositories in")
		}
		if key := strings.TrimSpace(req.LinearAPIKey); key != "" {
			into.LinearAPIKey = key
		}
		home := into.HomeAssistant
		home.URL = req.HomeAssistantURL
		home.DeskEntity = req.HomeAssistantDesk
		home.NotifyTarget = req.HomeAssistantNotify
		if token := strings.TrimSpace(req.HomeAssistantToken); token != "" {
			home.Token = token
		}
		home, err := settings.CheckHomeAssistant(home)
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.HomeAssistant = home
		if token := strings.TrimSpace(req.GitHubPackagesToken); token != "" {
			into.GitHubPackagesToken = token
		}
		into.Ignore.Authors = cleanList(req.IgnoreAuthors)
		into.Ignore.Repos = cleanList(req.IgnoreRepos)
		into.Ignore.Reasons = cleanList(req.IgnoreReasons)
		if req.IgnoreOlderDays < 0 {
			return refuse(http.StatusBadRequest, "a number of days cannot be negative")
		}
		into.Ignore.OlderThanDays = req.IgnoreOlderDays

		hosts, err := cleanHosts(req.Hosts)
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.Hosts = hosts

		if req.DeepBackground != nil {
			into.DeepBackground = req.DeepBackground
		}
		if req.Palette != nil {
			palette, err := settings.CheckPalette(*req.Palette)
			if err != nil {
				return refuse(http.StatusBadRequest, err.Error())
			}
			into.Palette = palette
		}
		if req.Colors != nil {
			colors, err := settings.CheckColors(*req.Colors)
			if err != nil {
				return refuse(http.StatusBadRequest, err.Error())
			}
			into.Colors = colors
		}

		hostHome, err := os.UserHomeDir()
		if err != nil {
			return refuse(http.StatusInternalServerError, err.Error())
		}
		// The page was given a marker for every secret, not the value.
		env, err := sandbox.RestoreEnv(req.SandboxEnv, into.Sandbox.Env)
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		box, err := sandbox.CheckWorkspace(hostHome, sandbox.Workspace{Paths: req.SandboxPaths, Env: env})
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.Sandbox = box

		shot, err := preview.ValidURL(req.PreviewURL)
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.Preview = settings.Preview{URL: shot, Off: req.PreviewOff}
		into.SharpenOff = req.SharpenOff
		into.MergeQueuedOff = req.MergeQueuedOff

		cfg = *into
		return nil
	})
	if errors.Is(err, errRefused) {
		writeError(w, code, said)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A rule you just typed should take effect while you are still looking at
	// the dashboard, rather than on the next tick.
	a.changed()
	path, _ := settings.Path()
	writeJSON(w, http.StatusOK, ViewOf(cfg, path))
}

// HandlePatchSettings saves the webview's small form without clearing settings
// it does not know how to edit yet.
func (a *API) HandlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ScanDirs            []string `json:"scanDirs"`
		IgnoredDirs         []string `json:"ignoredDirs"`
		Instructions        *string  `json:"instructions"`
		HomeAssistantURL    string   `json:"homeAssistantUrl"`
		HomeAssistantDesk   string   `json:"homeAssistantDesk"`
		HomeAssistantNotify string   `json:"homeAssistantNotify"`
		PreviewURL          string   `json:"previewUrl"`
		PreviewOff          bool     `json:"previewOff"`
		SharpenOff          bool     `json:"sharpenOff"`
		MergeQueuedOff      bool     `json:"mergeQueuedOff"`
		DashboardURL        string   `json:"dashboardUrl"`
		DashboardHost       string   `json:"dashboardHost"`
		DashboardClaude     string   `json:"dashboardClaude"`
		DashboardCodex      string   `json:"dashboardCodex"`
	}
	if !decode(w, r, &req) {
		return
	}
	var cfg settings.Settings
	var code int
	var said string
	refuse := func(status int, msg string) error {
		code, said = status, msg
		return errRefused
	}
	err := settings.Update(func(into *settings.Settings) error {
		if req.Instructions != nil {
			if len(*req.Instructions) > settings.MaxInstructions {
				return refuse(http.StatusBadRequest, fmt.Sprintf(
					"instructions are %d bytes, over the %d byte cap", len(*req.Instructions), settings.MaxInstructions))
			}
			into.Instructions = strings.TrimSpace(*req.Instructions)
		}
		into.ScanDirs = cleanList(req.ScanDirs)
		if len(into.ScanDirs) == 0 {
			return refuse(http.StatusBadRequest, "name at least one directory to look for repositories in")
		}
		into.IgnoredDirs = cleanList(req.IgnoredDirs)
		home, err := settings.CheckHomeAssistant(settings.HomeAssistant{
			URL: req.HomeAssistantURL, DeskEntity: req.HomeAssistantDesk, NotifyTarget: req.HomeAssistantNotify,
			Token: into.HomeAssistant.Token,
		})
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.HomeAssistant = home
		preview, err := preview.ValidURL(req.PreviewURL)
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.Preview = settings.Preview{URL: preview, Off: req.PreviewOff}
		into.SharpenOff = req.SharpenOff
		into.MergeQueuedOff = req.MergeQueuedOff
		dashboard, err := settings.CheckDashboard(settings.Dashboard{
			URL: req.DashboardURL, Host: req.DashboardHost, Claude: req.DashboardClaude,
			Codex: req.DashboardCodex, Token: into.Dashboard.Token,
		})
		if err != nil {
			return refuse(http.StatusBadRequest, err.Error())
		}
		into.Dashboard = dashboard
		cfg = *into
		return nil
	})
	if errors.Is(err, errRefused) {
		writeError(w, code, said)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.changed()
	path, _ := settings.Path()
	writeJSON(w, http.StatusOK, ViewOf(cfg, path))
}

func ViewOf(cfg settings.Settings, path string) View {
	view := View{
		ScanDirs: cfg.ScanDirs, IgnoredDirs: cfg.IgnoredDirs, Path: path,
		IgnoreAuthors: cfg.Ignore.Authors, IgnoreRepos: cfg.Ignore.Repos,
		IgnoreReasons: cfg.Ignore.Reasons, IgnoreOlderDays: cfg.Ignore.OlderThanDays,
		Hosts: cfg.Hosts, Palette: settings.PaletteID(cfg.Palette),
		DeepBackground: settings.DeepFor(cfg.Palette, cfg.DeepBackground),
		Colors:         settings.SafeColors(cfg.Colors),
		SandboxPaths:   cfg.Sandbox.Paths, SandboxEnv: sandbox.RedactEnv(cfg.Sandbox.Env),
		PreviewURL: cfg.Preview.URL, PreviewOff: cfg.Preview.Off,
		SharpenOff:     cfg.SharpenOff,
		MergeQueuedOff: cfg.MergeQueuedOff,
		Instructions:   cfg.Instructions, InstructionsMaxLength: settings.MaxInstructions,
	}
	if view.SandboxPaths == nil {
		view.SandboxPaths = []sandbox.Path{}
	}
	if view.SandboxEnv == nil {
		view.SandboxEnv = map[string]string{}
	}
	if view.Hosts == nil {
		view.Hosts = []settings.Host{}
	}
	if view.Colors == nil {
		view.Colors = map[string]string{}
	}
	// Empty lists rather than nulls: the page joins them into a textarea, and
	// a null there reads as the string "null".
	for _, list := range []*[]string{&view.ScanDirs, &view.IgnoredDirs, &view.IgnoreAuthors, &view.IgnoreRepos, &view.IgnoreReasons} {
		if *list == nil {
			*list = []string{}
		}
	}
	if key := strings.TrimSpace(cfg.LinearAPIKey); key != "" {
		view.LinearKeySet = true
		if len(key) > 4 {
			view.LinearKeyEnd = key[len(key)-4:]
		}
	}
	view.HomeAssistantURL = cfg.HomeAssistant.URL
	view.HomeAssistantDesk = cfg.HomeAssistant.DeskEntity
	view.HomeAssistantNotify = cfg.HomeAssistant.NotifyTarget
	if token := strings.TrimSpace(cfg.HomeAssistant.Token); token != "" {
		view.HomeAssistantTokenSet = true
		if len(token) > 4 {
			view.HomeAssistantTokenEnd = token[len(token)-4:]
		}
	}
	if token := strings.TrimSpace(cfg.GitHubPackagesToken); token != "" {
		view.GitHubPackagesTokenSet = true
		if len(token) > 4 {
			view.GitHubPackagesTokenEnd = token[len(token)-4:]
		}
	}
	view.DashboardURL = cfg.Dashboard.URL
	view.DashboardHost = cfg.Dashboard.Host
	view.DashboardClaude = cfg.Dashboard.Claude
	view.DashboardCodex = cfg.Dashboard.Codex
	if token := strings.TrimSpace(cfg.Dashboard.Token); token != "" {
		view.DashboardTokenSet = true
		if len(token) > 4 {
			view.DashboardTokenEnd = token[len(token)-4:]
		}
	}
	return view
}

// cleanHosts checks each name is one ssh will take, so a host that saves is
// one that can be connected to.
func cleanHosts(in []settings.Host) ([]settings.Host, error) {
	out := []settings.Host{}
	seen := map[string]bool{}
	for _, host := range in {
		name := strings.TrimSpace(host.Name)
		if name == "" {
			continue
		}
		if err := settings.CheckHostName(name); err != nil {
			return nil, err
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, settings.Host{Name: name, Label: strings.TrimSpace(host.Label)})
	}
	return out, nil
}

// cleanList trims the entries and drops the blank ones and the repeats.
func cleanList(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// HandleNotifyTargets asks Home Assistant what it can notify, so the settings
// page offers a list rather than asking you to spell a service name.
func (a *API) HandleNotifyTargets(w http.ResponseWriter, r *http.Request) {
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	client := homeassistant.Client{URL: cfg.HomeAssistant.URL, Token: cfg.HomeAssistant.Token}
	// Its own deadline: a host that accepts and never answers would hold this
	// open for as long as the browser waits.
	ctx, cancel := context.WithTimeout(r.Context(), targetsWait)
	defer cancel()
	targets, err := client.Targets(ctx)
	if err != nil {
		// The url and token are typed on this page, so a failure here is
		// ordinary and belongs beside the field rather than in a log.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

// HandleModels is what the agent pickers offer. Never an error: a machine that
// has never synced has the built-in list, which is the answer either way.
func (a *API) HandleModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, models.Load())
}

// HandleSyncModels fetches the catalogue from cost.dev and keeps it.
func (a *API) HandleSyncModels(w http.ResponseWriter, r *http.Request) {
	got, err := models.Sync(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if a.Synced != nil {
		go a.Synced()
	}
	writeJSON(w, http.StatusOK, got)
}

func (a *API) HandleRoles(w http.ResponseWriter, _ *http.Request) {
	view, err := roles.ViewOf()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) HandleSaveRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		roles.Role
		ExistingName string `json:"existingName"`
	}
	if !decode(w, r, &req) {
		return
	}
	var err error
	if strings.TrimSpace(req.ExistingName) == "" {
		err = roles.Create(req.Name, req.Prompt, req.Model)
	} else if req.ExistingName == req.Name {
		err = roles.Save(req.Name, req.Prompt, req.Model)
	} else {
		err = fmt.Errorf("a role cannot be renamed; create a new one instead")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.changed()
	a.HandleRoles(w, r)
}

func (a *API) HandleDefaultRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := roles.SetDefault(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.changed()
	a.HandleRoles(w, r)
}

func (a *API) HandleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if err := roles.Delete(r.PathValue("name")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	a.changed()
	a.HandleRoles(w, r)
}

// HandleUserAgents is ~/.claude/agents on the machine this server runs on:
// the directory its sandboxes mount, so an edit here is the one agents read.
func (a *API) HandleUserAgents(w http.ResponseWriter, _ *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	view, err := agents.User(home)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *API) HandleSaveUserAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &req) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := agents.SaveUser(home, r.PathValue("name"), req.Content); err != nil {
		writeError(w, agentStatus(err), err.Error())
		return
	}
	a.changed()
	a.HandleUserAgents(w, r)
}

func (a *API) HandleInstallUserAgent(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := agents.InstallOne(home, r.PathValue("name")); err != nil {
		writeError(w, agentStatus(err), err.Error())
		return
	}
	a.changed()
	a.HandleUserAgents(w, r)
}

// agentStatus is 500 for a disk that failed the write, 400 for content or a name refused.
func agentStatus(err error) int {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && !errors.Is(err, fs.ErrNotExist) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

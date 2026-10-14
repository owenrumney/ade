// Package settings holds the little machine-wide configuration ade has: where
// to look for checkouts, and which ones to leave out of the list.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/owenrumney/ade/internal/homeassistant"
	"github.com/owenrumney/ade/internal/preview"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/statedir"
)

// Settings is ~/.ade/settings.json.
type Settings struct {
	// ScanDirs are searched for git checkouts, shallowly: a directory holding
	// your repositories, not your whole home.
	ScanDirs []string `json:"scan_dirs"`

	// IgnoredDirs are left out. An entry is either a path, which drops that
	// directory and anything under it, or a bare name, which drops every
	// directory called that.
	IgnoredDirs []string `json:"ignored_dirs"`

	// Instructions are told to every agent ade starts, in every repository:
	// ade's own, appended to the system prompt rather than read from a file.
	Instructions string `json:"instructions,omitempty"`

	// DefaultRole is the machine-wide prompt used for new main chats.
	DefaultRole string            `json:"default_role,omitempty"`
	RoleModels  map[string]string `json:"role_models,omitempty"`

	// LinearAPIKey is a read-only personal API key, for the issues assigned
	// to you in the top bar. Empty means no Linear.
	LinearAPIKey string `json:"linear_api_key,omitempty"`

	// WikiRemote names the shared wiki's origin. Empty leaves wiki support off.
	WikiRemote string `json:"wiki_remote,omitempty"`
	// WikiDistill publishes pages distilled from this machine's transcripts.
	// Off by default: the text goes to a Git remote.
	WikiDistill bool `json:"wiki_distill,omitempty"`

	// WikiVerbatim lets a repository publish full prompts in its commit records.
	// Here as well as in its config, which a sandboxed agent can write.
	WikiVerbatim bool `json:"wiki_verbatim,omitempty"`

	HomeAssistant HomeAssistant `json:"home_assistant,omitempty"`

	// Dashboard pushes this machine's usage to a kiosk screen. Off unless
	// both a URL and a token are set.
	Dashboard Dashboard `json:"dashboard,omitempty"`

	// GitHubPackagesToken authenticates private npm packages in sandboxes.
	GitHubPackagesToken string `json:"github_packages_token,omitempty"`

	// Sandbox is what every container gets, whatever the repository. A
	// repository's own sandbox.json is laid over this.
	Sandbox sandbox.Workspace `json:"sandbox,omitempty"`

	// Preview is screenshot capture for every repository. A repository's own
	// preview.json overrides the URL.
	Preview Preview `json:"preview,omitempty"`

	// SharpenOff stops the questions a cheap model asks about a vague prompt
	// before it is sent.
	SharpenOff bool `json:"sharpen_off,omitempty"`

	// MergeQueuedOff stops prompts queued during one run going as one turn.
	MergeQueuedOff bool `json:"merge_queued_off,omitempty"`

	// Ignore is what never to show on the dashboard. Rules, as against the
	// per-item ignoring the page does: this is the standing "not from them,
	// not from there, not that kind".
	Ignore Ignore `json:"ignore,omitempty"`

	// Agentbus is the team bus: questions from other people's agents, and
	// review requests, offered to this ade.
	Agentbus Agentbus `json:"agentbus,omitempty"`

	// Hosts are other machines to run ade on, over ssh. A bookmark list and
	// nothing more: ade never interprets a name, it hands it to ssh.
	Hosts []Host `json:"hosts,omitempty"`

	Palette string `json:"palette,omitempty"`
	// DeepBackground takes a palette's deeper background where it has one. A
	// pointer: unset means migrate from the palette, not "off".
	DeepBackground *bool             `json:"deep_background,omitempty"`
	Colors         map[string]string `json:"colors,omitempty"`
}

// Agentbus is this machine's standing on the team bus. It is on wherever a
// server can be resolved; Off is the one switch that stops ade joining.
type Agentbus struct {
	Off bool `json:"off,omitempty"`

	// URL overrides the tailnet lookup, for a bus somewhere else.
	URL string `json:"url,omitempty"`

	// Repos limits the review requests offered here, as globs on owner/repo.
	// Empty takes them all.
	Repos []string `json:"repos,omitempty"`

	// NoAsk and NoReview drop one kind of request without dropping the other.
	NoAsk    bool `json:"no_ask,omitempty"`
	NoReview bool `json:"no_review,omitempty"`
}

// Preview is where ade looks for a running app to capture, and the one switch
// that turns capture off everywhere.
type Preview struct {
	URL string `json:"url,omitempty"`
	Off bool   `json:"off,omitempty"`
}

// PreviewURL is the URL to capture, given a repository's own. Empty means
// nothing to capture.
func (p Preview) PreviewURL(repo string) string {
	if p.Off {
		return ""
	}
	if repo != "" {
		return repo
	}
	if p.URL != "" {
		return p.URL
	}
	return preview.DefaultURL
}

type HomeAssistant struct {
	URL        string `json:"url,omitempty"`
	DeskEntity string `json:"desk_entity,omitempty"`
	Token      string `json:"token,omitempty"`

	// NotifyTarget is a notify service, "notify.mobile_app_…", for a waiting
	// session. Empty sends nothing: a light is a desk signal, this follows you.
	NotifyTarget string `json:"notify_target,omitempty"`
}

// Dashboard is a kiosk screen this machine reports into. Several machines
// report into one, which is what Host and the account labels are for.
type Dashboard struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
	Host  string `json:"host,omitempty"`

	// Claude and Codex name the subscription each login belongs to: the same
	// label on two machines means one account, and empty reports nothing.
	Claude string `json:"claude_account,omitempty"`
	Codex  string `json:"codex_account,omitempty"`
}

// On is whether there is anywhere to report to. Either half missing is off.
func (d Dashboard) On() bool { return d.URL != "" && d.Token != "" }

// Machine is the hostname this machine reports under.
func (d Dashboard) Machine() string {
	if d.Host != "" {
		return d.Host
	}
	name, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return name
}

// CheckDashboard validates what the settings UI sends. An empty URL and token
// is the ordinary off state and not an error.
func CheckDashboard(config Dashboard) (Dashboard, error) {
	config.URL = strings.TrimRight(strings.TrimSpace(config.URL), "/")
	config.Host = strings.TrimSpace(config.Host)
	config.Claude = strings.TrimSpace(config.Claude)
	config.Codex = strings.TrimSpace(config.Codex)
	if config.URL == "" {
		return config, nil
	}
	parsed, err := url.Parse(config.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return config, fmt.Errorf("dashboard url %q should look like http://pi.local:8080", config.URL)
	}
	return config, nil
}

const DefaultPalette = "ghostty"

// legacyDeep was a palette before the deeper background became a checkbox.
// Still read: it is what every saved settings file says.
const legacyDeep = "ghostty-deep"

func CheckHomeAssistant(config HomeAssistant) (HomeAssistant, error) {
	config.URL = strings.TrimSpace(config.URL)
	config.DeskEntity = strings.TrimSpace(config.DeskEntity)
	config.NotifyTarget = strings.TrimSpace(config.NotifyTarget)
	if config.URL == "" && config.DeskEntity == "" && config.NotifyTarget == "" {
		return config, nil
	}
	url, err := homeassistant.ValidURL(config.URL)
	if err != nil {
		return config, err
	}
	// Either is enough on its own: the light is a desk signal and the notify
	// target follows you, and wanting one without the other is ordinary.
	if config.DeskEntity == "" && config.NotifyTarget == "" {
		return config, fmt.Errorf("name a light to flash or a notify service to send to")
	}
	if config.DeskEntity != "" && !strings.HasPrefix(config.DeskEntity, "light.") {
		return config, fmt.Errorf("%q is not a light entity", config.DeskEntity)
	}
	if config.NotifyTarget != "" && !strings.HasPrefix(config.NotifyTarget, "notify.") {
		return config, fmt.Errorf("%q is not a notify service", config.NotifyTarget)
	}
	config.URL = url
	return config, nil
}

// palettes are the ids ade offers. Nothing it serves draws them any more; the
// editor's theme does, and this is what a setting is still allowed to say.
var palettes = []string{DefaultPalette, "ayu", "monokai", "adwaita-dark"}

func PaletteID(id string) string {
	id = strings.TrimSpace(id)
	if id == legacyDeep {
		return DefaultPalette
	}
	for _, known := range palettes {
		if id == known {
			return id
		}
	}
	return DefaultPalette
}

// DeepFor migrates an unset flag: ghostty-deep meant deep, and so does a file
// written before the setting existed. That is what everyone sees today.
func DeepFor(id string, flag *bool) bool {
	if flag != nil {
		return *flag
	}
	return strings.TrimSpace(id) == legacyDeep || strings.TrimSpace(id) == ""
}

func CheckPalette(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == legacyDeep {
		return PaletteID(id), nil
	}
	if PaletteID(id) == id {
		return id, nil
	}
	return "", fmt.Errorf("%q is not a palette ade knows", id)
}

var colorName = map[string]bool{
	"bg": true, "panel": true, "text": true, "muted": true,
	"accent": true, "red": true, "green": true, "amber": true,
}

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func CheckColors(colors map[string]string) (map[string]string, error) {
	for name, value := range colors {
		if !colorName[name] || !hexColor.MatchString(strings.TrimSpace(value)) {
			return nil, fmt.Errorf("%q is not a colour ade can use", value)
		}
	}
	return SafeColors(colors), nil
}

func SafeColors(colors map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range colors {
		value = strings.TrimSpace(value)
		if colorName[name] && hexColor.MatchString(value) {
			out[name] = value
		}
	}
	return out
}

// Host is one machine on the list. Name is an ssh destination — an alias out
// of ~/.ssh/config, or user@host — and Label is what to call it on the page.
type Host struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
}

// hostName is what may be a host name, which also becomes a socket filename.
//
//nolint:gochecknoglobals // a compiled pattern, not state
var hostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)

// CheckHostName rejects a name ssh would misread. It lives here, not beside
// the ssh code, so saving a host and connecting to one agree on what is
// allowed rather than failing later with a different complaint.
func CheckHostName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a host needs a name")
	}
	// A leading dash would be read by ssh as an option rather than a
	// destination, and the rest keeps the name usable as a filename.
	if !hostName.MatchString(name) {
		return fmt.Errorf("%q is not a host name ade will pass to ssh; use an ssh alias or user@host", name)
	}
	return nil
}

// Dir is where ade keeps its own state.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find your home directory: %w", err)
	}
	// A test that reaches the real one edits the user's own sessions. A panic,
	// not an error: callers drop the error and quietly take another branch.
	if testing.Testing() && !underTemp(home) {
		panic("set HOME to a t.TempDir() before touching ade's state directory; got " + home)
	}
	return statedir.Home(home), nil
}

// ScratchDir holds the throwaway checkouts a scratch session works in. It is
// under ade's own state so a scratch belongs to no project until it is moved.
func ScratchDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "scratch"), nil
}

// IsScratch reports whether a checkout is one of those. Both sides are
// resolved: git reports /private/var where the home said /var.
func IsScratch(path string) bool {
	root, err := ScratchDir()
	if err != nil || strings.TrimSpace(path) == "" {
		return false
	}
	return strings.HasPrefix(resolve(path), resolve(root)+string(filepath.Separator))
}

// underTemp is whether a path is inside the temporary directory, counted in
// path elements: /tmpfoo is not under /tmp.
// GOTMPDIR as well as TMPDIR: since Go 1.24 t.TempDir() roots at GOTMPDIR,
// which ade sets away from /var/folders to keep Gatekeeper off test binaries.
func underTemp(path string) bool {
	real := resolve(path)
	for _, root := range []string{os.TempDir(), os.Getenv("GOTMPDIR")} {
		if root == "" {
			continue
		}
		tmp := resolve(root)
		if real == tmp || strings.HasPrefix(real, tmp+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// resolve follows symlinks; a path not there yet stands as it was written.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// Path is the settings file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// RecordPath is where the launcher records itself. It serves no checkout, so it
// sits beside the settings rather than in a git dir.
func RecordPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server.json"), nil
}

// ScanOverride is the scan root the environment names, if it does. A worker is
// told where to look and must not inherit what a previous run happened to save.
func ScanOverride() string {
	return strings.TrimSpace(os.Getenv("ADE_SCAN_DIR"))
}

// Defaults is what a machine with no settings file behaves as.
func Defaults() Settings {
	if root := ScanOverride(); root != "" {
		return Settings{ScanDirs: []string{root}, IgnoredDirs: []string{}}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Settings{IgnoredDirs: []string{}}
	}
	return Settings{
		ScanDirs:    []string{filepath.Join(home, "code")},
		IgnoredDirs: []string{},
	}
}

// Load reads the settings, writing the defaults out first if there are none.
// The file is how you change what the launcher offers, so it exists whether or
// not you have edited it.
func Load() (Settings, error) {
	path, err := Path()
	if err != nil {
		return Defaults(), err
	}

	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if errors.Is(err, os.ErrNotExist) {
		defaults := Defaults()
		if err := Save(defaults); err != nil {
			// Not fatal: the defaults are what would have been written anyway.
			return defaults, nil //nolint:nilerr // a missing file is not a failure to load
		}
		return defaults, nil
	}
	if err != nil {
		slog.Warn("could not read the settings; using defaults", "path", path, "err", err)
		return Defaults(), fmt.Errorf("read %s: %w", path, err)
	}

	loaded, err := parse(data)
	if err != nil {
		slog.Warn("the settings file does not parse; using defaults", "path", path, "err", err)
		return Defaults(), fmt.Errorf("parse %s: %w", path, err)
	}
	return scanOverridden(loaded), nil
}

// scanOverridden applies ADE_SCAN_DIR over what was loaded: the file a worker
// writes on its first run would otherwise decide where later runs look.
func scanOverridden(s Settings) Settings {
	if root := ScanOverride(); root != "" {
		s.ScanDirs = []string{root}
	}
	return s
}

// parse reads the file's contents. Leaving scan_dirs out is how you say "the
// usual place, but skip these", so it falls back rather than emptying the list.
func parse(data []byte) (Settings, error) {
	var loaded Settings
	if err := json.Unmarshal(data, &loaded); err != nil {
		return Defaults(), err
	}
	if len(loaded.ScanDirs) == 0 {
		loaded.ScanDirs = Defaults().ScanDirs
	}
	return loaded, nil
}

// Save writes the settings out.
func Save(s Settings) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		slog.Warn("could not write the settings", "path", path, "err", err)
		return fmt.Errorf("write %s: %w", path, err)
	}
	slog.Info("settings saved", "path", path)
	return nil
}

// Roots are the scan directories as absolute paths, with ~ expanded, and the
// scratch directory: a scratch checkout is found again like any other.
func (s Settings) Roots() []string {
	out := make([]string, 0, len(s.ScanDirs)+1)
	for _, dir := range s.ScanDirs {
		if expanded := Expand(dir); expanded != "" {
			out = append(out, expanded)
		}
	}
	if scratch, err := ScratchDir(); err == nil {
		out = append(out, scratch)
	}
	return out
}

// Host finds one of the listed machines by name. Connecting goes through this
// rather than a name off the request, so the page can only reach a host that
// was offered.
func (s Settings) Host(name string) (Host, bool) {
	name = strings.TrimSpace(name)
	for _, host := range s.Hosts {
		if strings.TrimSpace(host.Name) == name && name != "" {
			return host, true
		}
	}
	return Host{}, false
}

// Ignored reports whether a directory is one to leave out.
func (s Settings) Ignored(dir string) bool {
	base := filepath.Base(dir)
	for _, entry := range s.IgnoredDirs {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// A bare name drops every directory called that, wherever it is.
		if !strings.ContainsRune(entry, filepath.Separator) && !strings.HasPrefix(entry, "~") {
			if entry == base {
				return true
			}
			continue
		}
		full := Expand(entry)
		if full == "" {
			continue
		}
		if dir == full || strings.HasPrefix(dir, full+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Expand resolves a leading ~ and makes the path absolute.
func Expand(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		path = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return abs
}

// MaxInstructions caps the standing instruction. It is in the system prompt of
// every session, so it is a screenful rather than a document.
const MaxInstructions = 4 << 10

// Instructions is the standing instruction for every agent, or empty.
// Unreadable settings are no instruction rather than a refused run.
func Instructions() string {
	cfg, err := Load()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Instructions)
}

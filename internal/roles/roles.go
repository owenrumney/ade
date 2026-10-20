// Package roles stores the machine-wide prompts a main ade chat can use.
package roles

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/statedir"
)

const MaxPrompt = 16 << 10

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Role is one named prompt. Prompt is Markdown rather than a settings value so
// it stays readable and editable outside ade.
type Role struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
}

// View is the machine's roles and the one used by new main chats.
type View struct {
	Roles   []Role `json:"roles"`
	Default string `json:"default"`
}

//go:embed defaults
var shipped embed.FS

// defaults is what shipped, or what a test put there instead.
var defaults fs.FS = shipped

// Dir is where machine-wide role files live.
func Dir() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "roles"), nil
}

// Load reads every valid role file. A machine without roles has an empty list.
func Load() ([]Role, error) {
	// Settings failing to parse costs the per-role model, not the roles: the
	// .md files on disk are readable and are what this lists.
	cfg, _ := settings.Load()
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Role{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := make([]Role, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if !nameRE.MatchString(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name())) // #nosec G304 -- a file under ade's own directory
		if err != nil {
			return nil, fmt.Errorf("read role %s: %w", name, err)
		}
		if len(data) > MaxPrompt {
			continue
		}
		prompt := strings.TrimSpace(string(data))
		if prompt == "" {
			continue
		}
		out = append(out, Role{Name: name, Prompt: prompt, Model: cfg.RoleModels[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Current is the selected role, if it still exists. A stale selection quietly
// becomes no role so a deleted file never stops a chat starting.
func Current() string {
	cfg, err := settings.Load()
	if err != nil || cfg.DefaultRole == "" {
		return ""
	}
	list, err := Load()
	if err != nil {
		return ""
	}
	for _, role := range list {
		if role.Name == cfg.DefaultRole {
			return role.Prompt
		}
	}
	return ""
}

// Prompt is one role's text by name. A name no file matches is no role, so a
// session asked for a deleted one still starts.
func Prompt(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	list, err := Load()
	if err != nil {
		return ""
	}
	for _, role := range list {
		if role.Name == name {
			return role.Prompt
		}
	}
	return ""
}

// ViewOf reports what the settings page can manage.
func ViewOf() (View, error) {
	list, err := Load()
	if err != nil {
		return View{}, err
	}
	cfg, err := settings.Load()
	if err != nil {
		return View{}, err
	}
	found := false
	for _, role := range list {
		if role.Name == cfg.DefaultRole {
			found = true
			break
		}
	}
	selected := cfg.DefaultRole
	if !found {
		selected = ""
	}
	return View{Roles: list, Default: selected}, nil
}

// Create writes a new role without replacing an existing prompt.
func Create(name, prompt string, model ...string) error {
	return write(name, prompt, model, false)
}

// Save replaces an existing role after it has been explicitly selected.
func Save(name, prompt string, model ...string) error { return write(name, prompt, model, true) }

// write takes the model as the caller gave it: no argument leaves whatever is
// configured alone, and an empty one clears it.
func write(name, prompt string, model []string, replace bool) error {
	name = strings.TrimSpace(name)
	prompt = strings.TrimSpace(prompt)
	if !nameRE.MatchString(name) {
		return fmt.Errorf("role name %q: letters, digits, - and _ only", name)
	}
	if prompt == "" {
		return errors.New("a role needs instructions")
	}
	if len(prompt) > MaxPrompt {
		return fmt.Errorf("role is %d bytes, over the %d byte cap", len(prompt), MaxPrompt)
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	path := filepath.Join(dir, name+".md")
	_, statErr := os.Stat(path)
	if statErr == nil && !replace {
		return fmt.Errorf("a role named %q already exists", name)
	}
	if errors.Is(statErr, os.ErrNotExist) && replace {
		return fmt.Errorf("no role named %q", name)
	}
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, statErr)
	}
	if err := os.WriteFile(path, []byte(prompt+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if len(model) == 0 {
		return nil
	}
	return settings.Update(func(cfg *settings.Settings) error {
		if cfg.RoleModels == nil {
			cfg.RoleModels = map[string]string{}
		}
		cfg.RoleModels[name] = strings.TrimSpace(model[0])
		return nil
	})
}

// SetDefault makes name the role used by future main chats. An empty name
// clears the selection.
func SetDefault(name string) error {
	name = strings.TrimSpace(name)
	if name != "" {
		list, err := Load()
		if err != nil {
			return err
		}
		found := false
		for _, role := range list {
			found = found || role.Name == name
		}
		if !found {
			return fmt.Errorf("no role named %q", name)
		}
	}
	return settings.Update(func(cfg *settings.Settings) error {
		cfg.DefaultRole = name
		return nil
	})
}

// Delete removes a role and clears it as the default when needed.
func Delete(name string) error {
	name = strings.TrimSpace(name)
	if !nameRE.MatchString(name) {
		return fmt.Errorf("role name %q: letters, digits, - and _ only", name)
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name+".md")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete %s: %w", path, err)
	}
	return settings.Update(func(cfg *settings.Settings) error {
		if cfg.DefaultRole == name {
			cfg.DefaultRole = ""
		}
		delete(cfg.RoleModels, name)
		return nil
	})
}

// Installed is what Install did with one shipped role.
type Installed struct {
	Name  string
	Path  string
	State string
}

// Install copies ade's shipped roles into ~/.ade/roles without replacing local
// edits unless force is set.
func Install(home string, force bool) ([]Installed, error) {
	if home == "" {
		return nil, errors.New("no home directory to install into")
	}
	dir := filepath.Join(statedir.Home(home), "roles")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	entries, err := fs.ReadDir(defaults, "defaults")
	if err != nil {
		return nil, err
	}
	out := make([]Installed, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		data, err := fs.ReadFile(defaults, "defaults/"+entry.Name())
		if err != nil {
			return out, err
		}
		path := filepath.Join(dir, entry.Name())
		state := "written"
		if _, err := os.Stat(path); err == nil {
			if !force {
				out = append(out, Installed{Name: strings.TrimSuffix(entry.Name(), ".md"), Path: path, State: "kept"})
				continue
			}
			state = "replaced"
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return out, fmt.Errorf("write %s: %w", path, err)
		}
		out = append(out, Installed{Name: strings.TrimSuffix(entry.Name(), ".md"), Path: path, State: state})
	}
	return out, nil
}

// Defaults reads the roles ade ships for installation.
func Defaults() ([]Role, error) {
	entries, err := fs.ReadDir(defaults, "defaults")
	if err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		data, err := fs.ReadFile(defaults, "defaults/"+entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Role{Name: strings.TrimSuffix(entry.Name(), ".md"), Prompt: strings.TrimSpace(string(data))})
	}
	return out, nil
}

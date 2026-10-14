package skills

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Found is one skill on disk: the name it goes by and the directory with its
// SKILL.md.
type Found struct {
	Name string
	Dir  string
}

// All is every skill Claude Code loads for this user: their own under
// ~/.claude/skills, then each enabled plugin's. The first of a name wins.
func All(home string) ([]Found, error) {
	roots := []string{Dir(home)}
	plugins, err := enabledPlugins(home)
	if err != nil {
		return nil, err
	}
	roots = append(roots, plugins...)
	var out []Found
	seen := map[string]bool{}
	for _, root := range roots {
		found, err := under(root)
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			if seen[f.Name] || !nameRE.MatchString(f.Name) {
				continue
			}
			seen[f.Name] = true
			out = append(out, f)
		}
	}
	return out, nil
}

// Link fills dir with a link per skill, replacing the links it made before.
// A link into ~/.claude is ade's to replace; anything else in dir is kept.
func Link(home, dir string, found []Found) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	mine := filepath.Join(home, ".claude") + string(filepath.Separator)
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		path := filepath.Join(dir, e.Name())
		target, err := os.Readlink(path)
		if err != nil || !strings.HasPrefix(target, mine) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	for _, f := range found {
		path := filepath.Join(dir, f.Name)
		if _, err := os.Lstat(path); err == nil {
			continue
		}
		if err := os.Symlink(f.Dir, path); err != nil {
			return err
		}
	}
	return nil
}

// CodexDir is where Codex reads a user's skills.
func CodexDir(home string) string { return filepath.Join(home, ".codex", "skills") }

// ShareWithCodex links every skill into ~/.codex/skills, so Codex has what
// Claude has. Without a ~/.codex there is nothing to share them with.
func ShareWithCodex(home string) error {
	if info, err := os.Stat(filepath.Join(home, ".codex")); err != nil || !info.IsDir() {
		return nil
	}
	found, err := All(home)
	if err != nil {
		return err
	}
	return Link(home, CodexDir(home), found)
}

// enabledPlugins is the install directory of each plugin the user has on, in
// name order, read from the two files Claude Code keeps them in.
func enabledPlugins(home string) ([]string, error) {
	var settings struct {
		Enabled map[string]bool `json:"enabledPlugins"`
	}
	if err := readJSON(filepath.Join(home, ".claude", "settings.json"), &settings); err != nil {
		return nil, err
	}
	var index struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := readJSON(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), &index); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(index.Plugins))
	for name := range index.Plugins {
		if settings.Enabled[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var dirs []string
	for _, name := range names {
		for _, install := range index.Plugins[name] {
			if install.InstallPath != "" {
				dirs = append(dirs, install.InstallPath)
			}
		}
	}
	return dirs, nil
}

func readJSON(path string, into any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- under the user's own ~/.claude
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

// under is every SKILL.md below root, named after its directory. A root that
// is not there holds nothing.
func under(root string) ([]Found, error) {
	var out []Found
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return nil
			}
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			dir := filepath.Dir(path)
			out = append(out, Found{Name: filepath.Base(dir), Dir: dir})
		}
		return nil
	})
	return out, err
}

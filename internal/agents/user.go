package agents

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxFile caps one definition written from the settings page.
const maxFile = 64 << 10

// Source is where an entry in the user's view comes from.
const (
	SourceUser    = "user"
	SourceDefault = "default"
)

// Entry is one agent as the settings page edits it: the whole file, so what
// is saved is what Claude Code reads, frontmatter and all.
type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Source      string `json:"source"`
	Content     string `json:"content"`
	// Error is why a user file does not load as an agent, so it can be fixed here.
	Error string `json:"error,omitempty"`
}

// UserView is ~/.claude/agents and the shipped agents not yet in it.
type UserView struct {
	Dir       string  `json:"dir"`
	Installed bool    `json:"installed"`
	Agents    []Entry `json:"agents"`
}

// User reads home's agents directory, with each default that is missing from
// it listed as a default: shown, but not editable until installed.
func User(home string) (UserView, error) {
	if home == "" {
		return UserView{}, errors.New("no home directory")
	}
	dir := filepath.Join(home, ".claude", "agents")
	view := UserView{Dir: dir}
	have := map[string]bool{}
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil:
		view.Installed = true
	case !errors.Is(err, fs.ErrNotExist):
		return view, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		one := Entry{Name: agentName(entry.Name()), Path: path, Source: SourceUser}
		have[one.Name] = true
		data, err := readRegular(path)
		if err != nil {
			one.Error = err.Error()
			view.Agents = append(view.Agents, one)
			continue
		}
		one.Content = string(data)
		if agent, err := Parse(path, ScopeUser, data); err != nil {
			one.Error = err.Error()
		} else {
			one.Description = agent.Description
			have[agent.Name] = true
		}
		view.Agents = append(view.Agents, one)
	}
	shipped, err := fs.ReadDir(defaults, "defaults")
	if err != nil {
		return view, err
	}
	for _, entry := range shipped {
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		if have[agentName(entry.Name())] {
			continue
		}
		data, err := fs.ReadFile(defaults, "defaults/"+entry.Name())
		if err != nil {
			return view, err
		}
		agent, err := Parse(entry.Name(), ScopeUser, data)
		if err != nil {
			return view, err
		}
		view.Agents = append(view.Agents, Entry{
			Name: agentName(entry.Name()), Description: agent.Description,
			Source: SourceDefault, Content: string(data),
		})
	}
	sort.Slice(view.Agents, func(i, j int) bool { return view.Agents[i].Name < view.Agents[j].Name })
	return view, nil
}

// readRegular refuses a pipe or device, which os.ReadFile would block on forever.
func readRegular(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > maxFile {
		return nil, fmt.Errorf("over the %d byte cap", maxFile)
	}
	return os.ReadFile(path) // #nosec G304 -- a file under ~/.claude/agents
}

// SaveUser rewrites an agent already in home's agents directory. A default is
// installed first; a new file is not made here.
func SaveUser(home, name, content string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("agent name %q: letters, digits, - and _ only", name)
	}
	if len(content) > maxFile {
		return fmt.Errorf("an agent definition is at most %d bytes", maxFile)
	}
	path := filepath.Join(home, ".claude", "agents", name+".md")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s is not installed: %w", name, err)
	}
	agent, err := Parse(path, ScopeUser, []byte(content))
	if err != nil {
		return err
	}
	if agent.Name != name {
		return fmt.Errorf("the frontmatter names %q, but this file is %s: Claude Code would load it as %q", agent.Name, name, agent.Name)
	}
	// Written in place, not renamed over, so a symlink into dotfiles stays one.
	return os.WriteFile(path, []byte(content), 0o600) // #nosec G306 G304 -- under ~/.claude/agents
}

// InstallOne writes the default called name into home's agents directory,
// leaving a file already there alone.
func InstallOne(home, name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("agent name %q: letters, digits, - and _ only", name)
	}
	data, err := fs.ReadFile(defaults, "defaults/"+name+".md")
	if err != nil {
		return fmt.Errorf("no default agent called %s", name)
	}
	dir := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, name+".md"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

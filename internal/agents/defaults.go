package agents

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// defaults are the agents ade ships with: a reviewer, a second opinion on
// another model, and three narrower critics. Installed to ~/.claude/agents
// by `ade agents install`, where Claude Code and ade both read them.
//
//go:embed defaults
var shipped embed.FS

// defaults is what shipped, or what a test put there instead.
var defaults fs.FS = shipped

// UseDefaults stands prompts in for the shipped ones, which live in
// ade-private, and hands back the way to put them back.
func UseDefaults(files fs.FS) func() {
	was := defaults
	defaults = files
	return func() { defaults = was }
}

// Installed is what Install did with one file.
type Installed struct {
	Name string
	Path string
	// State is "written", "kept" (yours was left alone) or "replaced".
	State string
}

// Install writes the default agents into home's .claude/agents. A file that
// is already there is yours — edited, perhaps — and is kept unless force.
func Install(home string, force bool) ([]Installed, error) {
	if home == "" {
		return nil, errors.New("no home directory to install into")
	}
	dir := filepath.Join(home, ".claude", "agents")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	entries, err := fs.ReadDir(defaults, "defaults")
	if err != nil {
		return nil, err
	}
	var out []Installed
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
				out = append(out, Installed{Name: agentName(entry.Name()), Path: path, State: "kept"})
				continue
			}
			state = "replaced"
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return out, fmt.Errorf("write %s: %w", path, err)
		}
		out = append(out, Installed{Name: agentName(entry.Name()), Path: path, State: state})
	}
	return out, nil
}

// Defaults is the shipped set, parsed, for listing and for tests.
func Defaults() ([]Agent, error) {
	entries, err := fs.ReadDir(defaults, "defaults")
	if err != nil {
		return nil, err
	}
	var out []Agent
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		data, err := fs.ReadFile(defaults, "defaults/"+entry.Name())
		if err != nil {
			return nil, err
		}
		agent, err := Parse(entry.Name(), ScopeUser, data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		out = append(out, agent)
	}
	return out, nil
}

func agentName(file string) string {
	return file[:len(file)-len(filepath.Ext(file))]
}

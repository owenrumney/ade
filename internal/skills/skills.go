// Package skills reads ~/.claude/skills/<name>/SKILL.md, so a session can be
// started under one. The file format is the agents one.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/owenrumney/ade/internal/agents"
)

// MaxPrompt caps what is appended to a system prompt.
const MaxPrompt = 32 << 10

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Skill is one definition: what it is called, and the prompt it carries.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
	Path        string `json:"path"`
}

// Dir is where a user's skills live.
func Dir(home string) string { return filepath.Join(home, ".claude", "skills") }

// Load reads one skill by name.
func Load(home, name string) (Skill, error) {
	if !nameRE.MatchString(name) {
		return Skill{}, fmt.Errorf("skill name %q: letters, digits, - and _ only", name)
	}
	path := filepath.Join(Dir(home), name, "SKILL.md")
	info, err := os.Stat(path)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", name, err)
	}
	if info.Size() > MaxPrompt {
		return Skill{}, fmt.Errorf("%s is %d bytes; a skill used as a system prompt is capped at %d", path, info.Size(), MaxPrompt)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- a named skill under the user's own ~/.claude
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", name, err)
	}
	parsed, err := agents.Parse(path, agents.ScopeUser, data)
	if err != nil {
		return Skill{}, fmt.Errorf("read skill %s: %w", name, err)
	}
	if parsed.Prompt == "" {
		return Skill{}, fmt.Errorf("%s has no body below its frontmatter", path)
	}
	return Skill{Name: name, Description: parsed.Description, Prompt: parsed.Prompt, Path: path}, nil
}

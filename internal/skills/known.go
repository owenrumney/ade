package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// Known is a slash command that names a skill or a custom command: a turn for
// the model, not something Claude Code runs on its own.
func Known(home, root, command string) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	name := strings.TrimPrefix(words[0], "/")
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:]
	}
	if !nameRE.MatchString(name) {
		return false
	}
	files := []string{
		filepath.Join(home, ".claude", "commands", name+".md"),
		filepath.Join(root, ".claude", "commands", name+".md"),
		filepath.Join(root, ".claude", "skills", name, "SKILL.md"),
	}
	for _, file := range files {
		if _, err := os.Stat(file); err == nil {
			return true
		}
	}
	found, err := All(home)
	if err != nil {
		return false
	}
	for _, f := range found {
		if f.Name == name {
			return true
		}
	}
	return false
}

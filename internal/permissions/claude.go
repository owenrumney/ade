package permissions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
)

// claudeSettings is where Claude Code keeps the user's own rules.
var claudeSettings = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// claudeDenies is the user's own Claude deny list, less what ade cannot match.
func claudeDenies() []Rule {
	path, err := claudeSettings()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own Claude settings
	if err != nil {
		return nil
	}
	var file struct {
		Permissions struct {
			Deny []Rule `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil
	}
	return slices.DeleteFunc(file.Permissions.Deny, func(rule Rule) bool { return !rule.Valid() })
}

// upgrade brings an older file up to date. Each step runs once, so a rule
// taken out afterwards stays out.
func (s *Store) upgrade() {
	if s.file.Version < 2 {
		s.adopt()
	}
	held := slices.ContainsFunc(s.file.Deny, func(r Rule) bool { return r.Matches(bashTool, "git commit") })
	if s.file.Version < 3 && !held {
		s.file.Deny = append(s.file.Deny, commitDeny)
	}
}

// adopt copies the user's Claude denies in.
func (s *Store) adopt() {
	for _, rule := range claudeDenies() {
		if !slices.Contains(s.file.Deny, rule) {
			s.file.Deny = append(s.file.Deny, rule)
		}
	}
}

package web

import (
	"net/http"
	"path/filepath"

	"github.com/owenrumney/ade/internal/permissions"
	"github.com/owenrumney/ade/internal/settings"
)

// RulesAPI is where the deny list is read and written.
const (
	RulesAPI = "/api/permissions"
)

// rulesView is the deny list and the file it is kept in.
type rulesView struct {
	Deny []permissions.Rule `json:"deny"`
	File string             `json:"file"`
}

// handleRules is the rule set as the page wants it.
func (s *Server) handleRules(w http.ResponseWriter, _ *http.Request) {
	rules := s.agent.rulesStore()
	if rules == nil {
		writeError(w, http.StatusConflict, "this session has no rule file")
		return
	}
	file, _ := rules.Rules()
	writeJSON(w, http.StatusOK, rulesView{
		Deny: orEmpty(file.Deny),
		File: rulesFile(),
	})
}

// handleRuleChange adds or withdraws one deny rule.
func (s *Server) handleRuleChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule   string `json:"rule"`
		Forget bool   `json:"forget"`
	}
	if !decode(w, r, &req) {
		return
	}
	rules := s.agent.rulesStore()
	if rules == nil {
		writeError(w, http.StatusConflict, "this session has no rule file")
		return
	}
	var err error
	if req.Forget {
		err = rules.Forget(permissions.Rule(req.Rule))
	} else {
		err = rules.Remember(permissions.Rule(req.Rule), true, false)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.handleRules(w, r)
}

// rulesFile is where the rules are kept, so the page can say so and you can
// open it yourself. Empty when ade cannot name its own directory.
func rulesFile() string {
	dir, err := settings.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "permissions.json")
}

// orEmpty keeps a nil slice out of the JSON: the page reads a list, and null
// is not one.
func orEmpty(rules []permissions.Rule) []permissions.Rule {
	if rules == nil {
		return []permissions.Rule{}
	}
	return rules
}

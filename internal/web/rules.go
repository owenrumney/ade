package web

import (
	_ "embed"
	"net/http"
	"path/filepath"

	"github.com/owenrumney/ade/internal/permissions"
	"github.com/owenrumney/ade/internal/settings"
)

// RulesPath is the page that shows what ade allows, and RulesAPI is what it
// reads and writes.
const (
	RulesPath = "/permissions"
	RulesAPI  = "/api/permissions"
)

//go:embed rules.html
var rulesPage []byte

// rulesView is the whole of what the page draws.
type rulesView struct {
	Allow   []permissions.Rule `json:"allow"`
	Deny    []permissions.Rule `json:"deny"`
	Session []permissions.Rule `json:"session"`
	Free    bool               `json:"free"`
	File    string             `json:"file"`
}

// handleRulesPage serves the page itself, which is plain and self-contained so
// it needs nothing from the app's bundle.
func (s *Server) handleRulesPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Embedded, so it changes with the binary. A webview that cached it keeps
	// drawing the old page long after the server it talks to was replaced.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(rulesPage)
}

// handleRules is the rule set as the page wants it.
func (s *Server) handleRules(w http.ResponseWriter, _ *http.Request) {
	rules := s.agent.rulesStore()
	if rules == nil {
		writeError(w, http.StatusConflict, "this session has no rule file")
		return
	}
	file, session := rules.Rules()
	writeJSON(w, http.StatusOK, rulesView{
		Allow:   orEmpty(file.Allow),
		Deny:    orEmpty(file.Deny),
		Session: orEmpty(session),
		Free:    rules.IsFree(),
		File:    rulesFile(),
	})
}

// handleRuleChange adds or withdraws one rule.
func (s *Server) handleRuleChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rule   string `json:"rule"`
		Deny   bool   `json:"deny"`
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
		err = rules.Remember(permissions.Rule(req.Rule), req.Deny, false)
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

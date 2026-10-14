package bridge

import (
	"encoding/json"

	"github.com/owenrumney/ade/internal/permissions"
)

// held is one ask waiting on the user. Two things arrive this way and settle
// differently: a control frame over stdin, and a PreToolUse hook over HTTP.
type held interface {
	tool() string
	rule() string
	target() string
	allowed() []string
	settle(verdict) error
}

// verdict is how an ask was settled. Answers go back as the tool's own input.
type verdict struct {
	allow    bool
	remember string
	reason   string
	answers  map[string]string
}

// controlAsk is an ask that came over the stdio permission-prompt tool.
type controlAsk struct {
	claude  *Claude
	request canUseToolRequest
}

// hookAsk came over the hook: its HTTP handler is blocked on reply. decision
// is what the rules made of it, so the card asks only about what they left.
type hookAsk struct {
	request  HookRequest
	reply    chan verdict
	decision permissions.Decision
}

func (a controlAsk) tool() string      { return a.request.Request.Tool }
func (a controlAsk) rule() string      { return a.request.rule() }
func (a controlAsk) target() string    { return "" }
func (a controlAsk) allowed() []string { return nil }

func (a controlAsk) settle(v verdict) error {
	if !v.allow {
		return a.claude.writeControlResponse(a.request, map[string]any{"behavior": "deny", "message": v.reason})
	}
	decision := map[string]any{"behavior": "allow"}
	if len(v.answers) > 0 {
		decision["updatedInput"] = a.answered(v.answers)
	}
	// Claude Code wrote the rule and named the file; handing it back is the
	// whole of remembering. ade only redirects where it is kept.
	if v.remember != "" && len(a.request.Request.Suggestions) > 0 {
		decision["updatedPermissions"] = a.request.scoped(v.remember)
	}
	return a.claude.writeControlResponse(a.request, decision)
}

func (a controlAsk) answered(answers map[string]string) map[string]any {
	var input map[string]any
	if err := json.Unmarshal(a.request.Request.Input, &input); err != nil || input == nil {
		input = map[string]any{}
	}
	input["answers"] = answers
	return input
}

func (a hookAsk) tool() string { return a.request.ToolName }

// target is the segment no rule covered: the part the card is really about.
func (a hookAsk) target() string {
	if len(a.decision.Unmatched) > 0 && a.request.ToolName == "Bash" {
		return a.decision.Unmatched[0]
	}
	return a.request.Target()
}

// rule is what remembering this would write: ade's own proposal, not the command.
func (a hookAsk) rule() string {
	if target := a.target(); target != "" {
		return string(permissions.Generalise(a.request.ToolName, target))
	}
	return a.request.ToolName
}

// allowed is the rules already covering the rest of the command.
func (a hookAsk) allowed() []string {
	out := make([]string, 0, len(a.decision.Rules))
	for _, rule := range a.decision.Rules {
		out = append(out, string(rule))
	}
	return out
}

func (a hookAsk) settle(v verdict) error {
	select {
	case a.reply <- v:
	default:
		// The handler has gone — its own timeout, or the turn was stopped.
		// Nothing to tell, and nothing broken.
	}
	return nil
}

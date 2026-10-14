package bridge

import (
	"context"
	"strings"

	"github.com/owenrumney/ade/internal/permissions"
)

// HookRequest is the PreToolUse body Claude Code POSTs. It arrives whatever
// the mode, where canUseTool is never consulted under bypassPermissions.
type HookRequest struct {
	HookEventName  string         `json:"hook_event_name"`
	ToolName       string         `json:"tool_name"`
	ToolInput      map[string]any `json:"tool_input"`
	ToolUseID      string         `json:"tool_use_id"`
	SessionID      string         `json:"session_id"`
	PermissionMode string         `json:"permission_mode"`
}

// HookResponse is what Claude Code reads back. Deny stops the call running.
type HookResponse struct {
	Output HookOutput `json:"hookSpecificOutput"`

	// Rules decided the call when no card was drawn. Never sent: it is for the
	// chat to say which rule answered.
	Rules []string `json:"-"`
}

// HookOutput is the decision. UpdatedInput is how a question is answered.
type HookOutput struct {
	HookEventName            string         `json:"hookEventName"`
	PermissionDecision       string         `json:"permissionDecision"`
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
}

// Rules is the rule set a run is gated on, as the bridge needs it.
type Rules interface {
	Decide(tool, target string) permissions.Decision
}

// Gate is an agent whose tool calls are decided over a PreToolUse hook.
type Gate interface {
	Hook(context.Context, HookRequest) HookResponse
}

// preToolUse is the hook event this gate serves.
const preToolUse = "PreToolUse"

// HookTokenVar is the environment variable the hook's bearer token is read
// from. The token never appears in the settings JSON, only its name does.
const HookTokenVar = "ADE_HOOK_TOKEN"

// Hook decides one tool call. It answers at once when a rule covers the call,
// and otherwise blocks — the card is drawn, and this is the request it answers.
func (c *Claude) Hook(ctx context.Context, request HookRequest) HookResponse {
	// Nowhere to draw a card, so refusing is the only answer that is not a hole.
	if request.ToolName == "" || request.ToolUseID == "" {
		return denied("ade could not tell what this call was, so it was not allowed")
	}
	var decision permissions.Decision
	if c.opts.Rules != nil && request.ToolName != askTool {
		var response HookResponse
		decision = c.opts.Rules.Decide(request.ToolName, request.Target())
		switch decision.Verdict {
		case permissions.Allow:
			response = allowed("allowed by your rule " + rulesText(decision.Rules))
		case permissions.Deny:
			response = denied("denied by your rule " + rulesText(decision.Rules) +
				". Do not try it again this turn; say what you could not do.")
		}
		if response.Output.PermissionDecision != "" {
			for _, rule := range decision.Rules {
				response.Rules = append(response.Rules, string(rule))
			}
			return response
		}
	}
	ask := hookAsk{request: request, reply: make(chan verdict, 1), decision: decision}
	c.hold(request.ToolUseID, ask)
	select {
	case v := <-ask.reply:
		if !v.allow {
			return denied(v.reason)
		}
		if len(v.answers) > 0 {
			return answered(request, v.answers)
		}
		return allowed("you allowed this")
	case <-ctx.Done():
		_, _ = c.take(request.ToolUseID)
		return denied("ade stopped waiting for an answer to this")
	case <-c.gone:
		_, _ = c.take(request.ToolUseID)
		return denied("the session ended before this was answered")
	}
}

// Target is the one string a rule is written against.
func (r HookRequest) Target() string {
	for _, key := range []string{"command", "file_path", "path", "url", "pattern", "notebook_path"} {
		if value, ok := r.ToolInput[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func allowed(reason string) HookResponse {
	return HookResponse{Output: HookOutput{
		HookEventName:            preToolUse,
		PermissionDecision:       "allow",
		PermissionDecisionReason: reason,
	}}
}

func denied(reason string) HookResponse {
	if strings.TrimSpace(reason) == "" {
		reason = refused
	}
	return HookResponse{Output: HookOutput{
		HookEventName:            preToolUse,
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}}
}

// answered folds the choices into the input, which Claude Code takes as asked.
func answered(request HookRequest, answers map[string]string) HookResponse {
	input := map[string]any{}
	for k, v := range request.ToolInput {
		input[k] = v
	}
	input["answers"] = answers
	response := allowed("you answered this")
	response.Output.UpdatedInput = input
	return response
}

// rulesText names the rules a decision rested on, for the model and the chat.
func rulesText(rules []permissions.Rule) string {
	if len(rules) == 0 {
		return "(none)"
	}
	names := make([]string, 0, len(rules))
	for _, rule := range rules {
		names = append(names, string(rule))
	}
	return strings.Join(names, ", ")
}

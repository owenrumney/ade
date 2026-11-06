// Package oneshot runs one cold `claude -p` turn and decodes what it streamed.
package oneshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Event struct {
	Type      string
	Subtype   string
	IsError   bool
	SessionID string
	CostUSD   float64

	// Result is the final assistant text, set only on Type == "result".
	Result string

	Text        string
	ToolUses    []ToolUse
	ToolResults []ToolResult

	// Notice is what the harness put to the agent as a user message.
	Notice string

	Usage *Usage
}

type Turn struct {
	Text      string
	Events    []Event
	CostUSD   float64
	SessionID string
}

type Usage struct {
	Input         int
	Output        int
	CacheRead     int
	CacheCreation int
}

// Context is the tokens in the window: everything the model read.
func (u Usage) Context() int { return u.Input + u.CacheRead + u.CacheCreation }

// ToolUse is one tool call. Input is left raw for the caller to unmarshal.
type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is what a tool call returned, matched to the call by its id.
type ToolResult struct {
	ID      string
	Content string
	IsError bool
}

type rawEvent struct {
	Type      string    `json:"type"`
	Subtype   string    `json:"subtype"`
	IsError   bool      `json:"is_error"`
	Result    string    `json:"result"`
	SessionID string    `json:"session_id"`
	CostUSD   float64   `json:"total_cost_usd"`
	Usage     *rawUsage `json:"usage"`
	Message   struct {
		Usage   *rawUsage `json:"usage"`
		Content []struct {
			Type      string          `json:"type"`
			Text      string          `json:"text"`
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Input     json.RawMessage `json:"input"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
			IsError   bool            `json:"is_error"`
		} `json:"content"`
	} `json:"message"`
}

type rawUsage struct {
	Input         int `json:"input_tokens"`
	Output        int `json:"output_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
}

func (r *rawUsage) usage() *Usage {
	if r == nil {
		return nil
	}
	return &Usage{Input: r.Input, Output: r.Output, CacheRead: r.CacheRead, CacheCreation: r.CacheCreation}
}

func DecodeEvent(line []byte) (Event, bool) {
	var raw rawEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return decodeNotice(line)
	}
	ev := Event{
		Type:      raw.Type,
		Subtype:   raw.Subtype,
		IsError:   raw.IsError,
		SessionID: raw.SessionID,
		CostUSD:   raw.CostUSD,
		Result:    raw.Result,
		Usage:     raw.Message.Usage.usage(),
	}
	if ev.Usage == nil {
		ev.Usage = raw.Usage.usage()
	}
	var text strings.Builder
	for _, block := range raw.Message.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			ev.ToolUses = append(ev.ToolUses, ToolUse{ID: block.ID, Name: block.Name, Input: block.Input})
		case "tool_result":
			ev.ToolResults = append(ev.ToolResults, ToolResult{
				ID: block.ToolUseID, Content: resultText(block.Content), IsError: block.IsError,
			})
		}
	}
	ev.Text = text.String()
	return ev, true
}

// decodeNotice is a user message whose content is a string, not blocks.
func decodeNotice(line []byte) (Event, bool) {
	var raw struct {
		Type    string `json:"type"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &raw) != nil || raw.Type != "user" || raw.Message.Content == "" {
		return Event{}, false
	}
	return Event{Type: "user", Notice: raw.Message.Content}, true
}

// resultText flattens a tool result, a string or a list of blocks, to text.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// FindToolUse returns the input of the last call to the named tool, since a
// report-style tool called repeatedly is only complete on its final call.
func FindToolUse(events []Event, name string) (json.RawMessage, bool) {
	var found json.RawMessage
	for _, ev := range events {
		for _, tu := range ev.ToolUses {
			if tu.Name == name {
				found = tu.Input
			}
		}
	}
	return found, found != nil
}

// TurnError is why a result event said the turn failed.
func TurnError(ev Event) error {
	if msg := strings.TrimSpace(ev.Result); msg != "" {
		return errors.New(msg)
	}
	if ev.Subtype != "" {
		return fmt.Errorf("claude turn failed: %s", ev.Subtype)
	}
	return errors.New("claude turn failed")
}

// withoutAnthropicKeys keeps Claude Code on the subscription login.
func withoutAnthropicKeys(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key == "ANTHROPIC_API_KEY" || key == "ANTHROPIC_AUTH_TOKEN" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func stderrTail(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	if len(stderr) > 4000 {
		stderr = stderr[len(stderr)-4000:]
	}
	return "\nstderr:\n" + stderr
}

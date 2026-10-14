package bridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/limits"
)

// codexItem is the union of the ThreadItem variants ade shows. Fields it does
// not read are left off; a variant it does not know decodes to an empty Type.
type codexItem struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Command   string          `json:"command"`
	Output    string          `json:"aggregatedOutput"`
	ExitCode  *int            `json:"exitCode"`
	Status    string          `json:"status"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Namespace string          `json:"namespace"`
	Arguments json.RawMessage `json:"arguments"`
	Query     string          `json:"query"`
	Changes   json.RawMessage `json:"changes"`
	Path      string          `json:"path"`
	// What an MCP tool returned, and what a dynamic tool returned. Without
	// them a tool row shows the status word in place of its whole output.
	Result       json.RawMessage `json:"result"`
	ContentItems json.RawMessage `json:"contentItems"`
	Error        *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// decodeCodexEvent maps one server notification onto an Event. The deltas are
// ignored: the chat appends a row per event, and the item carries the text.
func decodeCodexEvent(method string, params json.RawMessage) (Event, bool) {
	switch method {
	case "item/started":
		return codexItemStarted(params)
	case "item/completed":
		return codexItemCompleted(params)
	case "thread/tokenUsage/updated":
		return codexUsage(params)
	case "turn/completed":
		return codexTurnCompleted(params)
	case "error":
		var raw struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			WillRetry bool `json:"willRetry"`
		}
		if json.Unmarshal(params, &raw) != nil || raw.WillRetry {
			return Event{}, false
		}
		return Event{Type: "result", IsError: true, Result: raw.Error.Message}, true
	}
	return Event{}, false
}

// codexItemStarted reports the tool call. An agent message has nothing to show
// until it is complete, and a reasoning item has nowhere in the chat to go.
func codexItemStarted(params json.RawMessage) (Event, bool) {
	item, ok := codexItemOf(params)
	if !ok {
		return Event{}, false
	}
	name, input := codexToolCall(item)
	if name == "" {
		return Event{}, false
	}
	return Event{Type: "assistant", ToolUses: []ToolUse{{ID: item.ID, Name: name, Input: input}}}, true
}

// codexItemCompleted is the agent's message, or what a tool call returned.
func codexItemCompleted(params json.RawMessage) (Event, bool) {
	item, ok := codexItemOf(params)
	if !ok {
		return Event{}, false
	}
	if item.Type == "agentMessage" {
		if item.Text == "" {
			return Event{}, false
		}
		return Event{Type: "assistant", Text: item.Text}, true
	}
	if name, _ := codexToolCall(item); name == "" {
		return Event{}, false
	}
	result := ToolResult{ID: item.ID, Content: codexToolOutput(item), IsError: codexFailed(item)}
	return Event{Type: "user", ToolResults: []ToolResult{result}}, true
}

func codexItemOf(params json.RawMessage) (codexItem, bool) {
	var raw struct {
		Item codexItem `json:"item"`
	}
	if json.Unmarshal(params, &raw) != nil || raw.Item.ID == "" {
		return codexItem{}, false
	}
	return raw.Item, true
}

// codexToolCall names the call the way the chat labels one, with its arguments
// as the raw JSON a caller can unmarshal. Empty name is not a tool call.
func codexToolCall(item codexItem) (string, json.RawMessage) {
	switch item.Type {
	case "commandExecution":
		return "Bash", jsonOf(map[string]any{"command": item.Command})
	case "fileChange":
		return "Edit", jsonOf(map[string]any{"changes": json.RawMessage(item.Changes)})
	case "webSearch":
		return "WebSearch", jsonOf(map[string]any{"query": item.Query})
	case "imageView":
		return "Read", jsonOf(map[string]any{"file_path": item.Path})
	case "mcpToolCall":
		return item.Server + "__" + item.Tool, item.Arguments
	case "dynamicToolCall":
		return item.Tool, item.Arguments
	}
	return "", nil
}

func codexToolOutput(item codexItem) string {
	if item.Error != nil && item.Error.Message != "" {
		return item.Error.Message
	}
	if item.Output != "" {
		return item.Output
	}
	if text := codexToolText(item); text != "" {
		return text
	}
	if item.ExitCode != nil {
		return fmt.Sprintf("exit %d", *item.ExitCode)
	}
	return item.Status
}

// codexToolText is what an MCP or dynamic tool returned: the text of its
// content parts, or the raw JSON when the shape is not one ade knows.
func codexToolText(item codexItem) string {
	if text, ok := contentText(item.ContentItems); ok {
		return text
	}
	if len(item.Result) == 0 {
		return ""
	}
	var wrapped struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(item.Result, &wrapped) == nil {
		if text, ok := contentText(wrapped.Content); ok {
			return text
		}
	}
	return strings.TrimSpace(string(item.Result))
}

func contentText(raw json.RawMessage) (string, bool) {
	var parts []struct {
		Text string `json:"text"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Text != "" {
			out = append(out, part.Text)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "\n"), true
}

func codexFailed(item codexItem) bool {
	if item.Error != nil {
		return true
	}
	if item.ExitCode != nil && *item.ExitCode != 0 {
		return true
	}
	return item.Status == "failed" || item.Status == "declined"
}

// codexUsage is the thread's running token count. `last` is the request that
// produced the message, which is what the window meter reads.
func codexUsage(params json.RawMessage) (Event, bool) {
	var raw struct {
		TokenUsage struct {
			Last struct {
				InputTokens     int `json:"inputTokens"`
				CachedInput     int `json:"cachedInputTokens"`
				CacheWriteInput int `json:"cacheWriteInputTokens"`
				OutputTokens    int `json:"outputTokens"`
			} `json:"last"`
		} `json:"tokenUsage"`
	}
	if json.Unmarshal(params, &raw) != nil {
		return Event{}, false
	}
	last := raw.TokenUsage.Last
	// Codex reports cached tokens inside the input count; Claude reports them
	// beside it, and Usage.Context sums the two.
	input := last.InputTokens - last.CachedInput
	if input < 0 {
		input = 0
	}
	usage := &Usage{Input: input, Output: last.OutputTokens, CacheRead: last.CachedInput, CacheCreation: last.CacheWriteInput}
	if usage.Context() == 0 && usage.Output == 0 {
		return Event{}, false
	}
	return Event{Type: "assistant", Usage: usage}, true
}

func codexTurnCompleted(params json.RawMessage) (Event, bool) {
	var raw struct {
		Turn struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	if json.Unmarshal(params, &raw) != nil {
		return Event{}, false
	}
	// A failed turn is not something the agent said: it must not enter the
	// transcript as prose, or a later provider switch carries it as context.
	if raw.Turn.Status == "failed" {
		message := "the turn failed"
		if raw.Turn.Error != nil && raw.Turn.Error.Message != "" {
			message = raw.Turn.Error.Message
		}
		return Event{Type: "result", IsError: true, Result: message}, true
	}
	return Event{Type: "result"}, true
}

// codexRateLimits turns the rolling snapshot into the windows the badge reads.
func codexRateLimits(params json.RawMessage) (limits.Limits, bool) {
	var raw struct {
		RateLimits struct {
			Primary   *codexWindow `json:"primary"`
			Secondary *codexWindow `json:"secondary"`
		} `json:"rateLimits"`
	}
	if json.Unmarshal(params, &raw) != nil {
		return limits.Limits{}, false
	}
	out := limits.Limits{Provider: "codex", CheckedAt: time.Now()}
	for _, w := range []struct {
		window *codexWindow
		name   string
	}{{raw.RateLimits.Primary, "5h"}, {raw.RateLimits.Secondary, "7d"}} {
		if w.window == nil {
			continue
		}
		out.Windows = append(out.Windows, w.window.window(w.name))
	}
	return out, len(out.Windows) > 0
}

type codexWindow struct {
	UsedPercent float64 `json:"usedPercent"`
	ResetsAt    int64   `json:"resetsAt"`
	Minutes     int     `json:"windowDurationMins"`
}

func (w codexWindow) window(name string) limits.Window {
	out := limits.Window{Name: name, Percent: w.UsedPercent}
	if w.ResetsAt > 0 {
		out.ResetsAt = time.Unix(w.ResetsAt, 0)
	}
	switch {
	case w.Minutes <= 0:
	case w.Minutes <= 300:
		out.Name = "5h"
	case w.Minutes >= 7*24*60:
		out.Name = "7d"
	default:
		out.Name = fmt.Sprintf("%dh", w.Minutes/60)
	}
	return out
}

func jsonOf(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

// otelEndpointOverride is the `-c` that points the user's OTLP exporter at a
// collector on the host, which "localhost" inside the container is not.
func otelEndpointOverride(config []byte, host string) string {
	if host == "" || len(config) == 0 {
		return ""
	}
	variant, endpoint, ok := otelExporterEndpoint(string(config))
	if !ok {
		return ""
	}
	rewritten, ok := swapHost(endpoint, host)
	if !ok {
		return ""
	}
	return fmt.Sprintf("otel.exporter.%s.endpoint=%q", variant, rewritten)
}

// otelExporterEndpoint finds the OTLP exporter variant and endpoint in a
// config.toml, or reports nothing rather than guess at a key path.
func otelExporterEndpoint(config string) (string, string, bool) {
	table := ""
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if cut := strings.Index(line, "#"); cut == 0 {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			table = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if table == "otel" && key == "exporter" {
			if variant, endpoint, found := inlineExporter(value); found {
				return variant, endpoint, true
			}
			continue
		}
		if key == "endpoint" && strings.HasPrefix(table, "otel.exporter.") {
			endpoint, ok := tomlString(value)
			if !ok {
				continue
			}
			return strings.TrimPrefix(table, "otel.exporter."), endpoint, true
		}
	}
	return "", "", false
}

// tomlString is the quoted value alone. Trimming quotes off the line would
// keep a trailing comment, which is ordinary TOML after a value.
func tomlString(value string) (string, bool) {
	open := strings.IndexAny(value, `"'`)
	if open < 0 {
		return "", false
	}
	rest := value[open+1:]
	end := strings.IndexByte(rest, value[open])
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// inlineExporter reads `exporter = { otlp-grpc = { endpoint = "..." } }`.
func inlineExporter(value string) (string, string, bool) {
	inner := strings.TrimSpace(strings.Trim(value, "{}"))
	variant, rest, ok := strings.Cut(inner, "=")
	if !ok {
		return "", "", false
	}
	variant = strings.TrimSpace(variant)
	if !strings.HasPrefix(variant, "otlp-") {
		return "", "", false
	}
	_, endpoint, ok := strings.Cut(rest, "endpoint")
	if !ok {
		return "", "", false
	}
	endpoint = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(endpoint), "="))
	quote := strings.IndexAny(endpoint, `"'`)
	if quote < 0 {
		return "", "", false
	}
	endpoint = endpoint[quote+1:]
	end := strings.IndexAny(endpoint, `"'`)
	if end < 0 {
		return "", "", false
	}
	return variant, endpoint[:end], true
}

// swapHost replaces a loopback host in a URL. Anything else is already
// reachable from the container and is left alone.
func swapHost(endpoint, host string) (string, bool) {
	for _, local := range []string{"localhost", "127.0.0.1", "0.0.0.0", "[::1]"} {
		if strings.Contains(endpoint, "//"+local) {
			return strings.Replace(endpoint, "//"+local, "//"+host, 1), true
		}
	}
	return "", false
}

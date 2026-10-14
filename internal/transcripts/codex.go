package transcripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type codexLine struct {
	Timestamp time.Time     `json:"timestamp"`
	Type      string        `json:"type"`
	Payload   *codexPayload `json:"payload"`
}

type codexPayload struct {
	Type       string          `json:"type"`
	Status     string          `json:"status"`
	TurnID     string          `json:"turn_id"`
	RootTurnID string          `json:"root_turn_id"`
	DurationMS int64           `json:"duration_ms"`
	Error      json.RawMessage `json:"error"`
	Item       *codexItem      `json:"item"`

	// session_meta
	ID  string `json:"id"`
	CWD string `json:"cwd"`

	// turn_context
	Model  string `json:"model"`
	Effort string `json:"effort"`

	// response_item
	Role      string          `json:"role"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Content   []codexBlock    `json:"content"`

	// event_msg user_message / agent_message
	Message string `json:"message"`

	// event_msg token_count
	Info *codexTokenInfo `json:"info"`
}

type codexItem struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Tool   string `json:"tool"`
}

type codexBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexTokenInfo struct {
	Total  *codexTokens `json:"total_token_usage"`
	Last   *codexTokens `json:"last_token_usage"`
	Window int          `json:"model_context_window"`
}

type codexTokens struct {
	InputTokens     int `json:"input_tokens"`
	CachedInput     int `json:"cached_input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ReasoningOutput int `json:"reasoning_output_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

type codexIndexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
}

func codexRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func indexCodex() []*Session {
	root := codexRoot()
	if root == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return nil
	}
	sessions := indexFiles(matches, indexCodexFile)
	// The thread names live beside the transcripts, not in them, so they are
	// applied over the cache rather than stored in it.
	titles := codexThreadNames()
	for _, s := range sessions {
		if name, ok := titles[s.ID]; ok {
			s.Title = name
		}
	}
	return sessions
}

func indexCodexFile(path string) *Session {
	s := &Session{Agent: AgentCodex, Path: path}
	if unix, ok := modTime(path); ok {
		s.LastUsed = time.Unix(unix, 0)
	}
	lines := 0
	err := scanJSONL(path, func(raw []byte) error {
		if lines++; lines > indexLineCap {
			return errStopScan
		}
		var line codexLine
		if json.Unmarshal(raw, &line) != nil || line.Payload == nil {
			return nil
		}
		p := line.Payload
		switch {
		case line.Type == "session_meta":
			s.ID, s.CWD = p.ID, p.CWD
		case line.Type == "turn_context" && p.Model != "":
			s.Model = p.Model
		case p.Type == "user_message":
			s.setFirstPrompt(p.Message)
		case p.Type == "message" && p.Role == "user":
			s.setFirstPrompt(codexText(p.Content))
		}
		if s.CWD != "" && s.FirstPrompt != "" && s.Model != "" {
			return errStopScan
		}
		return nil
	})
	if err != nil || s.ID == "" {
		return nil
	}
	return s
}

func loadCodex(s *Session) error {
	var last *codexTokenInfo
	var running Usage
	roots := map[string]int{}
	err := scanJSONL(s.Path, func(raw []byte) error {
		var line codexLine
		if json.Unmarshal(raw, &line) != nil || line.Payload == nil {
			return nil
		}
		p := line.Payload
		if line.Type == "turn_context" && p.Model != "" {
			s.Model = p.Model
		}
		switch p.Type {
		case "task_started":
			root := p.RootTurnID
			if root == "" {
				root = p.TurnID
			}
			if roots[root] > 0 {
				s.Retries++
			}
			roots[root]++
		case "task_complete":
			s.TurnsDone = append(s.TurnsDone, Turn{
				At: line.Timestamp, Model: s.Model,
				Duration: time.Duration(p.DurationMS) * time.Millisecond,
				Failed:   len(p.Error) > 0 && string(p.Error) != "null",
			})
		case "item_completed":
			if p.Item != nil && (p.Item.Status == "failed" || p.Item.Status == "error") {
				s.failTool(codexItemTool(p.Item))
			}
		case "user_message":
			s.addPrompt(line.Timestamp, p.Message)
		case "message":
			if p.Role == "user" {
				s.addPrompt(line.Timestamp, codexText(p.Content))
			}
		case "function_call", "custom_tool_call":
			s.addTool(p.Name)
			if p.Status == "failed" || p.Status == "error" {
				s.failTool(p.Name)
			}
		case "tool_search_call":
			s.addTool("tool_search")
		case "token_count":
			if p.Info != nil {
				// Running totals, so what this event spent is what it added.
				if t := p.Info.Total; t != nil {
					now := codexUsage(t)
					s.addCharge(line.Timestamp, s.Model, sub(now, running))
					running = now
				}
				last = p.Info
			}
		}
		if line.Type == "compacted" {
			s.Compactions++
		}
		return nil
	})
	if err != nil {
		return err
	}
	// token_count carries running totals, so only the final event matters.
	if last != nil {
		s.ContextWindow = last.Window
		if last.Last != nil {
			s.ContextTokens = last.Last.InputTokens
		}
		if t := last.Total; t != nil {
			usage := codexUsage(t)
			usage.CostUSD, usage.CostKnown = costFor(s.Model, usage)
			s.Usage = usage
		}
	}
	s.Turns = len(s.Prompts)
	return nil
}

func codexItemTool(item *codexItem) string {
	if item == nil {
		return "unknown"
	}
	if item.Tool != "" {
		return item.Tool
	}
	switch item.Type {
	case "CommandExecution":
		return "exec"
	case "FileChange":
		return "apply_patch"
	}
	return item.Type
}

func codexText(blocks []codexBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return cleanPrompt(strings.Join(parts, " "))
}

// codexThreadNames reads the session index for its human-readable thread titles,
// which the rollout files themselves do not carry. Re-read on every scan: it is
// one small file, and caching it hid renamed and newly created threads.
func codexThreadNames() map[string]string {
	titles := map[string]string{}
	root := codexRoot()
	if root == "" {
		return titles
	}
	_ = scanJSONL(filepath.Join(root, "session_index.jsonl"), func(raw []byte) error {
		var entry codexIndexEntry
		if json.Unmarshal(raw, &entry) == nil && entry.ID != "" && entry.ThreadName != "" {
			titles[entry.ID] = entry.ThreadName
		}
		return nil
	})
	return titles
}

// codexUsage is one token_count total. OpenAI counts cached tokens inside
// input_tokens; split them so the cheaper cache rate applies.
func codexUsage(t *codexTokens) Usage {
	return Usage{
		Input:     max(t.InputTokens-t.CachedInput, 0),
		Output:    t.OutputTokens,
		CacheRead: t.CachedInput,
		Reasoning: t.ReasoningOutput,
	}
}

// sub is what one running total added over the last. A total below the last is
// a restarted count; one field below it is a glitch, and clamping charges once.
func sub(now, was Usage) Usage {
	if now.Total()+now.Reasoning < was.Total()+was.Reasoning {
		return now
	}
	out := Usage{
		Input:     max(now.Input-was.Input, 0),
		Output:    max(now.Output-was.Output, 0),
		CacheRead: max(now.CacheRead-was.CacheRead, 0),
		Reasoning: max(now.Reasoning-was.Reasoning, 0),
	}
	return out
}

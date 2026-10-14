package transcripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type claudeLine struct {
	Type        string          `json:"type"`
	Timestamp   time.Time       `json:"timestamp"`
	CWD         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	AITitle     string          `json:"aiTitle"`
	AgentName   string          `json:"agentSetting"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	RequestID   string          `json:"requestId"`
	Subtype     string          `json:"subtype"`
	DurationMS  int64           `json:"duration_ms"`
	DurationMS2 int64           `json:"durationMs"`
	IsError     bool            `json:"is_error"`
	Message     *claudeMessage  `json:"message"`
	ToolUseRes  json.RawMessage `json:"toolUseResult"`
}

type claudeMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
}

type claudeUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

func claudeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

func indexClaude() []*Session {
	root := claudeRoot()
	if root == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return nil
	}
	return indexFiles(matches, indexClaudeFile)
}

// ClaudeSession is the session recorded in one Claude Code transcript, not
// yet loaded, or nil for a file with nothing in it. For the checkout that
// knows exactly which file it wants — the running session's — rather than
// a scan of everything.
func ClaudeSession(path string) *Session {
	return indexClaudeFile(path)
}

func indexClaudeFile(path string) *Session {
	s := &Session{
		Agent: AgentClaude,
		ID:    strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path:  path,
	}
	if unix, ok := modTime(path); ok {
		s.LastUsed = time.Unix(unix, 0)
	}
	lines, hitCap := 0, false
	err := scanJSONL(path, func(raw []byte) error {
		if lines++; lines > indexLineCap {
			hitCap = true
			return errStopScan
		}
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			return nil
		}
		if s.CWD == "" && line.CWD != "" {
			s.CWD = line.CWD
		}
		if s.Branch == "" && line.GitBranch != "" {
			s.Branch = line.GitBranch
		}
		if line.Type == "ai-title" && line.AITitle != "" {
			s.Title = line.AITitle
		}
		// A named agent's run announces itself on the first line. It is a
		// reply inside somebody's conversation, not a conversation to reopen.
		if line.Type == "agent-setting" && line.AgentName != "" {
			s.Subagent = line.AgentName
		}
		if line.Type == "assistant" && line.Message != nil && line.Message.Model != "" && !strings.HasPrefix(line.Message.Model, "<") {
			s.Model = line.Message.Model
		}
		if line.Type == "user" && !line.IsMeta && !line.IsSidechain {
			s.setFirstPrompt(claudeUserText(line.Message))
		}
		if s.CWD != "" && s.FirstPrompt != "" && s.Title != "" {
			return errStopScan
		}
		return nil
	})
	if err != nil && s.CWD == "" {
		return nil
	}
	// A fully-read transcript with nothing the user typed is a session that was
	// cleared or abandoned before it started. Not worth listing.
	if !hitCap && s.FirstPrompt == "" && s.Title == "" {
		return nil
	}
	if s.CWD == "" {
		s.CWD = claudeCWDFromDir(path)
	}
	return s
}

func loadClaude(s *Session) error {
	seen := map[string]bool{}
	tools := map[string]string{}
	byModel := map[string]Usage{}
	err := scanJSONL(s.Path, func(raw []byte) error {
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil {
			return nil
		}
		if line.Message == nil {
			if line.Type == "system" && line.Subtype == "compact_boundary" {
				s.Compactions++
			}
			if line.Type == "result" {
				duration := line.DurationMS
				if duration == 0 {
					duration = line.DurationMS2
				}
				s.TurnsDone = append(s.TurnsDone, Turn{At: line.Timestamp, Model: s.Model, Duration: time.Duration(duration) * time.Millisecond, Failed: line.IsError})
			}
			return nil
		}
		switch line.Type {
		case "user":
			if line.IsMeta || line.IsSidechain {
				return nil
			}
			for _, block := range claudeBlocks(line.Message.Content) {
				if block.Type == "tool_result" && block.IsError {
					s.failTool(tools[block.ToolUseID])
				}
			}
			if text := claudeUserText(line.Message); text != "" {
				s.addPrompt(line.Timestamp, text)
			}
		case "assistant":
			if line.Message.Model != "" && !strings.HasPrefix(line.Message.Model, "<") {
				s.Model = line.Message.Model
			}
			for _, block := range claudeBlocks(line.Message.Content) {
				if block.Type == "tool_use" {
					s.addTool(block.Name)
					tools[block.ID] = block.Name
				}
			}
			// One API response spans several lines, each repeating the same
			// usage. Count it once per request.
			key := line.RequestID
			if key == "" {
				key = line.Message.ID
			}
			if key == "" || seen[key] || line.Message.Usage == nil {
				return nil
			}
			seen[key] = true
			u := line.Message.Usage
			// The final request's prompt side is how full the context got.
			s.ContextTokens = u.InputTokens + u.CacheReadTokens + u.CacheCreationTokens
			model := line.Message.Model
			one := Usage{
				Input:      u.InputTokens,
				Output:     u.OutputTokens,
				CacheRead:  u.CacheReadTokens,
				CacheWrite: u.CacheCreationTokens,
			}
			s.addCharge(line.Timestamp, model, one)
			acc := byModel[model]
			acc.Input += one.Input
			acc.Output += one.Output
			acc.CacheRead += one.CacheRead
			acc.CacheWrite += one.CacheWrite
			byModel[model] = acc
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.Usage = sumByModel(byModel)
	s.Turns = len(s.Prompts)
	return nil
}

// sumByModel totals usage across models and prices each model separately. Cost
// is known only when every model that spent tokens could be priced.
func sumByModel(byModel map[string]Usage) Usage {
	var total Usage
	total.CostKnown = true
	for model, u := range byModel {
		total.Input += u.Input
		total.Output += u.Output
		total.CacheRead += u.CacheRead
		total.CacheWrite += u.CacheWrite
		total.Reasoning += u.Reasoning
		cost, ok := costFor(model, u)
		if !ok {
			total.CostKnown = false
			continue
		}
		total.CostUSD += cost
	}
	if len(byModel) == 0 {
		total.CostKnown = false
	}
	return total
}

// claudeUserText pulls the typed text out of a user turn, ignoring tool
// results, and returns "" when the turn held no prompt.
func claudeUserText(msg *claudeMessage) string {
	if msg == nil {
		return ""
	}
	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		return cleanPrompt(text)
	}
	var parts []string
	for _, block := range claudeBlocks(msg.Content) {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return cleanPrompt(strings.Join(parts, " "))
}

func claudeBlocks(content json.RawMessage) []claudeBlock {
	if len(content) == 0 || content[0] != '[' {
		return nil
	}
	var blocks []claudeBlock
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	return blocks
}

// claudeCWDFromDir unslugs Claude's project directory name. Ambiguous when a
// directory name contains dashes, so it is only a fallback.
func claudeCWDFromDir(path string) string {
	slug := filepath.Base(filepath.Dir(path))
	if !strings.HasPrefix(slug, "-") {
		return slug
	}
	return string(filepath.Separator) + strings.ReplaceAll(strings.TrimPrefix(slug, "-"), "-", string(filepath.Separator))
}

package transcripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type piLine struct {
	Type      string     `json:"type"`
	Timestamp time.Time  `json:"timestamp"`
	CWD       string     `json:"cwd"`
	ID        string     `json:"id"`
	ModelID   string     `json:"modelId"`
	Provider  string     `json:"provider"`
	Message   *piMessage `json:"message"`
}

type piMessage struct {
	Role     string    `json:"role"`
	Model    string    `json:"model"`
	Provider string    `json:"provider"`
	Content  []piBlock `json:"content"`
	Usage    *piUsage  `json:"usage"`
}

type piBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type piUsage struct {
	Input       int     `json:"input"`
	Output      int     `json:"output"`
	CacheRead   int     `json:"cacheRead"`
	CacheWrite  int     `json:"cacheWrite"`
	Reasoning   int     `json:"reasoning"`
	TotalTokens int     `json:"totalTokens"`
	Cost        *piCost `json:"cost"`
}

type piCost struct {
	Total float64 `json:"total"`
}

func piRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

func indexPi() []*Session {
	root := piRoot()
	if root == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return nil
	}
	return indexFiles(matches, indexPiFile)
}

// PiSession indexes one pi transcript. The counterpart of ClaudeSession.
func PiSession(path string) *Session {
	return indexPiFile(path)
}

func indexPiFile(path string) *Session {
	s := &Session{Agent: AgentPi, Path: path, ID: piSessionID(path)}
	if unix, ok := modTime(path); ok {
		s.LastUsed = time.Unix(unix, 0)
	}
	lines := 0
	err := scanJSONL(path, func(raw []byte) error {
		if lines++; lines > indexLineCap {
			return errStopScan
		}
		var line piLine
		if json.Unmarshal(raw, &line) != nil {
			return nil
		}
		switch line.Type {
		case "session":
			if line.CWD != "" {
				s.CWD = line.CWD
			}
			if line.ID != "" {
				s.ID = line.ID
			}
		case "model_change":
			if line.ModelID != "" {
				s.Model = line.ModelID
			}
		case "message":
			if line.Message == nil || line.Message.Role != "user" {
				return nil
			}
			if s.setFirstPrompt(piText(line.Message)) {
				return errStopScan
			}
		}
		return nil
	})
	if err != nil && s.CWD == "" {
		return nil
	}
	if s.CWD == "" {
		s.CWD = piCWDFromDir(path)
	}
	return s
}

func loadPi(s *Session) error {
	var usage Usage
	err := scanJSONL(s.Path, func(raw []byte) error {
		var line piLine
		if json.Unmarshal(raw, &line) != nil {
			return nil
		}
		if line.Type == "model_change" && line.ModelID != "" {
			s.Model = line.ModelID
		}
		if line.Type != "message" || line.Message == nil {
			return nil
		}
		switch line.Message.Role {
		case "user":
			s.addPrompt(line.Timestamp, piText(line.Message))
		case "assistant":
			if line.Message.Model != "" {
				s.Model = line.Message.Model
			}
			// Tools come from the toolCall blocks only: every call also produces
			// a toolResult message, so counting both would double every tool.
			for _, block := range line.Message.Content {
				if block.Type == "toolCall" {
					s.addTool(block.Name)
				}
			}
			u := line.Message.Usage
			if u == nil {
				return nil
			}
			one := Usage{
				Input: u.Input, Output: u.Output, CacheRead: u.CacheRead,
				CacheWrite: u.CacheWrite, Reasoning: u.Reasoning,
			}
			if u.Cost != nil {
				one.CostUSD, one.CostKnown = u.Cost.Total, true
			}
			s.addCharge(line.Timestamp, s.Model, one)
			usage.Input += u.Input
			usage.Output += u.Output
			usage.CacheRead += u.CacheRead
			usage.CacheWrite += u.CacheWrite
			usage.Reasoning += u.Reasoning
			if u.TotalTokens > 0 {
				s.ContextTokens = u.TotalTokens
			}
			// pi records provider-reported cost per turn, so trust it over
			// our pricing table.
			if u.Cost != nil {
				usage.CostUSD += u.Cost.Total
				usage.CostKnown = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !usage.CostKnown {
		if cost, ok := costFor(s.Model, usage); ok {
			usage.CostUSD, usage.CostKnown = cost, true
		}
	}
	s.Usage = usage
	s.Turns = len(s.Prompts)
	return nil
}

// piText joins the text blocks of a pi message; tool calls and thinking are
// skipped so only what the user typed survives.
func piText(msg *piMessage) string {
	var parts []string
	for _, block := range msg.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return cleanPrompt(strings.Join(parts, " "))
}

// piSessionID pulls the UUID out of a "<timestamp>_<uuid>.jsonl" filename.
func piSessionID(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if i := strings.LastIndex(name, "_"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// piCWDFromDir recovers the path from pi's "--Users-owen-code-x--" directory
// name. Only a fallback: dashes in real directory names are ambiguous.
func piCWDFromDir(path string) string {
	slug := strings.Trim(filepath.Base(filepath.Dir(path)), "-")
	if slug == "" {
		return ""
	}
	return string(filepath.Separator) + strings.ReplaceAll(slug, "-", string(filepath.Separator))
}

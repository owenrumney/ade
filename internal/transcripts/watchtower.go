package transcripts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type wtManifest struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Status           string      `json:"status"`
	Agent            string      `json:"agent"`
	SessionDir       string      `json:"session_dir"`
	PrimaryWorkspace string      `json:"primary_workspace"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
	LastUsedAt       time.Time   `json:"last_used_at"`
	Metadata         wtMetadata  `json:"metadata"`
	Workspaces       []wtWorkspc `json:"workspaces"`
}

type wtMetadata struct {
	Description string `json:"description"`
}

type wtWorkspc struct {
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	IsPrimary bool   `json:"is_primary"`
}

// wtEvent is one line of watchtower's harness-native transcript.
type wtEvent struct {
	Time     time.Time    `json:"ts"`
	Type     string       `json:"type"`
	Content  string       `json:"content"`
	ToolName string       `json:"tool_name"`
	Model    string       `json:"model"`
	Usage    *wtUsage     `json:"usage"`
	Estimate int          `json:"context_estimate"`
	Calls    []wtToolCall `json:"tool_calls"`
}

type wtUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// wtToolCall nests the tool name OpenAI-style, under function.name.
type wtToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func watchtowerRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".watchtower", "sessions")
}

// indexWatchtower reads the per-session manifests rather than watchtower.db, so
// sm needs no sqlite driver and cannot contend with a running daemon.
func indexWatchtower() []*Session {
	root := watchtowerRoot()
	if root == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "*", "manifest.json"))
	if err != nil {
		return nil
	}
	sessions := make([]*Session, 0, len(matches))
	for _, path := range matches {
		if s := indexWatchtowerFile(path); s != nil {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

func indexWatchtowerFile(path string) *Session {
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from globbing watchtower's session dir.
	if err != nil {
		return nil
	}
	var m wtManifest
	if json.Unmarshal(raw, &m) != nil || m.ID == "" {
		return nil
	}
	dir := m.SessionDir
	if dir == "" {
		dir = filepath.Dir(path)
	}
	s := &Session{
		Agent:    AgentWatchtower,
		ID:       m.ID,
		Path:     filepath.Join(dir, "run", "harness-transcript.jsonl"),
		CWD:      m.PrimaryWorkspace,
		Status:   m.Status,
		Title:    m.Name,
		LastUsed: wtLastUsed(m, dir),
	}
	// The manifest description is the session's headline, but it is optional;
	// fall back to the first thing typed into the transcript.
	if !s.setFirstPrompt(m.Metadata.Description) {
		wtPeekPrompt(s)
	}
	for _, ws := range m.Workspaces {
		if ws.IsPrimary && ws.Branch != "" {
			s.Branch = ws.Branch
			break
		}
	}
	return s
}

func loadWatchtower(s *Session) error {
	byModel := map[string]Usage{}
	model := ""
	err := scanJSONL(s.Path, func(raw []byte) error {
		var event wtEvent
		if json.Unmarshal(raw, &event) != nil {
			return nil
		}
		switch event.Type {
		case "user":
			s.addPrompt(event.Time, event.Content)
		case "tool_result":
			// Every dispatched tool lands here by name; the assistant event's
			// tool_calls repeat them, so counting both would double each tool.
			s.addTool(event.ToolName)
		case "usage":
			if event.Model != "" {
				model = event.Model
			}
			if event.Usage == nil {
				return nil
			}
			one := Usage{Input: event.Usage.PromptTokens, Output: event.Usage.CompletionTokens}
			s.addCharge(event.Time, model, one)
			acc := byModel[model]
			acc.Input += event.Usage.PromptTokens
			acc.Output += event.Usage.CompletionTokens
			byModel[model] = acc
			if event.Estimate > 0 {
				s.ContextTokens = event.Estimate
			} else if event.Usage.PromptTokens > 0 {
				s.ContextTokens = event.Usage.PromptTokens
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if model != "" {
		s.Model = model
	}
	s.Usage = sumByModel(byModel)
	s.Turns = len(s.Prompts)
	return nil
}

// wtPeekPrompt reads just far enough into the transcript to find the opening
// prompt, for sessions whose manifest carries no description.
func wtPeekPrompt(s *Session) {
	lines := 0
	_ = scanJSONL(s.Path, func(raw []byte) error {
		if lines++; lines > indexLineCap {
			return errStopScan
		}
		var event wtEvent
		if json.Unmarshal(raw, &event) != nil || event.Type != "user" {
			return nil
		}
		if s.setFirstPrompt(event.Content) {
			return errStopScan
		}
		return nil
	})
}

// wtLastUsed prefers the transcript's mtime, which reflects real activity
// better than the manifest's own timestamps.
func wtLastUsed(m wtManifest, dir string) time.Time {
	if unix, ok := modTime(filepath.Join(dir, "run", "harness-transcript.jsonl")); ok {
		return time.Unix(unix, 0)
	}
	if !m.LastUsedAt.IsZero() {
		return m.LastUsedAt
	}
	if !m.UpdatedAt.IsZero() {
		return m.UpdatedAt
	}
	return m.CreatedAt
}

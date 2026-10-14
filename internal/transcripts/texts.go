package transcripts

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

// Role says where a piece of transcript text came from.
type Role string

// Text is one readable line of a transcript.
type Text struct {
	Role Role
	At   time.Time
	Body string
}

const (
	RoleUser  Role = "you"
	RoleAgent Role = "agent"
	RoleTool  Role = "tool"
)

// toolTextLimit caps how much of a tool call's arguments we surface. Enough to
// carry file paths and commands, short of pasting a whole file back.
const toolTextLimit = 400

// textWalkers maps each agent to its transcript text extractor.
var textWalkers = map[Agent]func(string, func(Text) error) error{
	AgentClaude:     claudeTexts,
	AgentCodex:      codexTexts,
	AgentPi:         piTexts,
	AgentWatchtower: watchtowerTexts,
}

// ScanTexts streams what you typed, what the agent said and the tools it called.
// Streaming keeps a multi-megabyte transcript off the heap. errStopScan ends it.
func ScanTexts(s *Session, fn func(Text) error) error {
	walk, ok := textWalkers[s.Agent]
	if !ok {
		return nil
	}
	return walk(s.Path, fn)
}

func claudeTexts(path string, fn func(Text) error) error {
	return scanJSONL(path, func(raw []byte) error {
		var line claudeLine
		if json.Unmarshal(raw, &line) != nil || line.Message == nil || line.IsMeta {
			return nil
		}
		switch line.Type {
		case "user":
			// A sidechain turn is a subagent's brief, written by the agent.
			role := RoleUser
			if line.IsSidechain {
				role = RoleAgent
			}
			return emit(fn, role, line.Timestamp, claudeUserText(line.Message))
		case "assistant":
			for _, block := range claudeBlocks(line.Message.Content) {
				var err error
				switch block.Type {
				case "text":
					err = emit(fn, RoleAgent, line.Timestamp, block.Text)
				case "tool_use":
					err = emit(fn, RoleTool, line.Timestamp, toolText(block.Name, block.Input))
				}
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func piTexts(path string, fn func(Text) error) error {
	return scanJSONL(path, func(raw []byte) error {
		var line piLine
		if json.Unmarshal(raw, &line) != nil || line.Type != "message" || line.Message == nil {
			return nil
		}
		switch line.Message.Role {
		case "user":
			return emit(fn, RoleUser, line.Timestamp, piText(line.Message))
		case "assistant":
			for _, block := range line.Message.Content {
				var err error
				switch block.Type {
				case "text":
					err = emit(fn, RoleAgent, line.Timestamp, block.Text)
				case "toolCall":
					err = emit(fn, RoleTool, line.Timestamp, toolText(block.Name, block.Arguments))
				}
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func codexTexts(path string, fn func(Text) error) error {
	return scanJSONL(path, func(raw []byte) error {
		var line codexLine
		if json.Unmarshal(raw, &line) != nil || line.Payload == nil {
			return nil
		}
		p := line.Payload
		switch p.Type {
		case "user_message":
			return emit(fn, RoleUser, line.Timestamp, cleanPrompt(p.Message))
		case "agent_message":
			return emit(fn, RoleAgent, line.Timestamp, p.Message)
		case "message":
			role := RoleAgent
			if p.Role == "user" {
				role = RoleUser
			}
			return emit(fn, role, line.Timestamp, codexText(p.Content))
		case "function_call", "custom_tool_call":
			return emit(fn, RoleTool, line.Timestamp, toolText(p.Name, p.Arguments))
		}
		return nil
	})
}

func watchtowerTexts(path string, fn func(Text) error) error {
	return scanJSONL(path, func(raw []byte) error {
		var event wtEvent
		if json.Unmarshal(raw, &event) != nil {
			return nil
		}
		switch event.Type {
		case "user":
			return emit(fn, RoleUser, event.Time, event.Content)
		case "assistant":
			if err := emit(fn, RoleAgent, event.Time, event.Content); err != nil {
				return err
			}
			for _, call := range event.Calls {
				if err := emit(fn, RoleTool, event.Time, toolText(call.Function.Name, call.Function.Arguments)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// emit passes one line on, skipping anything that collapsed to nothing.
func emit(fn func(Text) error, role Role, at time.Time, body string) error {
	if body = collapse(body); body == "" {
		return nil
	}
	return fn(Text{Role: role, At: at, Body: body})
}

// toolText renders a tool call as a searchable line: the name plus enough of its
// arguments to carry the paths and commands you might be looking for.
func toolText(name string, args json.RawMessage) string {
	if name == "" && len(args) == 0 {
		return ""
	}
	body := name
	if len(args) > 0 {
		body = strings.TrimSpace(body + " " + collapse(string(args)))
	}
	return truncateRunes(body, toolTextLimit)
}

// lastSaid is one transcript's answer, kept against the file it was read from.
type lastSaid struct {
	size int64
	mod  time.Time
	text string
}

// The launcher asks for every unread session every thirty seconds, and a scan
// reads the whole file; an unchanged transcript is a stat, not a read.
var (
	lastSaidMu sync.Mutex
	lastSaidBy = map[string]lastSaid{}
)

// LastAgent is the last thing the agent said, or "" if it never spoke. One
// streaming pass: only the newest body is kept, never the transcript.
func LastAgent(s *Session) string {
	info, err := os.Stat(s.Path)
	if err != nil {
		return ""
	}
	lastSaidMu.Lock()
	was, ok := lastSaidBy[s.Path]
	lastSaidMu.Unlock()
	if ok && was.size == info.Size() && was.mod.Equal(info.ModTime()) {
		return was.text
	}
	last := ""
	_ = ScanTexts(s, func(t Text) error {
		if t.Role == RoleAgent && strings.TrimSpace(t.Body) != "" {
			last = t.Body
		}
		return nil
	})
	last = strings.TrimSpace(last)
	lastSaidMu.Lock()
	lastSaidBy[s.Path] = lastSaid{size: info.Size(), mod: info.ModTime(), text: last}
	lastSaidMu.Unlock()
	return last
}

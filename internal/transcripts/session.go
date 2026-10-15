// Package session discovers and parses coding-agent transcripts from the
// on-disk stores that claude, codex, pi and watchtower each keep.
package transcripts

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Agent identifies which coding agent produced a transcript.
type Agent string

// Usage is the token spend for a session. CostKnown distinguishes a genuinely
// free session from one whose model we could not price.
type Usage struct {
	Input      int     `json:"input"`
	Output     int     `json:"output"`
	CacheRead  int     `json:"cacheRead"`
	CacheWrite int     `json:"cacheWrite"`
	Reasoning  int     `json:"reasoning"`
	CostUSD    float64 `json:"costUSD"`
	CostKnown  bool    `json:"costKnown"`
}

// Prompt is one thing the user typed.
type Prompt struct {
	At   time.Time
	Text string
}

// Charge is one request's usage and when it was made. A session's total is
// one timestamp, which cannot say which metered window the tokens fell in.
type Charge struct {
	At    time.Time
	Model string
	Usage Usage
}

// Turn is one completed user turn when the transcript records its boundary.
type Turn struct {
	At       time.Time
	Model    string
	Duration time.Duration
	Failed   bool
}

// ToolCount is a tool name and how often the session called it.
type ToolCount struct {
	Name  string
	Count int
}

// Session is one resumable agent conversation. Fields above Loaded come from
// the cheap index pass; the rest need Load, which reads the whole transcript.
type Session struct {
	Agent       Agent
	ID          string
	Path        string
	CWD         string
	Branch      string
	Model       string
	Title       string
	Status      string
	LastUsed    time.Time
	FirstPrompt string
	// Repo is the repository CWD was in when the transcript was indexed. It
	// outlives the worktree, which a purge removes before the day is reported.
	Repo string

	Loaded  bool
	LoadErr error
	Prompts []Prompt
	// Subagent names the agent this transcript is a run of, when it is one.
	// A named agent's run is a reply inside a conversation, not one to reopen.
	Subagent string
	Tools    map[string]int
	Refs     []Reference
	Usage    Usage
	// Charges is the usage of each request, in the order the transcript has
	// them, so a slice of time can be totalled out of a longer session.
	Charges      []Charge
	TurnsDone    []Turn
	Turns        int
	Retries      int
	Compactions  int
	ToolFailures map[string]int
	// ContextTokens is how full the context was on the last turn;
	// ContextWindow is the model's maximum, when the agent records it.
	ContextTokens int
	ContextWindow int
}

const (
	AgentClaude     Agent = "claude"
	AgentCodex      Agent = "codex"
	AgentPi         Agent = "pi"
	AgentWatchtower Agent = "watchtower"
)

// Agents is every agent sm knows how to read, in display order.
var Agents = []Agent{AgentClaude, AgentCodex, AgentPi, AgentWatchtower}

// Total is every token the session consumed, cache included.
func (u Usage) Total() int {
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

// DirLabel is the last path element of the working directory, so
// ~/code/owenrumney/ade shows as "ade".
func (s *Session) DirLabel() string {
	if s.CWD == "" {
		return "?"
	}
	base := filepath.Base(strings.TrimRight(s.CWD, string(filepath.Separator)))
	if base == "." || base == string(filepath.Separator) {
		return s.CWD
	}
	return base
}

// Label is the best one-line description available: the first prompt, falling
// back to an agent-supplied title.
func (s *Session) Label() string {
	if s.FirstPrompt != "" {
		return s.FirstPrompt
	}
	if s.Title != "" {
		return s.Title
	}
	return "(no prompt)"
}

// TopTools returns the most-used tools, highest count first, ties broken by
// name so the detail pane does not reshuffle between renders.
func (s *Session) TopTools(n int) []ToolCount {
	counts := make([]ToolCount, 0, len(s.Tools))
	for name, count := range s.Tools {
		counts = append(counts, ToolCount{Name: name, Count: count})
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return counts[i].Name < counts[j].Name
	})
	if n > 0 && len(counts) > n {
		counts = counts[:n]
	}
	return counts
}

// Repository is the recorded repo, or what the working directory says now.
func (s *Session) Repository() string {
	if s.Repo != "" {
		return s.Repo
	}
	return RepoKey(s.CWD)
}

// LastPrompt is the most recent thing the user typed, if the session is loaded.
func (s *Session) LastPrompt() string {
	if len(s.Prompts) == 0 {
		return s.FirstPrompt
	}
	return s.Prompts[len(s.Prompts)-1].Text
}

// setFirstPrompt records the opening prompt if we do not have one yet, and
// reports whether one is now set. Never overwrites a real prompt with noise.
func (s *Session) setFirstPrompt(text string) bool {
	if s.FirstPrompt != "" {
		return true
	}
	if cleaned := cleanPrompt(text); cleaned != "" {
		s.FirstPrompt = cleaned
		return true
	}
	return false
}

func (s *Session) addTool(name string) {
	if name == "" {
		return
	}
	if s.Tools == nil {
		s.Tools = map[string]int{}
	}
	s.Tools[name]++
}

func (s *Session) failTool(name string) {
	if name == "" {
		name = "unknown"
	}
	if s.ToolFailures == nil {
		s.ToolFailures = map[string]int{}
	}
	s.ToolFailures[name]++
}

func (s *Session) addPrompt(at time.Time, text string) {
	text = cleanPrompt(text)
	if text == "" {
		return
	}
	s.Prompts = append(s.Prompts, Prompt{At: at, Text: text})
	if s.FirstPrompt == "" {
		s.FirstPrompt = text
	}
}

// addCharge records what one request spent. A request that spent nothing is
// not a charge: it would put a session in a window it did no work in.
func (s *Session) addCharge(at time.Time, model string, u Usage) {
	// Reasoning is spend too, and a Codex turn can be nothing else.
	if at.IsZero() || u.Total()+u.Reasoning == 0 {
		return
	}
	s.Charges = append(s.Charges, Charge{At: at, Model: model, Usage: u})
}

func (s *Session) inferTurns() {
	if len(s.TurnsDone) > 0 {
		return
	}
	for i, prompt := range s.Prompts {
		var last Charge
		for _, charge := range s.Charges {
			if charge.At.Before(prompt.At) {
				continue
			}
			if i+1 < len(s.Prompts) && !charge.At.Before(s.Prompts[i+1].At) {
				break
			}
			last = charge
		}
		if last.At.After(prompt.At) {
			s.TurnsDone = append(s.TurnsDone, Turn{At: last.At, Model: last.Model, Duration: last.At.Sub(prompt.At)})
		}
	}
}

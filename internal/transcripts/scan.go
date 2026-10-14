package transcripts

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"
)

// indexRow is what the cheap pass learns about a transcript, spelled out
// rather than stored as a Session: only these fields survive the cache.
type indexRow struct {
	Agent       Agent     `json:"agent"`
	ID          string    `json:"id"`
	CWD         string    `json:"cwd,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Model       string    `json:"model,omitempty"`
	Title       string    `json:"title,omitempty"`
	Status      string    `json:"status,omitempty"`
	Subagent    string    `json:"subagent,omitempty"`
	FirstPrompt string    `json:"firstPrompt,omitempty"`
	LastUsed    time.Time `json:"lastUsed,omitempty"`
}

// indexers maps each agent to its cheap discovery pass.
var indexers = map[Agent]func() []*Session{
	AgentClaude:     indexClaude,
	AgentCodex:      indexCodex,
	AgentPi:         indexPi,
	AgentWatchtower: indexWatchtower,
}

// loaders maps each agent to its full transcript parse.
var loaders = map[Agent]func(*Session) error{
	AgentClaude:     loadClaude,
	AgentCodex:      loadCodex,
	AgentPi:         loadPi,
	AgentWatchtower: loadWatchtower,
}

// Scan indexes every agent's store concurrently, most recent first. A missing
// or unreadable store contributes nothing rather than failing the scan.
func Scan() []*Session {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		sessions []*Session
	)
	for _, agent := range Agents {
		index, ok := indexers[agent]
		if !ok {
			continue
		}
		wg.Go(func() {
			found := index()
			mu.Lock()
			sessions = append(sessions, found...)
			mu.Unlock()
		})
	}
	wg.Wait()
	indexCache.flush()
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].LastUsed.After(sessions[j].LastUsed)
	})
	return sessions
}

// indexFiles indexes a glob's worth of transcripts at once, reusing what a
// previous run read off any file that has not changed. Nothing means nothing.
func indexFiles(paths []string, index func(string) *Session) []*Session {
	found := make([]*Session, len(paths))
	// A fixed pool, not one goroutine per file: a two-year corpus is tens of
	// thousands of transcripts, and that many stacks before any file is read.
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(max(runtime.NumCPU(), 2), len(paths)) {
		wg.Go(func() {
			for i := range work {
				found[i] = cachedIndex(paths[i], index)
			}
		})
	}
	for i := range paths {
		work <- i
	}
	close(work)
	wg.Wait()

	out := make([]*Session, 0, len(paths))
	for _, s := range found {
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

// cachedIndex indexes one transcript, or hands back what the last run read off
// it. A file we cannot stat is read every time rather than cached wrongly.
func cachedIndex(path string, index func(string) *Session) *Session {
	at, ok := stampOf(path)
	if !ok {
		return index(path)
	}
	if row, hit := indexCache.get(path, at); hit {
		return row.session(path)
	}
	s := index(path)
	indexCache.put(path, at, rowOf(s))
	return s
}

func rowOf(s *Session) *indexRow {
	if s == nil {
		return nil
	}
	return &indexRow{
		Agent: s.Agent, ID: s.ID, CWD: s.CWD, Branch: s.Branch, Model: s.Model,
		Title: s.Title, Status: s.Status, Subagent: s.Subagent,
		FirstPrompt: s.FirstPrompt, LastUsed: s.LastUsed,
	}
}

func (r *indexRow) session(path string) *Session {
	if r == nil {
		return nil
	}
	return &Session{
		Agent: r.Agent, ID: r.ID, Path: path, CWD: r.CWD, Branch: r.Branch,
		Model: r.Model, Title: r.Title, Status: r.Status, Subagent: r.Subagent,
		FirstPrompt: r.FirstPrompt, LastUsed: r.LastUsed,
	}
}

// LoadCopy parses into a copy, leaving the original untouched, so a UI can load
// details off its render goroutine without racing the renderer.
func LoadCopy(s *Session) (*Session, error) {
	if s == nil {
		return nil, nil
	}
	dup := *s
	dup.Loaded = false
	dup.LoadErr = nil
	dup.Refs = nil
	err := Load(&dup)
	return &dup, err
}

// Load parses the whole transcript. Idempotent: parsed at most once, and a
// failure is remembered so the UI does not retry on every keystroke.
func Load(s *Session) error {
	if s == nil || s.Loaded {
		if s != nil {
			return s.LoadErr
		}
		return nil
	}
	load, ok := loaders[s.Agent]
	if !ok {
		s.Loaded = true
		s.LoadErr = fmt.Errorf("no loader for agent %q", s.Agent)
		return s.LoadErr
	}
	s.Prompts = nil
	s.Tools = nil
	s.Refs = nil
	s.Charges = nil
	s.TurnsDone = nil
	s.ToolFailures = nil
	s.Retries = 0
	s.Compactions = 0
	s.LoadErr = load(s)
	if s.LoadErr == nil {
		s.inferTurns()
		s.Refs = References(s)
	}
	s.Loaded = true
	return s.LoadErr
}

package estimate

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/transcripts"
)

// sameRun is how close a backfilled build's start must be to a recorded one's
// for the two to be the same build.
const sameRun = 2 * time.Minute

// Backfill is every past build the transcripts show. A build is a fresh
// agent, so its prompt is the session's first and the cheap index finds it.
func Backfill(sessions []*transcripts.Session) []Run {
	var builds []*transcripts.Session
	for _, s := range sessions {
		if s != nil && s.Subagent == "" && strings.HasPrefix(s.FirstPrompt, design.BuildOpening) {
			builds = append(builds, s)
		}
	}
	var out []Run
	for _, loaded := range transcripts.LoadAll(builds) {
		if loaded == nil {
			continue
		}
		if run, ok := FromSession(loaded); ok {
			out = append(out, run)
		}
	}
	return out
}

// FromSession is the build a loaded transcript records: from its prompt to the
// last request before anything else was asked.
func FromSession(s *transcripts.Session) (Run, bool) {
	start := -1
	for i, p := range s.Prompts {
		if strings.HasPrefix(p.Text, design.BuildOpening) {
			start = i
			break
		}
	}
	if start < 0 {
		return Run{}, false
	}
	began := s.Prompts[start].At
	var until time.Time
	if start+1 < len(s.Prompts) {
		until = s.Prompts[start+1].At
	}
	run := Run{Kind: KindBuild, Repo: Repo(s.CWD), Started: began}
	spent := transcripts.Usage{CostKnown: true}
	for _, c := range s.Charges {
		if c.At.Before(began) || (!until.IsZero() && !c.At.Before(until)) {
			continue
		}
		if run.Model == "" {
			run.Model = c.Model
		}
		run.Finished = c.At
		spent.Input += c.Usage.Input
		spent.Output += c.Usage.Output
		spent.CacheRead += c.Usage.CacheRead
		spent.CacheWrite += c.Usage.CacheWrite
		spent.Reasoning += c.Usage.Reasoning
		cost, ok := transcripts.Price(c.Model, c.Usage)
		spent.CostUSD += cost
		spent.CostKnown = spent.CostKnown && ok
	}
	if run.Duration() <= 0 {
		return Run{}, false
	}
	run.Usage = spent
	if doc := docOf(s.Prompts[start].Text, s.CWD); doc != "" {
		run.DocWords, run.ChecklistItems = Size(doc)
	}
	return run, true
}

// docOf reads the design a build prompt names, as it stands now: the prompt
// has the path, not the words.
func docOf(prompt, cwd string) string {
	rest := strings.TrimPrefix(prompt, design.BuildOpening)
	// The prompt's whitespace is collapsed, so the path ends at ". - Read".
	path, _, _ := strings.Cut(rest, ". ")
	path = strings.TrimSuffix(path, ".")
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	// Only a regular file, and not much of one: a FIFO would block the read.
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(path) // #nosec G304 -- a design the user built from
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxDoc))
	if err != nil {
		return ""
	}
	return string(data)
}

const maxDoc = 1 << 20

// Merge is the recorded runs plus the backfilled ones they do not already cover.
func Merge(recorded, backfilled []Run) []Run {
	out := append([]Run(nil), recorded...)
	for _, b := range backfilled {
		covered := false
		for _, r := range recorded {
			if r.Kind == b.Kind && r.Repo == b.Repo && absDur(r.Started.Sub(b.Started)) < sameRun {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, b)
		}
	}
	return out
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// historyTTL is how long a read of the history is reused: it scans every
// transcript's index and loads each past build.
const historyTTL = 5 * time.Minute

// History is recorded and backfilled runs, cached so a modal opening does not
// rescan the machine.
type History struct {
	mu         sync.Mutex
	at         time.Time
	recorded   []Run
	backfilled []Run
	runs       []Run

	spentAt    time.Time
	spentSince time.Time
	spentAgent transcripts.Agent
	spent      float64
	spentKnown bool
}

// Runs is the history as of the last read, reading again when it is stale.
// The read happens outside the lock: it scans the machine.
func (h *History) Runs(dir string) []Run {
	h.mu.Lock()
	if !h.at.IsZero() && time.Since(h.at) < historyTTL {
		defer h.mu.Unlock()
		return h.runs
	}
	h.mu.Unlock()

	recorded, _ := Recorded(dir)
	backfilled := Backfill(transcripts.Scan())

	h.mu.Lock()
	defer h.mu.Unlock()
	h.recorded, h.backfilled, h.at = recorded, backfilled, time.Now()
	h.runs = Merge(h.recorded, h.backfilled)
	return h.runs
}

// Add puts a run just recorded into the cached history.
func (h *History) Add(run Run) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.at.IsZero() {
		h.recorded = append(h.recorded, run)
		h.runs = Merge(h.recorded, h.backfilled)
	}
}

// SpentSince is the priced usage of one agent's requests since a window
// opened, reused for a minute because it loads every transcript that ran in it.
func (h *History) SpentSince(agent transcripts.Agent, since time.Time) (float64, bool) {
	h.mu.Lock()
	if h.spentAgent == agent && h.spentSince.Equal(since) && time.Since(h.spentAt) < time.Minute {
		defer h.mu.Unlock()
		return h.spent, h.spentKnown
	}
	h.mu.Unlock()

	spent, known := transcripts.CostSince(transcripts.Scan(), agent, since)

	h.mu.Lock()
	defer h.mu.Unlock()
	h.spent, h.spentKnown = spent, known
	h.spentAt, h.spentSince, h.spentAgent = time.Now(), since, agent
	return spent, known
}

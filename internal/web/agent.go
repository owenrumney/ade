package web

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/oneshot"
	"github.com/owenrumney/ade/internal/permissions"
	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/settings"
)

// The cold runs ade makes itself. One at a time, beside the agent's terminal.
const (
	kindCommitMessage = "commit-message"
	kindConvert       = "convert"
	kindReview        = "review"
)

// sessionNone is what --session takes to mean "start fresh".
const sessionNone = "none"

// idleTimeout stops a wedged run once it has spoken; runCeiling stops any run.
const (
	idleTimeout   = 30 * time.Minute
	runCeiling    = 4 * time.Hour
	watchInterval = 30 * time.Second
)

// agentRunner is the checkout's agent: the CLI in its terminal, and the one
// cold run ade may have going beside it.
type agentRunner struct {
	session  string
	current  string
	bin      string
	dir      string
	model    string
	provider string

	// The address and token the agent's own process reaches this server by.
	delegateURL   string
	delegateToken string
	hookBase      string
	hookToken     string
	rules         *permissions.Store

	mu        sync.Mutex
	running   bool
	takenOver bool
	tuiID     string
	badge     string
	cancel    context.CancelFunc
	state     RunState
	lastEvent time.Time
	spoken    bool
	stopped   string
	doing     string
	beats     activity
	title     string

	onIdle    func(started time.Time)
	onStep    func(text string)
	onSession func(from, to string)

	mem *runlog.Log
}

type RunState struct {
	Running   bool      `json:"running"`
	Kind      string    `json:"kind,omitempty"`
	Error     string    `json:"error,omitempty"`
	Session   string    `json:"session,omitempty"`
	Current   string    `json:"current,omitempty"`
	Model     string    `json:"model,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`

	Activity    []int     `json:"activity,omitempty"`
	LastEventAt time.Time `json:"lastEventAt,omitzero"`
	Doing       string    `json:"doing,omitempty"`

	// TakenOver is the agent's own interface holding the conversation, and
	// Terminal what it is doing: working, needs, idle or ended.
	TakenOver bool   `json:"takenOver,omitempty"`
	Terminal  string `json:"terminal,omitempty"`

	SideRuns []SideRun `json:"sideRuns,omitempty"`
	Title    string    `json:"title,omitempty"`
}

func newAgentRunner(dir, session, bin, provider, model string, mem *runlog.Log) *agentRunner {
	if strings.TrimSpace(provider) == "" && mem != nil && mem.Provider() != "" {
		provider = mem.Provider()
	}
	provider = normalizeProvider(provider)
	if provider == "codex" && strings.HasPrefix(model, "claude-") {
		model = ""
	}
	a := &agentRunner{dir: dir, session: session, bin: bin, provider: provider, model: model, mem: mem}
	a.adopt()
	return a
}

// adopt picks up the conversation this checkout was last in, when none was named.
func (a *agentRunner) adopt() {
	named := a.session != "" && a.session != sessionNone
	switch a.session {
	case sessionNone:
		a.session = ""
	case "":
		a.session = a.mem.Session()
	}
	a.current = a.session
	a.title = settings.Titles()[a.current]
	// One named on the command line is otherwise only in memory, and the scan
	// lists no session for a checkout whose record holds none.
	if named && a.session != a.mem.Session() {
		if err := a.mem.SetAgentSession(a.session, a.provider); err != nil {
			slog.Debug("record the conversation ade was started on", "err", err)
		}
	}
}

// note records the conversation the CLI named, so the next ade here resumes it.
func (a *agentRunner) note(id string) {
	if id == "" {
		return
	}
	a.mu.Lock()
	mem, provider, was := a.mem, a.provider, a.current
	changed := was != id
	if changed {
		a.current = id
		a.title = settings.Titles()[id]
	}
	onSession := a.onSession
	a.mu.Unlock()

	if err := mem.SetAgentSession(id, provider); err != nil {
		slog.Debug("record the agent session", "err", err)
	}
	if changed && onSession != nil {
		onSession(was, id)
	}
}

func (a *agentRunner) Current() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

func (a *agentRunner) setTitle(title string) {
	a.mu.Lock()
	a.title = title
	a.mu.Unlock()
}

// denyRules is the deny list as Claude Code reads it, minus what it could not.
func denyRules(rules *permissions.Store) []string {
	if rules == nil {
		return nil
	}
	file, _ := rules.Rules()
	var out []string
	for _, rule := range permissions.Wrapped(file.Deny) {
		if rule.Valid() {
			out = append(out, string(rule))
		}
	}
	return out
}

// spokenIn is whether the CLI has written a transcript for this conversation.
func spokenIn(dir, provider, session string) bool {
	home, err := os.UserHomeDir()
	if err != nil || session == "" {
		return false
	}
	pattern := filepath.Join(home, ".codex", "sessions", "*", "*", "*", "*"+session+"*.jsonl")
	if provider != "codex" {
		pattern = filepath.Join(home, ".claude", "projects", "*", session+".jsonl")
	}
	found, _ := filepath.Glob(pattern)
	for _, path := range found {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return true
		}
	}
	return false
}

func validProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "" || provider == "claude" || provider == "codex" || provider == "pi"
}

// normalizeProvider folds "pi" in with "codex", which older records still say.
func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex", "pi":
		return "codex"
	}
	return "claude"
}

func (a *agentRunner) State() RunState {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.state
	out.Running = a.running
	out.Session = a.session
	out.Current = a.current
	out.Model = a.model
	out.Provider = a.provider
	out.TakenOver = a.takenOver
	out.LastEventAt = a.lastEvent
	out.Doing = a.doing
	if a.running {
		out.Activity = a.beats.series(time.Now())
	}
	out.Title = a.title
	return out
}

// begin claims the one cold run and returns the context it must use.
func (a *agentRunner) begin(parent context.Context, kind string) (context.Context, bool) {
	a.mu.Lock()
	if a.running || (a.takenOver && !cold(kind)) {
		a.mu.Unlock()
		return nil, false
	}
	ctx, cancel := context.WithCancel(parent)
	now := time.Now()
	a.running = true
	a.cancel = cancel
	a.stopped = ""
	a.lastEvent = now
	a.doing = ""
	a.spoken = false
	a.state = RunState{Kind: kind, StartedAt: now}
	a.mu.Unlock()

	go func() {
		defer func() { _ = recover() }()
		a.watch(ctx, cancel)
	}()
	return ctx, true
}

// cold is a run that needs no conversation, so a terminal agent does not block it.
func cold(kind string) bool { return kind == kindReview || kind == kindCommitMessage }

// watch ends a run that has gone quiet, or one that will not stop.
func (a *agentRunner) watch(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		a.mu.Lock()
		idle, total := time.Since(a.lastEvent), time.Since(a.state.StartedAt)
		var reason string
		switch {
		case a.spoken && idle >= idleTimeout:
			reason = fmt.Sprintf("the run went quiet for %s, so it was stopped", idle.Round(time.Minute))
		case total >= runCeiling:
			reason = fmt.Sprintf("the run passed %s and was stopped", runCeiling)
		}
		if reason != "" {
			a.stopped = reason
		}
		a.mu.Unlock()
		if reason != "" {
			slog.Warn("stopping the run", "why", reason)
			cancel()
			return
		}
	}
}

func (a *agentRunner) beat() ([]int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return nil, false
	}
	return a.beats.series(time.Now()), true
}

// touch records a sign of life, which keeps the watchdog off the run.
func (a *agentRunner) touch() {
	now := time.Now()
	a.mu.Lock()
	a.lastEvent = now
	a.spoken = true
	a.beats.note(now)
	a.mu.Unlock()
}

func (a *agentRunner) Cancel() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.cancel == nil {
		return false
	}
	a.stopped = "you cancelled the run"
	a.cancel()
	return true
}

func (a *agentRunner) finish(err error) {
	a.mu.Lock()
	a.running = false
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if err != nil {
		// When ade stopped it, ade's reason is the true one.
		a.state.Error = err.Error()
		if a.stopped != "" {
			a.state.Error = a.stopped
		}
	}
	a.stopped = ""
	idle, started := a.onIdle, a.state.StartedAt
	a.mu.Unlock()

	// After the lock, so whatever runs next can claim the runner.
	if idle != nil {
		idle(started)
	}
}

// recoverRun keeps a panic inside a run from taking the server with it.
func (s *Server) recoverRun(kind string) {
	r := recover()
	if r == nil {
		return
	}
	err := fmt.Errorf("the %s run panicked: %v", kind, r)
	slog.Error("agent run panicked", "kind", kind, "panic", r, "stack", string(debug.Stack()))

	s.agent.finish(err)
	s.agent.step(err.Error())
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// step says what the run is doing, for the status line.
func (a *agentRunner) step(text string) {
	a.touch()
	a.mu.Lock()
	a.doing = text
	onStep := a.onStep
	a.mu.Unlock()
	if onStep != nil {
		onStep(text)
	}
}

func (a *agentRunner) setDelegate(url, token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delegateURL, a.delegateToken = url, token
}

// setHook is where the CLI's hooks report, and opens the deny list they obey.
func (a *agentRunner) setHook(base, token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hookBase, a.hookToken = strings.TrimSuffix(base, "/"), token
	if a.rules != nil {
		return
	}
	rules, err := permissions.Open()
	if err != nil {
		slog.Warn("read your permission rules", "err", err)
		return
	}
	a.rules = rules
}

func (a *agentRunner) rulesStore() *permissions.Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rules
}

func (s *Server) hookAuthorized(r *http.Request) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return s.hookToken != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.hookToken)) == 1
}

// handleCancel stops the cold run and every named agent beside it.
func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	stopped := s.agent.Cancel()
	sides := s.stopSides()
	if !stopped && sides == 0 {
		writeError(w, http.StatusBadRequest, "nothing is running")
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleStopReview ends the named agents and leaves the session's own agent
// to its turn: the Diff's stop is for the reviewers it shows.
func (s *Server) handleStopReview(w http.ResponseWriter, _ *http.Request) {
	if s.stopSides() == 0 {
		writeError(w, http.StatusBadRequest, "no reviewer is running")
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// runOneShot is oneshot.Run, a variable so a test can answer for claude.
var runOneShot = oneshot.Run

// oneShot is one cold claude run, whichever CLI the session's terminal holds.
func (s *Server) oneShot(ctx context.Context, opts oneshot.Opts) (*oneshot.Turn, error) {
	return runOneShot(ctx, opts)
}

// tell says one line about a run: in the log and on the status line.
func (s *Server) tell(text string) {
	slog.Info("run", "said", text)
	s.agent.step(text)
}

// onAgentEvent is a cold run's sign of life.
func (s *Server) onAgentEvent(ev oneshot.Event) { s.onSubagentEvent("", ev) }

// holdIdle runs fn unless a run is in flight, and none begins while it runs.
func (a *agentRunner) holdIdle(fn func()) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return false
	}
	fn()
	return true
}

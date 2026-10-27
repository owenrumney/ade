package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/bridge/codex"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/wiki"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gate"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/models"
	"github.com/owenrumney/ade/internal/permissions"
	"github.com/owenrumney/ade/internal/resolve"
	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/statedir"
	"github.com/owenrumney/ade/internal/transcripts"
)

// The run kinds that can be stacked up. A button pressed twice still means
// once, so the ones here that are buttons stack at most one entry.
const (
	kindChat          = "chat"
	kindCommitMessage = "commit-message"
	kindConvert       = "convert"
	kindSwitchModel   = "switch-model"
	kindFixChecks     = "fix-checks"
	kindReview        = "review"
	kindResolve       = "resolve"
	kindDraftReply    = "draft-reply"
	kindPRCreate      = "pr-create"
	kindQuiz          = "quiz"
	kindSeed          = "design-seed"
	kindFold          = "design-fold"
)

// kindWake is a turn the agent started itself, on a background task's word.
// Nothing sends it, so nothing queues it.
const kindWake = "wake"

// sessionNone is what --session takes to mean "start fresh", now that leaving it
// unset means "whatever this checkout was last in". Without it a session that
// has gone bad could only be escaped by deleting a file by hand.
const sessionNone = "none"

// idleTimeout stops a wedged agent holding the single-run lock forever. It
// counts silence rather than elapsed time: a run still reporting tool calls is
// doing its job however long it takes, and a total budget kills the long builds
// this exists to protect.
//
// It only applies once the agent has said something. Silence is a poor proxy
// for stuck and a terrible one before the first event: a resumed session
// replays its whole transcript before it speaks, and stream-json emits nothing
// during a long reply that calls no tools. Both look identical to wedged, and
// killing them destroys real work. Until the first event, only runCeiling
// applies.
const idleTimeout = 30 * time.Minute

// runCeiling is the backstop for a run that keeps talking and never finishes.
const runCeiling = 4 * time.Hour

// watchInterval is how often the two are checked.
const watchInterval = 30 * time.Second

// agentRunner owns the active agent subprocess and serialises runs through it.
type agentRunner struct {
	// session is what this process resumed, and current the conversation the
	// agent is actually in: a fresh agent names itself on its first event, and
	// a resumed one can fork to a new id. current is what the conversation is
	// stored under.
	session       string
	current       string
	bin           string
	codexBin      string
	dir           string
	model         string
	provider      string
	delegateURL   string
	delegateToken string

	// hookURL and hookToken are where the agent's PreToolUse hook reaches this
	// server. rules is what that hook decides with.
	hookURL   string
	hookToken string
	rules     *permissions.Store

	// sessionBytes is how much transcript a resume replays, measured once.
	sessionBytes int64

	// contextTokens is the window in use as of the last assistant message,
	// and contextParts an estimate of what is filling it, from the transcript.
	contextTokens int
	contextParts  []transcripts.ContextPart

	// autoCompact is passed through to Claude Code, which does the
	// compacting; ade only asks for it and watches the window shrink.
	autoCompact string

	// cold is which cold container names are taken, under boxMu.
	cold [sandbox.ColdSlots]bool

	// sandbox runs claude in a container with the checkout mounted. gitDir
	// is the repository's, mounted too when dir is a worktree; runtime is
	// found on first use and kept.
	sandbox bool
	// workspace is the repository's sandbox file; own is this session's, laid
	// over it: the other repositories it was started across.
	workspace string
	own       string
	gitDir    string
	runtime   sandbox.Runtime
	// docsDir answers where design documents live now. A function because it
	// moves: a document is adopted mid-session, and a bind changes checkout.
	docsDir func() string
	// outbox is where a picture goes to reach the reply. A function for the
	// same reason, and because it is created on the way out.
	outbox func() string
	// alsoOn opens this server where a box can reach it; nil in a test.
	alsoOn func(host string)

	// boxMu guards building a spec and the cold names. Every run rewrites the
	// one box its container mounts, so two at once would read a half-written login.
	boxMu sync.Mutex

	// projectPrompt is the project's instructions and memory. A function
	// because both are edited while the server runs.
	projectPrompt func(withMemory bool) string

	// skill is a Claude Code skill's body, prepended to the main chat's system
	// prompt by `ade investigate`. The named agents keep their own briefs.
	skill string

	// title and cost are the session's, read from settings and its
	// transcript; refreshed as runs end.
	title string
	cost  float64

	spawn spawnWatch

	// wakePoll is how often a wake asks whether the process is still there.
	wakePoll time.Duration

	mu      sync.Mutex
	agent   bridge.Agent
	running bool
	// takenOver is the agent's own TUI holding the conversation instead of
	// the bridge. One process on a transcript at a time.
	takenOver bool
	cancel    context.CancelFunc
	state     RunState

	// lastEvent is when the agent last said anything, which is what the
	// watchdog measures, and spoken is whether it ever has. stopped is why we
	// ended the run, when we did.
	lastEvent time.Time
	spoken    bool
	stopped   string
	doing     string
	touched   []string
	// repeats is how many times running doing has been the call in hand with
	// nothing else between, and repeatedAt when the first of them was.
	repeats    int
	repeatedAt time.Time

	toldBusy bool

	// beats counts events per interval, for the run-is-working sparkline.
	beats activity

	// onIdle runs when a run ends, so whatever is waiting can take its turn.
	// onStep hears every progress line, so it can be written into the
	// conversation, and onSession hears the conversation change id, so what
	// was written under the old one can follow.
	onIdle    func(started time.Time)
	onStep    func(text string)
	onSession func(from, to string)
	// onWakeStart and onWake bracket a turn nobody sent, so what it cost and
	// what it changed are settled as any other turn's are.
	onWakeStart func()
	onWake      func(err error)

	// onLimits hears the subscription windows Codex reports as a turn runs.
	onLimits func(limits.Limits)

	onAsk func()

	// mem is this checkout's memory of the agent, written through on every
	// event so a restart comes back to the same conversation rather than to a
	// stranger.
	mem *runlog.Log

	// wikiDraft is ADE's stable ownership key for the private wiki mount.
	wikiDraft string

	// wiki is the shared wiki, or nil when no remote is configured.
	wiki *wiki.Store
}

// RunState is what the browser needs to show progress, including after a reload.
type RunState struct {
	Running bool   `json:"running"`
	Kind    string `json:"kind,omitempty"`
	Error   string `json:"error,omitempty"`
	Session string `json:"session,omitempty"`
	// Current is the conversation the agent is actually in. A fresh session
	// names itself mid-run and never fills Session, but it is switchable.
	Current   string    `json:"current,omitempty"`
	Model     string    `json:"model,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`

	// SessionBytes is the size of the transcript being replayed, which dominates
	// how long the first run takes.
	SessionBytes int64 `json:"sessionBytes,omitempty"`

	// Activity is events per interval, oldest first; a flat tail is a hang.
	Activity []int `json:"activity,omitempty"`

	// LastEventAt outlives the run, so an idle session can still say how
	// long it has been quiet.
	LastEventAt time.Time `json:"lastEventAt,omitzero"`

	// Sandbox is whether the agent runs in a container.
	Sandbox bool `json:"sandbox,omitempty"`

	// Queued is how many turns are stacked behind this run, and QueuedThreads
	// names the comments among them waiting to be rewritten.
	Queued        int      `json:"queued,omitempty"`
	QueuedThreads []string `json:"queuedThreads,omitempty"`

	// Waiting is that queue in order.
	Waiting []Waiting `json:"waiting,omitempty"`

	// Welded is the first prompt of a chat turn made of several, while it runs.
	Welded string `json:"welded,omitempty"`
	// Doing is the latest tool call as one line; Touched the files edited so far.
	Doing   string   `json:"doing,omitempty"`
	Touched []string `json:"touched,omitempty"`

	// Children is the sub-agents at work, the background ones after the
	// run that called them has ended.
	Children []Child `json:"children,omitempty"`

	// Repeats is how many times over Doing has been the call in hand, and
	// RepeatedAt when that started. A run going round is not a run working.
	Repeats    int       `json:"repeats,omitempty"`
	RepeatedAt time.Time `json:"repeatedAt,omitzero"`

	// Finished is how the last turn ended, until the next one starts.
	Finished *Finished `json:"finished,omitempty"`

	// Injectable is whether a message sent now would reach the run in flight,
	// rather than wait behind it. Only the shared session takes one mid-run; a
	// cold review has nowhere to put it.
	Injectable bool `json:"injectable,omitempty"`

	// Asking is everything waiting on an answer. A turn with one open is
	// paused, not working, and reads that way.
	Asking []bridge.Ask `json:"asking,omitempty"`

	// Free is the session running without being asked. Said out loud because
	// a gate nobody can see turned off is one nobody turns back on.
	Free bool `json:"free,omitempty"`

	// TakenOver is the agent's own interface holding the conversation, which
	// is why no turn can start here until it is given back.
	TakenOver bool `json:"takenOver,omitempty"`

	// SideRuns is the named agents at work beside the main one.
	SideRuns []SideRun `json:"sideRuns,omitempty"`

	// ContextTokens is how much of the window the last request used, and
	// ContextWindow how big the window is, so the page can say how close to
	// full — and to compaction — the session is.
	ContextTokens int `json:"contextTokens,omitempty"`
	ContextWindow int `json:"contextWindow,omitempty"`

	// ContextParts is roughly what is holding that window, biggest first, so
	// a long session can say where it went. An estimate off the transcript:
	// the agent reports one total and never itemises it.
	ContextParts []transcripts.ContextPart `json:"contextParts,omitempty"`

	// Title is the name the session was given after its first exchange, and
	// CostUSD what its transcript adds up to so far, when the model is one
	// the pricing table knows.
	Title   string  `json:"title,omitempty"`
	CostUSD float64 `json:"costUsd,omitempty"`
}

func newAgentRunner(dir, session, bin, model, autoCompact string, box sandboxConfig, mem *runlog.Log) *agentRunner {
	return newProviderAgentRunner(dir, session, bin, "", "claude", model, autoCompact, box, mem)
}

func newProviderAgentRunner(dir, session, bin, codexBin, provider, model, autoCompact string, box sandboxConfig, mem *runlog.Log) *agentRunner {
	if strings.TrimSpace(provider) == "" && mem != nil && mem.Provider() != "" {
		provider = mem.Provider()
	}
	provider = normalizeProvider(provider)
	if provider == "codex" {
		if strings.HasPrefix(model, "claude-") {
			model = "gpt-5.6-terra"
		}
	}
	a := &agentRunner{dir: dir, session: session, bin: bin, codexBin: codexBin, provider: provider, model: model, autoCompact: autoCompact,
		sandbox: box.On, gitDir: box.GitDir, workspace: box.Workspace, own: box.Own, wakePoll: watchInterval, mem: mem}
	// The remembered choice beats the flag default, which is on, but not a
	// --sandbox the user typed: a checkout stays out across a restart, not a flag.
	if on, known := mem.Sandbox(); known && !box.Explicit {
		a.sandbox = on
	}
	a.adopt()
	if mem != nil {
		a.wikiDraft = mem.WikiDraft()
	}
	return a
}

func (a *agentRunner) ensureWikiDraft(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.wikiDraft != "" {
		return
	}
	a.wikiDraft = id
	if err := a.mem.SetWikiDraft(id); err != nil {
		slog.Debug("record wiki draft", "err", err)
	}
}

// adopt picks up the conversation this checkout was last in, when one was not
// named outright, and measures whatever session we end up on. The caller holds
// the lock.
//
// A remembered id whose transcript has since been deleted would make every run
// fail on --resume, so it is only taken when the transcript is still there.
func (a *agentRunner) adopt() {
	if a.session == sessionNone {
		a.session, a.sessionBytes = "", 0
		a.current = ""
		return
	}
	if a.session == "" {
		if remembered := a.mem.Session(); remembered != "" {
			if size := providerTranscriptSize(a.dir, a.provider, remembered); size > 0 {
				a.session, a.sessionBytes = remembered, size
				a.current = remembered
				return
			}
		}
	}
	a.sessionBytes = providerTranscriptSize(a.dir, a.provider, a.session)
	a.current = a.session
}

// note records the conversation the agent is actually in, so the next ade in
// this checkout resumes it. A fresh session only names itself once it is
// running, which is why this is read off the stream rather than known upfront.
// Resuming can fork to a new id too, and the fork is what there is to go back
// to.
//
// Only the long-lived subprocess reports here. The one-shot runs — commit
// message, review, outgoing — have sessions of their own, and remembering one
// of those would resume the wrong conversation.
//
// session is left alone: it is what this process resumed, which the page says
// out loud, and a fresh agent that has just named itself did not resume
// anything. current moves, and the conversation moves with it.
func (a *agentRunner) note(id string) {
	if id == "" {
		return
	}
	a.mu.Lock()
	mem := a.mem
	provider := a.provider
	was := a.current
	changed := was != id
	if changed {
		a.current = id
	}
	onSession := a.onSession
	a.mu.Unlock()

	if err := mem.SetAgentSession(id, provider); err != nil {
		slog.Debug("record the agent session", "err", err)
	}
	if changed && onSession != nil {
		onSession(was, id)
	}
	if changed {
		a.lookupTitle()
	}
}

// Current is the conversation the agent is in, or empty for a fresh one that
// has not spoken yet.
func (a *agentRunner) Current() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

// denyRules is the deny list as Claude Code reads it, minus what it could not.
func denyRules(rules *permissions.Store) []string {
	if rules == nil {
		return nil
	}
	file, _ := rules.Rules()
	var out []string
	for _, rule := range file.Deny {
		if rule.Valid() {
			out = append(out, string(rule))
		}
	}
	return out
}

// forkable is what /btw forks from; not ok before the first event or on Codex.
func (a *agentRunner) forkable() (session, model, dir string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != "claude" || a.current == "" {
		return "", "", "", false
	}
	return a.current, a.model, a.dir, true
}

// transcript is the session's file on disk, or empty before it has one.
func (a *agentRunner) transcript() string {
	session := a.Current()
	a.mu.Lock()
	dir, provider := a.dir, a.provider
	a.mu.Unlock()
	return providerTranscriptPath(dir, provider, session)
}

// transcriptSize measures the session Claude Code would replay on resume, so the
// browser can warn before a multi-megabyte session is resumed.
func transcriptSize(dir, session string) int64 {
	return providerTranscriptSize(dir, "claude", session)
}

func providerTranscriptSize(dir, provider, session string) int64 {
	path := providerTranscriptPath(dir, provider, session)
	if path == "" {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// transcriptPath finds the session's transcript: under this directory's
// project first, then wherever it lives if ade was launched elsewhere.
// Empty when there is none.
func providerTranscriptPath(dir, provider, session string) string {
	if normalizeProvider(provider) == "codex" {
		for _, found := range transcripts.Scan() {
			if found.Agent == transcripts.AgentCodex && found.CWD == dir && found.ID == session {
				return found.Path
			}
		}
		return ""
	}
	return transcriptPath(dir, session)
}

func transcriptPath(dir, session string) string {
	if session == "" {
		return ""
	}
	if path := transcripts.ClaudeTranscript(dir, session); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	// Resume does not mind the directory it runs in, and a transcript never moves.
	return transcripts.ClaudeAnywhere(session)
}

// State returns a copy of the current run state.
func validProvider(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return provider == "" || provider == "claude" || provider == "codex" || provider == "pi"
}

// normalizeProvider folds "pi" in with "codex". It is the spelling a script
// and a run log written before the rename both still carry.
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
	out.SessionBytes = a.sessionBytes
	out.Injectable = a.running && a.agent != nil && a.agent.Busy()
	if a.running {
		out.Asking = a.asking()
	}
	out.Free = a.rules != nil && a.rules.IsFree()
	out.TakenOver = a.takenOver
	// The watchdog's clock, not the ring's: a new run resets it, so a fresh
	// run does not report the silence of the one before it.
	out.LastEventAt = a.lastEvent
	out.Doing = a.doing
	out.Touched = append([]string(nil), a.touched...)
	if a.running {
		out.Repeats, out.RepeatedAt = a.repeats, a.repeatedAt
	}
	if a.running {
		out.Activity = a.beats.series(time.Now())
	}
	out.ContextTokens = a.contextTokens
	out.ContextParts = a.contextParts
	out.ContextWindow = contextWindow(a.provider, a.model)
	// A default model can be the long-context one, which nothing in its
	// name says; a window the usage has outgrown is the bigger window.
	if out.ContextTokens > out.ContextWindow {
		out.ContextWindow = 1_000_000
	}
	// A session adopted rather than named here has its title only on disk.
	if a.title == "" && a.session != "" {
		a.title = settings.Titles()[a.session]
	}
	out.Title = a.title
	out.CostUSD = a.cost
	out.Sandbox = a.sandbox
	return out
}

// Finished is how a turn ended. A run that is over leaves the badge blank,
// so what it did goes unsaid unless something says it.
type Finished struct {
	// Files is how many the turn edited, Asking whether it ended by putting
	// something to you rather than reporting.
	Files  int       `json:"files,omitempty"`
	Asking bool      `json:"asking,omitempty"`
	At     time.Time `json:"at"`
}

// finished records how the turn ended. begin clears it: it describes the last
// turn, and the next one is about to make it wrong.
func (a *agentRunner) finished(f *Finished) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Finished = f
}

// sandboxConfig is how a runner is told to run the agent in a container.
type sandboxConfig struct {
	// Explicit is --sandbox given on the command line rather than defaulted,
	// which outranks what this checkout chose last time.
	On        bool
	Explicit  bool
	GitDir    string
	Workspace string
	// Own is this session's own mounts file, under its worktree's git dir.
	Own string
}

// sandboxOn is whether this session's agent runs in a container.
func (a *agentRunner) sandboxOn() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sandbox
}

// setSandbox turns the container on or off for the next process. The one
// running, if any, is dropped so the change takes; the conversation is not.
func (a *agentRunner) setSandbox(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sandbox == on {
		return
	}
	a.sandbox = on
	// Remembered so a restart does not put the agent back in a container the
	// user took it out of. A failed write costs the memory, not the change.
	if err := a.mem.SetSandbox(on); err != nil {
		slog.Warn("could not remember the sandbox choice", "err", err)
	}
	a.dropKeepingSession()
}

// unbox takes the agent out of the container for this session only. Unlike
// setSandbox it does not remember, so the next ade here is boxed as before.
func (a *agentRunner) unbox() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.sandbox {
		return
	}
	a.sandbox = false
	a.dropKeepingSession()
}

// ensureRuntime finds the container runtime once and keeps it. Turning the
// sandbox on fails here rather than on the next turn.
func (a *agentRunner) ensureRuntime(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runtime != nil {
		return nil
	}
	rt, err := detectRuntime(ctx)
	if err != nil {
		return err
	}
	a.runtime = rt
	a.reachable(rt)
	return nil
}

// reachable is here, not at listen time: detecting the runtime costs a probe.
func (a *agentRunner) reachable(rt sandbox.Runtime) {
	apple, ok := rt.(sandbox.Apple)
	if !ok || apple.Host == "" || a.alsoOn == nil {
		return
	}
	a.alsoOn(apple.Host)
}

// restartSandbox drops a running container so the next turn starts one with
// the workspace as it is now, and reports whether there was one to drop. On
// the host there is nothing to do.
//
// The caller says so in the conversation: a container going out from under a
// running agent is otherwise invisible, and the note in ensure that announces
// the new one does not arrive until the next turn.
func (a *agentRunner) restartSandbox() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.sandbox {
		return false
	}
	// Whether or not a process was up, the session must survive; only a
	// container that was actually running is worth announcing.
	running := a.agent != nil
	a.dropKeepingSession()
	return running
}

// detectRuntime and sandboxLogin are sandbox.Detect and
// sandbox.Credentials, variables so a test never probes a real runtime or
// reads a real login.
var (
	detectRuntime = sandbox.Detect
	sandboxLogin  = sandbox.Credentials
	sandboxSync   = sandbox.SyncCredentials
	codexSync     = sandbox.SyncCodexCredentials
	codexFreshen  = codex.Freshen
)

// freshenCodexLogin refreshes the login the next codex process will read.
// Best effort: a failure leaves the stamp alone, so the CLI may do it instead.
func freshenCodexLogin(ctx context.Context, sandboxed bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := sandbox.CodexHostAuth(home)
	if sandboxed {
		path = sandbox.CodexAuth(home)
	}
	if err := codexFreshen(ctx, path); err != nil {
		slog.Warn("refresh the codex login before the run", "path", path, "err", err)
	}
}

// loginSyncGrace is how long the end of a turn waits on the host's store.
const loginSyncGrace = 5 * time.Second

// sweepOrphan takes away a container left over from a run this server does not
// know about.
//
// Stopping a run stops its container: cmd.Cancel calls the runtime's stop. What
// that cannot cover is the server not getting to run it — killed, crashed, the
// machine slept. The container survives with the checkout mounted read-write and
// an agent possibly still writing to it, which is worse than untidy, and it
// holds the name every later run needs.
//
// Best effort, once, at startup. Anything wrong here is not a reason to refuse
// to serve the checkout.
func (a *agentRunner) sweepOrphan(ctx context.Context) {
	if !a.sandbox {
		return
	}
	rt, err := detectRuntime(ctx)
	if err != nil {
		slog.Debug("no runtime to sweep orphans with", "err", err)
		return
	}
	sandbox.Remove(ctx, rt, sandbox.ContainerName(a.dir))
	for slot := 0; slot < sandbox.ColdSlots; slot++ {
		sandbox.Remove(ctx, rt, sandbox.ColdContainerName(a.dir, slot))
	}
}

// takeColdSlot claims a free cold container name, or false when all are in
// use: a slot handed out twice is a `rm -f` on a run that is still going.
func (a *agentRunner) takeColdSlot() (int, bool) {
	a.boxMu.Lock()
	defer a.boxMu.Unlock()
	for slot := 0; slot < sandbox.ColdSlots; slot++ {
		if !a.cold[slot] {
			a.cold[slot] = true
			return slot, true
		}
	}
	return 0, false
}

func (a *agentRunner) freeColdSlot(slot int) {
	a.boxMu.Lock()
	defer a.boxMu.Unlock()
	a.cold[slot] = false
}

// boxInputs is what a spec is built from, read under a.mu and used without it.
type boxInputs struct {
	rt        sandbox.Runtime
	provider  string
	dir       string
	gitDir    string
	workspace string
	own       string
	docs      string
	wiki      *wiki.Store
	wikiDraft string
	delegate  string
	// delegateToken reaches ade's own tools from inside the box.
	delegateToken string
}

// sandboxOpts is the conversation's own provider; claudeSandboxOpts is claude
// whatever the conversation runs on. Neither caller holds a.mu.
func (a *agentRunner) sandboxOpts(ctx context.Context) (*sandbox.Spec, sandbox.Runtime, error) {
	return a.boxed(ctx, true)
}

func (a *agentRunner) claudeSandboxOpts(ctx context.Context) (*sandbox.Spec, sandbox.Runtime, error) {
	return a.boxed(ctx, false)
}

func (a *agentRunner) boxed(ctx context.Context, own bool) (*sandbox.Spec, sandbox.Runtime, error) {
	a.mu.Lock()
	in, err := a.boxFor(ctx, own)
	a.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	return a.specFor(ctx, in)
}

// boxFor reads the runner's side of a spec, finding the runtime once. own is
// the conversation's provider rather than claude. a.mu is held.
func (a *agentRunner) boxFor(ctx context.Context, own bool) (boxInputs, error) {
	if a.runtime == nil {
		rt, err := detectRuntime(ctx)
		if err != nil {
			return boxInputs{}, err
		}
		a.runtime = rt
	}
	in := boxInputs{rt: a.runtime, dir: a.dir, gitDir: a.gitDir, workspace: a.workspace, own: a.own,
		wiki: a.wiki, wikiDraft: a.wikiDraft, delegate: a.delegateURL, delegateToken: a.delegateToken}
	if own {
		in.provider = a.provider
	}
	if a.docsDir != nil {
		in.docs = a.docsDir()
	}
	return in, nil
}

// specFor builds it: login copied once, spec each time. boxMu serialises the
// writes, since every run rewrites the one box the containers mount.
func (a *agentRunner) specFor(ctx context.Context, in boxInputs) (*sandbox.Spec, sandbox.Runtime, error) {
	a.boxMu.Lock()
	defer a.boxMu.Unlock()
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil, err
	}
	ws, err := sandbox.LoadWorkspace(in.workspace)
	if err != nil {
		return nil, nil, err
	}
	own, err := sandbox.LoadWorkspace(in.own)
	if err != nil {
		return nil, nil, err
	}
	// The agent can reach this file, so it is checked on the way in and not
	// only where it is written.
	own, err = sandbox.CheckWorkspace(home, own)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := settings.Load()
	if err != nil {
		return nil, nil, err
	}
	ws = own.Under(ws.Under(cfg.Sandbox))
	// The worker's session token, and only a worker's: an exported GH_TOKEN in
	// the shell that started ade locally is the user's, not the session's.
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" && httpx.Wide() && ws.Env["GH_TOKEN"] == "" {
		ws.Env["GH_TOKEN"] = token
	}
	if ws.Env["NODE_AUTH_TOKEN"] == "" {
		ws.Env["NODE_AUTH_TOKEN"] = cfg.GitHubPackagesToken
	}
	fillGitHubTokens(ctx, ws.Env)
	// Inside the container the git directory is mounted at its own path, so
	// the outbox is the same string on both sides.
	if dir := a.outboxDir(); dir != "" {
		ws.Env[bridge.OutboxEnv] = dir
	}
	extra := append(ws.Mounts(), gitDirsFor(ctx, own)...)
	var servers map[string]json.RawMessage
	if draft, ok := wikiDraftFor(ctx, in.wiki, in.wikiDraft, in.dir); ok {
		mounts, env := wikiMounts(draft)
		extra = append(extra, mounts...)
		for key, value := range env {
			ws.Env[key] = value
		}
		servers = wikiServers(workerBase(in.rt, in.delegate), draft)
	}
	// Not optional for a box: it carries no toolchain, so this is the only
	// way a run in one builds or tests anything.
	if entry := checksServer(workerBase(in.rt, in.delegate), in.delegateToken); entry != nil {
		if servers == nil {
			servers = map[string]json.RawMessage{}
		}
		servers[ChecksServerName] = entry
	}
	if in.provider == "codex" {
		spec, err := sandbox.Codex{Home: home, Root: in.dir, GitDir: in.gitDir, Docs: in.docs, Extra: extra, Env: ws.Env}.Spec()
		if err != nil {
			return nil, nil, err
		}
		return &spec, in.rt, nil
	}
	creds, err := sandboxLogin(ctx, home)
	if err != nil {
		return nil, nil, err
	}
	spec, err := claudeSpec(in.rt, in, home, ws, extra, servers, creds)
	if err != nil {
		return nil, nil, err
	}
	return &spec, in.rt, nil
}

// gitDirsFor mounts the repository git directory behind each of this session's
// other worktrees: their .git is a file pointing at it, so git needs it too.
func gitDirsFor(ctx context.Context, own sandbox.Workspace) []sandbox.Mount {
	var out []sandbox.Mount
	for _, p := range own.Paths {
		dir, err := gitdiff.CommonGitDir(ctx, p.Path)
		if err != nil || dir == "" || strings.HasPrefix(dir, p.Path+string(os.PathSeparator)) {
			continue
		}
		out = append(out, sandbox.Mount{Host: dir, Guest: dir, RW: true})
	}
	return out
}

// claudeSpec builds the container for one runtime; the caller may try another.
func claudeSpec(rt sandbox.Runtime, in boxInputs, home string, ws sandbox.Workspace, extra []sandbox.Mount, servers map[string]json.RawMessage, creds string) (sandbox.Spec, error) {
	mcpHost := ""
	switch at := rt.(type) {
	case sandbox.Docker:
		if at.Network == "" {
			mcpHost = "host.docker.internal"
		}
	case sandbox.Apple:
		mcpHost = at.Host
	}
	_, apple := rt.(sandbox.Apple)
	return sandbox.Claude{Home: home, Root: in.dir, GitDir: in.gitDir, Docs: in.docs,
		Extra: extra, Env: ws.Env, MCPHost: mcpHost, Servers: servers, MakeMountPoints: apple}.Spec(filepath.Dir(creds))
}

// hostGitHubToken prefers ADE_GITHUB_TOKEN, a scoped token, over the whole gh login.
var hostGitHubToken = func(ctx context.Context) string {
	if token := strings.TrimSpace(os.Getenv("ADE_GITHUB_TOKEN")); token != "" {
		return token
	}
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fillGitHubTokens gives every GitHub token the box has no value for the host's.
func fillGitHubTokens(ctx context.Context, env map[string]string) {
	var host string
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "NODE_AUTH_TOKEN"} {
		if env[key] != "" {
			continue
		}
		if host == "" {
			if host = hostGitHubToken(ctx); host == "" {
				return
			}
		}
		env[key] = host
	}
}

// setTitle records the session's name on the runner, for the state.
func (a *agentRunner) setTitle(title string) {
	a.mu.Lock()
	a.title = title
	a.mu.Unlock()
}

// lookupTitle reads the name a session already has, if any, when the
// runner lands on it.
func (a *agentRunner) lookupTitle() {
	a.mu.Lock()
	id := a.current
	a.mu.Unlock()
	title := ""
	if id != "" {
		title = settings.Titles()[id]
	}
	a.setTitle(title)
}

// tallyCost re-reads the session's transcript for what it has cost and what
// is filling its context window. A transcript is megabytes on a long day, so
// this is for after a run, in the background, not on every poll.
func (a *agentRunner) tallyCost() {
	a.mu.Lock()
	id, dir := a.current, a.dir
	a.mu.Unlock()
	path := transcriptPath(dir, id)
	if path == "" {
		return
	}
	parts := transcripts.ContextBreakdown(path)
	a.mu.Lock()
	a.contextParts = parts
	a.mu.Unlock()

	sess := transcripts.ClaudeSession(path)
	if sess == nil || transcripts.Load(sess) != nil || !sess.Usage.CostKnown {
		return
	}
	a.mu.Lock()
	a.cost = sess.Usage.CostUSD
	a.mu.Unlock()
}

// noteUsage keeps the latest window size the agent reported. Only assistant
// messages count: a result repeats the turn's total, which is the same.
func (a *agentRunner) noteUsage(ev bridge.Event) {
	if ev.Usage == nil || ev.Type != "assistant" {
		return
	}
	now := ev.Usage.Context()
	a.mu.Lock()
	was := a.contextTokens
	a.contextTokens = now
	a.mu.Unlock()
	// A window that halves between one message and the next was compacted —
	// by Claude Code on its own, or because you asked. Said once, in the
	// conversation, so the drop on the meter has an explanation.
	if was >= compactNoticeFloor && now < was/2 {
		a.step(fmt.Sprintf("compacted the conversation: %dk → %dk tokens", was/1000, now/1000))
	}
}

// compactNoticeFloor is the smallest window worth announcing a compaction
// of; the first messages of a session halve and double for other reasons.
const compactNoticeFloor = 20_000

// contextWindow is the model's window: the catalogue's figure when it names
// one, since the picker prints that number and the meter must agree with it.
// Otherwise Claude Code's 200k, and a "[1m]" suffix asks for the long variant.
func contextWindow(provider, model string) int {
	if strings.Contains(strings.ToLower(model), "[1m]") {
		return 1_000_000
	}
	if window := models.Window(strings.TrimPrefix(model, "openai-codex/")); window > 0 {
		return window
	}
	if provider == "codex" {
		return 272_000
	}
	return 200_000
}

// begin claims the runner and returns the context the run must use, so Cancel can
// interrupt it. It reports false when a run is already in flight.
func (a *agentRunner) begin(parent context.Context, kind string) (context.Context, bool) {
	a.mu.Lock()
	// A takeover holds the transcript, so a turn here would be a second
	// process writing it.
	if a.running || a.takenOver {
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
	a.repeats, a.repeatedAt = 0, time.Time{}
	a.touched = nil
	a.spoken = false
	a.toldBusy = false
	a.state = RunState{Kind: kind, StartedAt: now}
	a.mu.Unlock()

	go a.watch(ctx, cancel)
	go a.watchSpawn(ctx, cancel)
	return ctx, true
}

// holdIdle runs fn unless a run is in flight, holding the runner's lock across
// it so one cannot begin while it runs. fn must not call back into the runner.
func (a *agentRunner) holdIdle(fn func()) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return false
	}
	fn()
	return true
}

// watch ends a run that has gone quiet, or one that will not stop. Cancelling
// with a reason means the browser is told what happened rather than being shown
// "context deadline exceeded".
func (a *agentRunner) watch(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.Lock()
			if len(a.asking()) > 0 {
				a.lastEvent = time.Now()
			}
			idle := time.Since(a.lastEvent)
			total := time.Since(a.state.StartedAt)
			spoken := a.spoken
			var reason string
			switch {
			case spoken && idle >= idleTimeout:
				reason = fmt.Sprintf("the agent went quiet for %s, so the run was stopped. It may have been working; raise idleTimeout if this was wrong.", idle.Round(time.Minute))
			case total >= runCeiling:
				reason = fmt.Sprintf("the run passed %s and was stopped", runCeiling)
			}
			if reason != "" {
				a.stopped = reason
			}
			a.mu.Unlock()

			if reason != "" {
				slog.Warn("stopping the agent", "why", reason)
				cancel()
				return
			}
		}
	}
}

// wake is a turn the agent started itself: Send sees none of it, so the run
// opens on its first event and closes on its result here. True on the result.
func (a *agentRunner) wake(ctx context.Context, ev bridge.Event) bool {
	if ev.Type == "result" {
		var err error
		if ev.IsError {
			err = bridge.TurnError(ev)
		}
		a.endWake(time.Time{}, err)
		return true
	}
	runCtx, ok := a.begin(ctx, kindWake)
	if !ok {
		return false
	}
	a.mu.Lock()
	started, agent, poll, began := a.state.StartedAt, a.agent, a.wakePoll, a.onWakeStart
	a.mu.Unlock()
	if began != nil {
		began()
	}
	a.step("carrying on by itself")
	go a.watchWake(runCtx, started, agent, poll)
	return false
}

// watchWake ends a wake nothing else can: a stop only cancels the context,
// and the process going is only seen by asking.
func (a *agentRunner) watchWake(ctx context.Context, started time.Time, agent bridge.Agent, poll time.Duration) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			a.endWake(started, nil)
			return
		case <-ticker.C:
			if agent != nil && agent.Dead() {
				a.endWake(started, errors.New("the agent exited mid-turn"))
				return
			}
		}
	}
}

// endWake finishes the wake begun at started, if it is still the run in
// flight; zero means whichever wake is. The reason it was stopped outranks err.
func (a *agentRunner) endWake(started time.Time, err error) {
	a.mu.Lock()
	mine := a.running && a.state.Kind == kindWake && (started.IsZero() || a.state.StartedAt.Equal(started))
	if mine && a.stopped != "" {
		err = errors.New(a.stopped)
	}
	wake := a.onWake
	a.mu.Unlock()
	if !mine {
		return
	}
	a.finish(err)
	if wake != nil {
		wake(err)
	}
}

// beat is the activity series while a run is in flight, for the badge. False
// when nothing is running: a finished run's tail is flat by definition.
func (a *agentRunner) beat() ([]int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return nil, false
	}
	return a.beats.series(time.Now()), true
}

// touch records that the agent is still saying things, which is what keeps the
// watchdog off it.
func (a *agentRunner) noteTool(line string) {
	a.mu.Lock()
	if line == a.doing {
		a.repeats++
	} else {
		a.doing, a.repeats, a.repeatedAt = line, 1, time.Now()
	}
	a.mu.Unlock()
}

func (a *agentRunner) noteTouched(rel string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, had := range a.touched {
		if had == rel {
			return
		}
	}
	if len(a.touched) < 100 {
		a.touched = append(a.touched, rel)
	}
}

func (a *agentRunner) touch() {
	now := time.Now()
	a.mu.Lock()
	a.lastEvent = now
	a.spoken = true
	a.beats.note(now)
	a.mu.Unlock()
}

// kind is the run in flight, or empty.
func (a *agentRunner) kind() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return ""
	}
	return a.state.Kind
}

// why turns a run's failure into something worth reading. When we cancelled it
// we know the reason; "context canceled" on its own tells nobody anything.
func (a *agentRunner) why(err error) string {
	if err == nil {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped != "" {
		return a.stopped
	}
	return err.Error()
}

// Cancel interrupts the in-flight run, if any.
func (a *agentRunner) Cancel() bool { return a.stopFor("you cancelled the run") }

// stopFor is Cancel with the reason the conversation will give.
func (a *agentRunner) stopFor(reason string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.cancel == nil {
		return false
	}
	a.stopped = reason
	a.cancel()
	return true
}

// stopWhile is stopFor with fn run first under the runner's lock, so what fn
// queues is there before the stop lets the drain look. fn must not call back in.
func (a *agentRunner) stopWhile(reason string, fn func() bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.cancel == nil || !fn() {
		return false
	}
	a.stopped = reason
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
		// Whatever the subprocess reported, if we were the ones who stopped it
		// then our reason is the true one and the useful one.
		a.state.Error = err.Error()
		if a.stopped != "" {
			a.state.Error = a.stopped
		}
		// A rotated login needs a new process, not a new conversation.
		if staleLogin(a.state.Error) {
			a.dropKeepingSession()
			a.state.Error += " — the agent was restarted; send it again"
		}
	}
	a.stopped = ""
	idle := a.onIdle
	started := a.state.StartedAt
	boxed := a.sandbox
	provider := a.provider
	a.mu.Unlock()

	// The turn may have refreshed the login inside the container, which voids
	// the host's. Writing it back here beats the next run, which is too late.
	if boxed {
		if home, err := os.UserHomeDir(); err == nil {
			if provider == "codex" {
				codexSync(home)
			} else {
				// Bounded: a locked keychain blocks `security`, and nothing takes the
				// next turn until this returns.
				ctx, cancel := context.WithTimeout(context.Background(), loginSyncGrace)
				sandboxSync(ctx, home)
				cancel()
			}
		}
	}

	// After the lock, so whatever takes the next turn can claim the runner.
	if idle != nil {
		idle(started)
	}
}

// recoverRun keeps a panic inside an agent run from taking the server with it.
// net/http recovers panics in its own handler goroutines, but these are ours, so
// an unhandled one would end the process and lose the session.
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

// step reports progress. It keeps the watchdog off the run and hands the line
// on to be written into the conversation, which is where progress is read.
func (a *agentRunner) step(text string) {
	now := time.Now()
	a.mu.Lock()
	a.lastEvent = now
	a.spoken = true
	a.beats.note(now)
	onStep := a.onStep
	a.mu.Unlock()
	if onStep != nil {
		onStep(text)
	}
}

// relocate moves the runner to another checkout. The subprocess is bound to its
// working directory, so it is killed rather than reused, and the session does
// not travel with it: a transcript recorded elsewhere would not resume here.
//
// The checkout being moved to has a memory of its own, though, so if the agent
// has worked there before it is picked up where it was left.
func (a *agentRunner) relocate(dir string, mem *runlog.Log) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
	a.dir = dir
	a.session = ""
	a.sessionBytes = 0
	a.mem = mem
	a.adopt()
}

// switchModel moves the conversation onto another model. The subprocess is
// bound to the model it started with, so it goes; the next run resumes the
// same session on the new one. The caller must not be mid-run.
func (a *agentRunner) switchModel(model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
	if a.current != "" {
		a.session = a.current
	}
	a.model = model
}

// modelName is the model the main agent is on, for tagging what it says.
func (a *agentRunner) modelName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// reset starts a fresh conversation, on a different model if one is named.
// The subprocess goes with the old one: it is bound to the session it resumed.
// The caller must not be mid-run.
func (a *agentRunner) reset(model string) {
	a.resetProvider(a.provider, model)
}

func (a *agentRunner) resetProvider(provider, model string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
	a.session, a.current, a.sessionBytes, a.contextTokens = "", "", 0, 0
	a.title, a.cost = "", 0
	a.provider = normalizeProvider(provider)
	if model != "" {
		a.model = model
	} else if a.provider == "codex" {
		a.model = "gpt-5.6-terra"
	}
}

// resume switches to another of this checkout's conversations, for the
// launcher handing a session to a server already running here. The caller
// must not be mid-run.
func (a *agentRunner) resume(id string) {
	a.resumeProvider(a.provider, a.model, id)
}

func (a *agentRunner) resumeProvider(provider, model, id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
	a.provider = normalizeProvider(provider)
	a.model = model
	a.session, a.current = id, id
	a.contextTokens, a.cost = 0, 0
	a.title = settings.Titles()[id]
	a.sessionBytes = providerTranscriptSize(a.dir, a.provider, id)
	if err := a.mem.SetAgentSession(id, a.provider); err != nil {
		slog.Debug("record the agent session", "err", err)
	}
}

// dropKeepingSession kills the subprocess so the next turn starts a container
// built from the workspace as it is now. Docker fixes a container's mounts when
// it is created, so a mount that changed means a new container and a new
// process; the conversation is not meant to go with it. The session the process
// was in only reaches the resume through session, and a conversation that
// started fresh here has its id in current alone. The caller holds the lock.
func (a *agentRunner) dropKeepingSession() {
	a.drop()
	if a.current != "" {
		a.session = a.current
		a.sessionBytes = providerTranscriptSize(a.dir, a.provider, a.session)
	}
}

// staleLogin is the agent reporting a login it cannot refresh, which happens
// when something else refreshed the same account and rotated its token.
func staleLogin(reason string) bool {
	reason = strings.ToLower(reason)
	return strings.Contains(reason, "oauth session expired") ||
		strings.Contains(reason, "could not be refreshed") ||
		(strings.Contains(reason, "authenticate") && strings.Contains(reason, "expired"))
}

// drop kills the subprocess, if there is one. The caller holds the lock.
func (a *agentRunner) drop() {
	if a.agent != nil {
		_ = a.agent.Close()
		a.agent = nil
	}
}

// gone is whether there is no agent process left to have started anything.
func (a *agentRunner) gone() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.agent == nil || a.agent.Dead()
}

// tellBusy is true once, when the run first becomes injectable: nothing else
// marks it, and the page sat on the "no" it read between begin and Send.
func (a *agentRunner) tellBusy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.toldBusy || !a.running || a.agent == nil || !a.agent.Busy() {
		return false
	}
	a.toldBusy = true
	return true
}

// inject hands a message to the run in flight, and reports whether it could.
// Only a turn on the shared session can take one: a cold reviewer has no
// stdin to speak of, and nothing that is not running can be spoken to.
func (a *agentRunner) inject(message string, images []bridge.Image) bool {
	a.mu.Lock()
	agent := a.agent
	running := a.running
	a.mu.Unlock()
	if !running || agent == nil || !agent.Busy() {
		return false
	}
	if err := agent.Inject(message, images...); err != nil {
		slog.Debug("inject into the running turn", "err", err)
		return false
	}
	return true
}

// answer replies to a question the run in flight is paused on.
func (a *agentRunner) answer(toolUseID string, answers map[string]string) error {
	a.mu.Lock()
	asker, ok := a.agent.(bridge.Asker)
	running := a.running
	a.mu.Unlock()
	if !running || !ok {
		return errors.New("nothing is waiting for an answer")
	}
	return asker.Answer(toolUseID, answers)
}

// decide allows or denies a permission request the run is paused on.
func (a *agentRunner) decide(toolUseID string, allow bool, remember, reason string) error {
	a.mu.Lock()
	asker, ok := a.agent.(bridge.Asker)
	running := a.running
	a.mu.Unlock()
	if !running || !ok {
		return errors.New("nothing is waiting on a decision")
	}
	return asker.Decide(toolUseID, allow, remember, reason)
}

// asking is the questions the run is paused on. Called with a.mu held.
func (a *agentRunner) asking() []bridge.Ask {
	asker, ok := a.agent.(bridge.Asker)
	if !ok {
		return nil
	}
	return asker.Asking()
}

// permissionMode is what a run may do unasked. Bypass only behind the gate,
// which is a command that always answers; an http hook would not have.
func (a *agentRunner) permissionMode() string {
	if a.gated() {
		return "bypassPermissions"
	}
	return "acceptEdits"
}

func (a *agentRunner) hookFor() string {
	if !a.gated() {
		return ""
	}
	return a.hookURL
}

// hookEnv is what `ade hook` reads to reach this server. Called with a.mu held.
func (a *agentRunner) hookEnv() []string {
	if !a.gated() {
		return nil
	}
	return []string{gate.URLVar + "=" + a.hookURL, gate.TokenVar + "=" + a.hookToken}
}

// hookBin is the ade the gate runs as: this one, not another build on PATH.
func (a *agentRunner) hookBin() string {
	if !a.gated() {
		return ""
	}
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return self
}

// gated is whether ade is in front of this run's tool calls. A boxed run is not:
// an http hook to the host is a private address, which Claude Code refuses.
func (a *agentRunner) gated() bool {
	return a.hookURL != "" && !a.sandbox
}

// Close kills the subprocess if one was started.
func (a *agentRunner) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop()
}

// delegateEnv is what `ade said` in the agent's own shell needs to reach this
// server. In a container the host is reachable under another name.
func (a *agentRunner) delegateEnv() []string {
	if a.delegateURL == "" || a.delegateToken == "" {
		return nil
	}
	url := rehost(a.delegateURL, a.containerHost())
	return []string{"ADE_DELEGATE_URL=" + url, "ADE_DELEGATE_TOKEN=" + a.delegateToken}
}

// outboxDir is the outbox, or "" when this server has none.
func (a *agentRunner) outboxDir() string {
	if a.outbox == nil {
		return ""
	}
	return a.outbox()
}

// containerHost is what the host answers to from inside the container, or ""
// when there is none. Apple has no name for it, so it is the gateway address.
func (a *agentRunner) containerHost() string {
	if !a.sandbox {
		return ""
	}
	switch at := a.runtime.(type) {
	case sandbox.Docker:
		if at.Network != "" {
			return workerHost(at)
		}
		return "host.docker.internal"
	case sandbox.Apple:
		return at.Host
	}
	return ""
}

// workerHost is the worker's ade by its container name on the hub's network, or
// "" off a worker: in the hub's namespace the host's name means nothing.
func workerHost(rt sandbox.Runtime) string {
	if docker, ok := rt.(sandbox.Docker); ok && docker.Network != "" {
		return strings.TrimSpace(os.Getenv("ADE_WORKER_NAME"))
	}
	return ""
}

// rehost is addr with its loopback host replaced by host, or addr when host is "".
func rehost(addr, host string) string {
	if host == "" {
		return addr
	}
	addr = strings.Replace(addr, "127.0.0.1", host, 1)
	return strings.Replace(addr, "localhost", host, 1)
}

// wikiMCP is the wiki tool as Codex reaches it: this server's address, under
// the name a container knows it by, with the draft id as the token.
// codexMCPs is every tool a codex run gets, addressed from where it runs. A
// box has no toolchain, so without the checks server it cannot build at all.
func (a *agentRunner) codexMCPs(boxed bool) []bridge.MCP {
	base := a.delegateURL
	if base == "" {
		return nil
	}
	if boxed {
		base = rehost(base, a.containerHost())
	}
	base = strings.TrimSuffix(base, "/")
	var out []bridge.MCP
	if a.wiki != nil && a.wiki.Enabled() && a.wikiDraft != "" {
		out = append(out, bridge.MCP{Name: "ade_wiki", URL: base + wiki.MCPPath, Token: a.wikiDraft})
	}
	if a.delegateToken != "" {
		out = append(out, bridge.MCP{Name: ChecksServerName, URL: base + ChecksMCPPath, Token: a.delegateToken})
	}
	return out
}

// setDelegate configures the private endpoint the agent's own tools use.
func (a *agentRunner) setDelegate(url, token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.delegateURL, a.delegateToken = url, token
}

// setHook configures the gate: where the PreToolUse hook POSTs, and the rules
// it is answered from. A rule file ade cannot read leaves the hook asking.
func (a *agentRunner) setHook(base, token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hookURL, a.hookToken = strings.TrimSuffix(base, "/")+HookPath, token
	if a.rules == nil {
		rules, err := permissions.Open()
		if err != nil {
			slog.Warn("read your permission rules", "err", err)
			return
		}
		a.rules = rules
	}
}

// gate is the running agent as the hook's decider, when there is one.
func (a *agentRunner) gate() (bridge.Gate, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running || a.agent == nil {
		return nil, false
	}
	gate, ok := a.agent.(bridge.Gate)
	return gate, ok
}

// rulesStore is the rule set, or nil before a server has opened one.
func (a *agentRunner) rulesStore() *permissions.Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.rules
}

// ensure lazily starts the subprocess, so nothing is spawned until a run needs
// it. There is no terminal to answer a permission prompt on, so the mode decides
// what an unattended run may do: acceptEdits writes files but gates the shell,
// bypassPermissions gates nothing.
//
// A subprocess that has died is replaced rather than talked to: the next run
// resumes the conversation it was in, which is what the memory is for.
func (a *agentRunner) ensure(ctx context.Context, onEvent func(bridge.Event)) (bridge.Agent, error) {
	a.mu.Lock()
	if a.agent != nil && a.agent.Dead() {
		a.drop()
		if a.current != "" {
			a.session = a.current
		}
	}
	if a.agent != nil {
		a.mu.Unlock()
		return a.agent, nil
	}
	opts := bridge.Opts{
		Bin:            a.bin,
		Dir:            a.dir,
		Resume:         a.session,
		Model:          a.model,
		AutoCompact:    a.autoCompact,
		PermissionMode: a.permissionMode(),
		SystemPrompt:   a.mainPrompt(),
		Env:            append(a.delegateEnv(), a.hookEnv()...),
		Rules:          a.rules,
		Hook:           a.hookFor(),
		HookBin:        a.hookBin(),
		HookToken:      a.hookToken,
		DenyRules:      denyRules(a.rules),
		OnEvent: func(ev bridge.Event) {
			a.note(ev.SessionID)
			a.noteUsage(ev)
			if a.wake(ctx, ev) {
				return
			}
			onEvent(ev)
		},
		// Read when it fires: onAsk is set after this closure is built.
		OnAsk: func() {
			if a.onAsk != nil {
				a.onAsk()
			}
		},
	}
	runtime, held := "", false
	if a.sandbox {
		// a.mu is already held, so the spec is built from here rather than
		// through sandboxOpts, which takes it.
		in, err := a.boxFor(ctx, true)
		if err != nil {
			a.mu.Unlock()
			return nil, fmt.Errorf("sandbox: %w", err)
		}
		spec, rt, err := a.specFor(ctx, in)
		if err != nil {
			a.mu.Unlock()
			return nil, fmt.Errorf("sandbox: %w", err)
		}
		opts.Sandbox, opts.Runtime = spec, rt
		runtime, held = rt.Name(), sandbox.HasImage(ctx, rt, spec.Image)
	}
	if dir := a.outboxDir(); dir != "" {
		opts.Env = append(opts.Env, bridge.OutboxEnv+"="+dir)
	}
	// The launcher holds the connection, so an agent under this process no
	// longer inherits it by pid and has to be told which instance to join.
	if home, err := os.UserHomeDir(); err == nil {
		opts.Env = append(opts.Env, agentbus.EnvInstance+"="+agentbus.InstanceID(statedir.Home(home)))
	}
	// Host runs only: a container has no agentbus binary and no tailnet, and
	// ade merges the wiki into its box rather than passing a config.
	if opts.Sandbox == nil {
		config, pairs := agentMCP(ctx, a)
		opts.MCPConfig = config
		opts.Env = append(opts.Env, pairs...)
	}
	var (
		agent bridge.Agent
		err   error
	)
	if a.provider == "codex" {
		freshenCodexLogin(ctx, opts.Sandbox != nil)
		codexOpts := bridge.CodexOpts{Bin: a.codexBin, Dir: a.dir, Resume: a.session, Model: a.model, SystemPrompt: bridge.MainSystemPrompt(a.mainPrompt()), Env: opts.Env, OnEvent: opts.OnEvent, OnLimits: a.onLimits}
		if opts.Sandbox != nil {
			codexOpts.Sandbox, codexOpts.Runtime = opts.Sandbox, opts.Runtime
			codexOpts.OTLPHost = a.containerHost()
		}
		codexOpts.MCPs = a.codexMCPs(opts.Sandbox != nil)
		agent, err = bridge.NewCodex(ctx, codexOpts)
	} else {
		agent, err = bridge.NewClaude(ctx, opts)
	}
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	a.agent = agent
	replaying := a.sessionBytes
	provider := a.provider
	a.mu.Unlock()

	// A resume replays the transcript once, when the process starts, and that
	// is a long silence with nothing to show for it. Said here, and only here:
	// every later turn on the same process replays nothing.
	if runtime != "" {
		a.step(containerStep(provider, runtime, held))
	}
	if replaying > 0 {
		a.step(fmt.Sprintf("resuming a %.1fMB session transcript", float64(replaying)/(1<<20)))
	}
	return agent, nil
}

// containerStep is what to say as the agent starts. Said before the container
// has proved it can run, so a missing image is a fetch and not a claim.
func containerStep(provider, runtime string, held bool) string {
	if held {
		return provider + " is running in a container, via " + runtime
	}
	return provider + " is fetching the sandbox image, via " + runtime +
		" — if it fails, build it here with make sandbox-image"
}

/* HTTP */

// resolveBlocked is why applying comments would land them on the wrong tree,
// or empty. A pull request that is not checked out is the case.
func (s *Server) resolveBlocked() string {
	if pr := s.space().pr; pr != nil {
		return pr.ResolveBlocked
	}
	return ""
}

func (s *Server) handleAgentResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ThreadIDs []string `json:"threadIds"`
	}
	if !decode(w, r, &req) {
		return
	}

	if why := s.resolveBlocked(); why != "" {
		writeError(w, http.StatusConflict, why)
		return
	}
	threads := s.threadsToResolve(req.ThreadIDs)
	if len(threads) == 0 {
		writeError(w, http.StatusBadRequest, "no open comments to resolve")
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindResolve)
	if !ok {
		// The ids, not the threads: what they say when the run comes is what
		// gets applied, as a convert already works.
		ids := make([]string, 0, len(threads))
		for _, t := range threads {
			ids = append(ids, t.ID)
		}
		s.queueBehind(w, queued{kind: kindResolve, threads: ids})
		return
	}

	s.spawn(func() { s.runResolve(runCtx, threads) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "threads": len(threads)})
}

// handleCancelSide stops one named agent and leaves everything else running.
func (s *Server) handleCancelSide(w http.ResponseWriter, r *http.Request) {
	if !s.stopSide(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "that agent is not running")
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// cancelLine says what the cancel took with it, so a stop that ended more
// than the turn says so rather than leaving you to notice.
func cancelLine(dropped, kids int) string {
	var also []string
	if dropped > 0 {
		also = append(also, fmt.Sprintf("dropped %d queued message(s)", dropped))
	}
	if kids > 0 {
		also = append(also, fmt.Sprintf("stopped %d sub-agent(s)", kids))
	}
	if len(also) == 0 {
		return "cancelled"
	}
	return "cancelled, and " + strings.Join(also, ", ")
}

func (s *Server) handleCancel(w http.ResponseWriter, _ *http.Request) {
	// Dropped first: the run ending drains the queue, and cancelling to advance
	// to the next thing you typed is not what the button means.
	dropped := s.dropStack()
	stopped := s.stopSides()
	checks := s.stopChecks()
	// A sub-agent runs inside the turn being cancelled, so it goes with it. A
	// backgrounded command is its own process and outlives the turn by design.
	kids := s.children.stop(childAgent)
	if !s.agent.Cancel() {
		if dropped > 0 || stopped > 0 || checks || kids > 0 {
			s.hub.Broadcast("agent", nil)
			s.hub.Broadcast("state", nil)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusBadRequest, "nothing is running")
		return
	}
	s.agent.step(cancelLine(dropped, kids))
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// threadsToResolve picks the requested open threads, or every open thread when
// no ids are given.
func (s *Server) threadsToResolve(ids []string) []comments.Thread {
	open := s.space().store.Unresolved()
	if len(ids) == 0 {
		// Resolve all means what is still waiting. A thread the resolver
		// declined is answered, and re-sending it asks the same question again.
		var waiting []comments.Thread
		for _, t := range open {
			if !t.Answered() {
				waiting = append(waiting, t)
			}
		}
		return waiting
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	var out []comments.Thread
	for _, t := range open {
		if wanted[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// runResolve sends the threads to the agent, then records what it did.
func (s *Server) runResolve(ctx context.Context, threads []comments.Thread) {
	defer s.recoverRun("resolve")

	s.beginRunTurn(fmt.Sprintf("Fix %d review comment(s).", len(threads)))
	err := s.resolveOnce(ctx, threads)
	s.endRun(err)
	if err != nil {
		slog.Warn("resolve failed", "err", err)
	}
	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after resolve", "err", refreshErr)
	}
	s.settleTurn()
	s.spawn(func() { s.capturePreview() })
	s.spawn(func() { s.attachOutbox() })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) resolveOnce(ctx context.Context, threads []comments.Thread) error {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	// Its own process, and no tool that fetches or shells out: this run acts
	// on what a comment says.
	s.agent.step(fmt.Sprintf("sending %d comment(s) to %s", len(threads), s.agent.State().Provider))
	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:            s.agent.bin,
		Dir:            s.space().diffRoot(),
		Model:          s.agent.model,
		Prompt:         resolve.BuildPrompt(diff, threads),
		Tools:          resolve.Tools(),
		AllowedTools:   resolve.Tools(),
		SystemPrompt:   s.sideProject(),
		PermissionMode: "acceptEdits",
		NoMCP:          true,
		OnEvent:        s.onAgentEvent,
	})
	if err != nil {
		return err
	}

	outcomes, parseErr := resolve.ParseOutcomes(turn.Text)
	if parseErr != nil {
		// The turn may still have edited files, so keep its reply rather than
		// discard it, and leave every thread open for the user to judge.
		s.agent.step("no structured outcome block; recording the reply verbatim")
		for _, thread := range threads {
			if _, err := s.space().store.Reply(thread.ID, comments.AuthorAgent, turn.Text); err != nil {
				slog.Debug("reply after unparsed resolve", "err", err)
			}
		}
		return nil
	}

	s.applyOutcomes(threads, outcomes)
	return nil
}

// applyOutcomes records the agent's note on each thread, resolving only those it
// says it actually fixed.
func (s *Server) applyOutcomes(threads []comments.Thread, outcomes []resolve.Outcome) {
	w := s.space()
	byID := make(map[string]resolve.Outcome, len(outcomes))
	for _, o := range outcomes {
		byID[o.ID] = o
	}

	for _, thread := range threads {
		outcome, ok := byID[thread.ID]
		if !ok {
			s.agent.step(fmt.Sprintf("%s:%d — no verdict reported, left open", thread.File, thread.Line))
			continue
		}

		note := strings.TrimSpace(outcome.Note)
		if note == "" {
			note = outcome.Status
		}
		if _, err := w.store.ReplyOutcome(thread.ID, comments.AuthorAgent, note, outcome.Status); err != nil {
			slog.Debug("reply with outcome", "err", err)
			continue
		}
		if outcome.Resolves() {
			if _, err := w.store.SetResolved(thread.ID, true); err != nil {
				slog.Debug("resolve thread", "err", err)
			}
		}
		s.agent.step(fmt.Sprintf("%s:%d — %s", thread.File, thread.Line, outcome.Status))
	}
}

// toolTarget pulls the most useful identifier out of a tool's arguments.
func toolTarget(input json.RawMessage) string {
	var fields struct {
		FilePath    string `json:"file_path"`
		Path        string `json:"path"`
		Pattern     string `json:"pattern"`
		Command     string `json:"command"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(input, &fields); err != nil {
		return ""
	}
	for _, candidate := range []string{fields.FilePath, fields.Path, fields.Pattern, fields.Description, fields.Command} {
		if candidate != "" {
			return truncate(candidate, 120)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// runOneShot is bridge.OneShot, a variable so a test can answer for claude.
// project is what the repository adds to a system prompt, or nothing when
// there is no store or it is not remembered.
func (a *agentRunner) project(withMemory bool) string {
	if a.projectPrompt == nil {
		return ""
	}
	return a.projectPrompt(withMemory)
}

// mainPrompt is the main chat's addition: its skill, then the project's notes.
func (a *agentRunner) mainPrompt() string {
	return withProject(a.skill, a.project(true))
}

// setSkill takes effect on the next subprocess; the caller drops the running one.
func (a *agentRunner) setSkill(prompt string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.skill = strings.TrimSpace(prompt)
}

var runOneShot = bridge.OneShot

func (s *Server) oneShot(ctx context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
	if s.agent.State().Provider == "codex" {
		return s.codexOneShot(ctx, opts)
	}
	return s.coldClaude(ctx, opts)
}

func hideOneShot(turn *bridge.Turn) {
	if turn == nil || turn.SessionID == "" {
		return
	}
	if err := settings.Hide(turn.SessionID); err != nil {
		slog.Debug("hide one-shot session", "id", turn.SessionID, "err", err)
	}
}

// coldClaude is one claude run in the container the session gets: a named
// agent is the conversation's trust boundary, not the host's.
func (s *Server) coldClaude(ctx context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
	// A named agent reads its way through the change, and a one-shot that
	// fills its window has nothing to resume from.
	if opts.AutoCompact == "" {
		opts.AutoCompact = s.agent.autoCompact
	}
	if s.agent.State().Sandbox {
		slot, ok := s.agent.takeColdSlot()
		if !ok {
			return nil, fmt.Errorf("sandbox: all %d cold containers are in use", sandbox.ColdSlots)
		}
		defer s.agent.freeColdSlot(slot)
		spec, runtime, err := s.agent.claudeSandboxOpts(ctx)
		if err != nil {
			return nil, fmt.Errorf("sandbox: %w", err)
		}
		spec.Name = sandbox.ColdContainerName(s.space().homeRoot, slot)
		opts.Sandbox, opts.Runtime = spec, runtime
		// --add-dir on a path the box has no mount for fails the run; the
		// design documents have one, so they survive the filter.
		opts.AddDirs = sandbox.Reachable(*spec, opts.AddDirs)
	}
	turn, err := runOneShot(ctx, opts)
	hideOneShot(turn)
	return turn, err
}

func (s *Server) codexOneShot(ctx context.Context, opts bridge.OneShotOpts) (*bridge.Turn, error) {
	state := s.agent.State()
	model := state.Model
	if strings.HasPrefix(opts.Model, "gpt-") || strings.HasPrefix(opts.Model, "openai-codex/") {
		model = opts.Model
	}
	codexOpts := bridge.CodexOpts{
		Bin:          s.agent.codexBin,
		Dir:          opts.Dir,
		Model:        model,
		SystemPrompt: opts.SystemPrompt,
		// Codex has no per-turn tool allowlist. A run given only read tools is
		// a reviewer, and read-only is how Codex says that.
		ReadOnly: readOnlyTools(opts.Tools),
		NoMCP:    opts.NoMCP,
		OnEvent:  opts.OnEvent,
		OnLimits: s.limits.NoteCodex,
	}
	if state.Sandbox {
		slot, ok := s.agent.takeColdSlot()
		if !ok {
			return nil, fmt.Errorf("sandbox: all %d cold containers are in use", sandbox.ColdSlots)
		}
		defer s.agent.freeColdSlot(slot)
		spec, runtime, err := s.agent.sandboxOpts(ctx)
		if err != nil {
			return nil, fmt.Errorf("sandbox: %w", err)
		}
		spec.Name = sandbox.ColdContainerName(s.space().homeRoot, slot)
		codexOpts.Sandbox, codexOpts.Runtime = spec, runtime
		codexOpts.OTLPHost = s.agent.containerHost()
	}
	freshenCodexLogin(ctx, codexOpts.Sandbox != nil)
	turn, err := bridge.CodexOneShot(ctx, codexOpts, opts.Prompt)
	hideOneShot(turn)
	return turn, err
}

// readOnlyTools reports whether a named tool set can write. nil is Claude
// Code's whole set, which can.
func readOnlyTools(tools []string) bool {
	if tools == nil {
		return false
	}
	for _, tool := range tools {
		switch tool {
		case "Edit", "MultiEdit", "Write", "NotebookEdit", "Bash":
			return false
		}
	}
	return true
}

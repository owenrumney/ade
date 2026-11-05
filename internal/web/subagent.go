package web

import (
	"cmp"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/oneshot"
	"github.com/owenrumney/ade/internal/review"
)

// A side run is bounded both ways: how long one may take, and how many may
// be going at once. Each is a process or a stream on the subscription, and
// "@a @b @c @d @e" should not be five of them plus whatever is already up.
const (
	sideTimeout = 20 * time.Minute
	maxSides    = 4
	// coldReserve is the slot a named agent may not take, so a rewrite of
	// seconds does not wait on reviews that run for minutes.
	coldReserve = 1
	// maxHops is how far a hand-off may chain: you call @a, @a calls @b,
	// @b calls @c, and @c's "@d …" is a sentence, not a call. Two agents
	// cannot ping-pong forever, and a repository-defined prompt that tries
	// runs out of road.
	maxHops = 2
)

// SideRun is a named agent at work beside the main one, as the page shows it.
type SideRun struct {
	// ID names this run so one can be stopped without stopping the rest.
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartedAt time.Time `json:"startedAt"`
}

// runSubagent is the kind a named agent's messages are written under, so
// they read as a voice in the conversation rather than as the main agent.
const runSubagent = "subagent"

// sideRun is one in flight: who, since when, how to stop it, and the batch
// it belongs to if any.
type sideRun struct {
	name    string
	started time.Time
	cancel  context.CancelFunc
	batch   string
}

// agentsList is the named agents this checkout can call on: the repository's
// own and the user's.
func (s *Server) agentsList() []agents.Agent {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return agents.Load(s.space().homeRoot, home)
}

// userAgentsList is the user's own named agents, without the repository's.
// A review resolves its reviewers here: agents.LoadAll gives a repository
// agent precedence, so a branch under review could ship
// .claude/agents/security.md and supply its own reviewer. Repo agents are
// clamped read-only, so that buys prompt influence rather than execution —
// but a reviewer that disputes every real finding is enough.
func (s *Server) userAgentsList() []agents.Agent {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return agents.Load("", home)
}

// handleAgents lists the named agents, for the composer to offer on @ and the
// design tab to review with — and the files that were meant to be agents
// and could not be read as one, so a typo is not a vanished agent.
func (s *Server) handleAgents(w http.ResponseWriter, _ *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	list, skipped := agents.LoadAll(s.space().homeRoot, home)
	if list == nil {
		list = []agents.Agent{}
	}
	if skipped == nil {
		skipped = []agents.Skipped{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": list, "skipped": skipped})
}

// sideCall is a named agent and what to say to it. asker is the agent that
// called it, if one did rather than you, and hop how many hand-offs deep
// that is.
type sideCall struct {
	agent  agents.Agent
	prompt string
	asker  string
	parent context.Context

	// batch groups the calls of one review, so its last can settle it.
	batch string
}

func (s *Server) delegateAuthorized(r *http.Request) bool {
	return s.delegateToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Ade-Delegate-Token")), []byte(s.delegateToken)) == 1
}

// startSides starts each call, all or none: the slots are claimed under one
// lock, so two messages arriving together cannot both squeeze under the
// cap. Over it is a note in the conversation, not a run, and false.
func (s *Server) startSides(calls []sideCall) bool {
	return s.startSidesHeld(calls, 0)
}

// startSidesHeld is startSides keeping hold slots back for the hand-offs
// these calls will make. startJobs is all-or-none against maxSides, so N
// reviewers each handing to @commenter would otherwise cap a review at two
// reviewers: the first two claim all four slots and their commenters are
// refused.
func (s *Server) startSidesHeld(calls []sideCall, hold int) bool {
	jobs := make([]sideJob, 0, len(calls))
	for _, call := range calls {
		jobs = append(jobs, sideJob{name: call.agent.Name, batch: call.batch, parent: call.parent, run: func(ctx context.Context) { s.runSubagent(ctx, call) }})
	}
	return s.startJobs(jobs, hold)
}

// sideJob is anything that runs beside the main agent under the side cap:
// a named agent, or a cold one-shot like a rewrite for GitHub.
type sideJob struct {
	name   string
	batch  string
	parent context.Context
	run    func(ctx context.Context)

	// cold is a one-shot with no agent behind it, allowed the reserve.
	cold bool
}

// slotsFor is how many slots this batch may fill; named agents leave one.
func slotsFor(jobs []sideJob) int {
	for _, job := range jobs {
		if !job.cold {
			return maxSides - coldReserve
		}
	}
	return maxSides
}

// startJobs claims a slot for each job under one lock — all or none — and
// runs them. hold is how many further slots the batch keeps back for the
// hand-offs these jobs will make; a job whose batch has slots held draws on
// those rather than on what is free. Over the cap is a note in the
// conversation, not a run, and false.
func (s *Server) startJobs(jobs []sideJob, hold int) bool {
	s.mu.Lock()
	drawn := map[string]int{}
	taken := 0
	for _, job := range jobs {
		if job.batch != "" && drawn[job.batch] < s.held[job.batch] {
			drawn[job.batch]++
			continue
		}
		taken++
	}
	limit := slotsFor(jobs)
	if len(s.sides)+s.heldSlots()+taken+hold > limit {
		// All four numbers, because any of them can be what refused this and
		// "0 agents are already running" on its own reads as a bug in the cap
		// rather than slots held for hand-offs that have not happened yet.
		running, heldNow, want := len(s.sides), s.heldSlots(), taken+hold
		s.mu.Unlock()
		most := fmt.Sprintf("%d at most", limit)
		if limit < maxSides {
			most = fmt.Sprintf("%d at most for agents, of %d, one kept for a rewrite", limit, maxSides)
		}
		s.tell(fmt.Sprintf(
			"No room: this needs %d slot(s), %d agent(s) running, %d held for hand-offs, %s. Wait for one, or stop them.",
			want, running, heldNow, most))
		return false
	}
	if s.held == nil {
		s.held = map[string]int{}
	}
	for batch, n := range drawn {
		s.held[batch] -= n
	}
	if hold > 0 && len(jobs) > 0 {
		s.held[jobs[0].batch] += hold
	}
	if s.sides == nil {
		s.sides = map[string]sideRun{}
	}
	type started struct {
		id     string
		ctx    context.Context
		cancel context.CancelFunc
		job    sideJob
	}
	runs := make([]started, 0, len(jobs))
	for _, job := range jobs {
		parent := job.parent
		if parent == nil {
			parent = s.ctx
		}
		ctx, cancel := context.WithTimeout(parent, sideTimeout)
		id := itoa(s.nextSide)
		s.nextSide++
		s.sides[id] = sideRun{name: job.name, started: time.Now(), cancel: cancel, batch: job.batch}
		runs = append(runs, started{id, ctx, cancel, job})
	}
	s.mu.Unlock()

	for _, run := range runs {
		go func() {
			defer run.cancel()
			defer func() {
				s.mu.Lock()
				delete(s.sides, run.id)
				// A batch is drained when its last run — reviewer or the
				// @commenter it handed to — is gone. The hand-off starts
				// inside run.job.run, before this delete, so a reviewer
				// that hands off never looks drained on the way out.
				drained := run.job.batch != "" && !s.batchRunning(run.job.batch)
				if drained {
					delete(s.held, run.job.batch)
				}
				s.mu.Unlock()
				s.hub.Broadcast("agent", nil)
				s.hub.Broadcast("state", nil)
				if drained {
					s.batchDrained(run.job.batch)
				}
			}()
			run.job.run(run.ctx)
		}()
	}
	return true
}

// runSubagent is one cold call on a named agent. Its prose and tool calls
// stream into the conversation under its name; when it is done, what it said
// is put in front of the main agent — into the run in flight if there is one,
// otherwise ahead of the next turn.
func (s *Server) runSubagent(ctx context.Context, call sideCall) {
	agent := call.agent
	filer := s.newFiler(agent, call.asker, call.batch)
	onEvent := func(ev oneshot.Event) {
		filer.push(ev.Text)
		s.onSubagentEvent(agent.Name, ev)
	}
	var err error
	if agent.Provider == agents.ProviderClaude {
		tools := agentTools(agent)
		_, err = s.oneShot(ctx, oneshot.Opts{
			Bin:    s.agent.bin,
			Dir:    s.space().diffRoot(),
			Model:  agent.Model,
			Agent:  agent.Name,
			Prompt: call.prompt,
			// The set it has is the set allowed unasked: nothing answers a prompt.
			Tools:          tools,
			AllowedTools:   tools,
			PermissionMode: "default",
			SystemPrompt:   s.elsewhereNote(),
			NoMCP:          !wantsMCP(tools),
			OnEvent:        onEvent,
		})
	} else {
		err = fmt.Errorf("no driver for %s agents: a review runs on claude", agent.Provider)
	}
	filer.done()
	s.reviewerEnded(ctx, call.batch, agent.Name, filer.held+len(filer.filed), err)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		s.tell("@" + agent.Name + " was stopped")
	default:
		slog.Warn("named agent failed", "agent", agent.Name, "err", err)
		s.tell("@" + agent.Name + ": " + err.Error())
	}
}

// filer files the ade-finding blocks a named agent emits as comments on the
// diff, the way the cold reviewer's are filed, as they arrive. The comments
// are from the agent that asked when one did — @commenter files
// @security's points as @security's — otherwise from the agent itself.
type filer struct {
	s        *Server
	name     string
	author   string
	batch    string
	streamer *review.Streamer
	filed    []string
	aside    []review.Finding

	// held is how many findings went to a batch rather than to the diff.
	held int

	// off is an agent that does not review: none of its reply is filed.
	off bool
}

func (s *Server) newFiler(agent agents.Agent, asker, batch string) *filer {
	author := asker
	if author == "" {
		author = agent.Name
	}
	// @commenter reviews nothing and files everything: filing is the whole job.
	off := !agent.Reviews && agent.Name != commenterAgent
	return &filer{s: s, name: agent.Name, author: author, batch: batch, streamer: review.NewStreamer(), off: off}
}

// push reads the next piece of the reply and files what it holds.
func (f *filer) push(text string) {
	if f.off {
		return
	}
	found := f.streamer.Push(text)
	if len(found) == 0 {
		return
	}
	root := f.s.space().diffRoot()
	onDiff, aside := f.split(review.Relative(found, root))
	f.aside = append(f.aside, aside...)
	if len(onDiff) == 0 {
		return
	}
	// Held and consolidated when every reviewer is done.
	if f.s.recordFound(f.batch, f.author, onDiff) {
		f.held += len(onDiff)
		return
	}
	f.s.fileFindings(onDiff, f.author, func(line string) { f.filed = append(f.filed, line) })
	f.s.hub.Broadcast("state", nil)
}

// split separates what can become a comment from a finding off the diff.
func (f *filer) split(found []review.Finding) (onDiff, aside []review.Finding) {
	f.s.mu.RLock()
	diff := f.s.diff
	f.s.mu.RUnlock()
	for _, finding := range found {
		if comments.HasFile(diff, finding.File) {
			onDiff = append(onDiff, finding)
			continue
		}
		aside = append(aside, finding)
	}
	return onDiff, aside
}

// done says what was filed, once, and what the diff has no line for.
func (f *filer) done() {
	if f.held > 0 {
		f.s.tell(fmt.Sprintf("@%s found %d point(s), filed when every reviewer has finished", f.name, f.held))
	}
	if len(f.filed) > 0 {
		f.s.tell(fmt.Sprintf("@%s filed %d comment(s) on the diff%s", f.name, len(f.filed), f.onBehalf()))
	}
	for _, finding := range f.aside {
		said := cmp.Or(finding.Headline, finding.Summary)
		f.s.tell(fmt.Sprintf("@%s, outside the diff: %s:%d %s", f.name, finding.File, finding.Line, said))
	}
}

// onSubagentEvent is a named agent's sign of life: its tool calls, by name.
func (s *Server) onSubagentEvent(name string, ev oneshot.Event) {
	s.agent.touch()
	who := name
	if name != "" {
		name = "@" + name + ": "
	}
	for _, tool := range ev.ToolUses {
		step := tool.Name + " " + toolTarget(tool.Input)
		s.reviewerDoing(who, step)
		s.tell(name + step)
	}
}

// toolTarget is what a tool call is aimed at, for one line of status.
func toolTarget(input json.RawMessage) string {
	var fields struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
		Command  string `json:"command"`
	}
	if err := json.Unmarshal(input, &fields); err != nil {
		return ""
	}
	return truncate(cmp.Or(fields.FilePath, fields.Path, fields.Pattern, fields.Command), 120)
}

// changedSummary is the current diff's shape, for a named agent's prompt: it
// has a shell, so it reads the parts it cares about itself.
func (s *Server) changedSummary() string {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	return review.RenderSummary(diff)
}

// wantsMCP is whether the set names a server's tool. Only a user-scoped
// definition can: agentTools filters a repository's down to read-only.
func wantsMCP(tools []string) bool {
	for _, tool := range tools {
		if strings.HasPrefix(tool, "mcp__") {
			return true
		}
	}
	return false
}

// writesCode is whether a tool set can change the checkout.
func writesCode(tools []string) bool {
	for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		if slices.Contains(tools, tool) {
			return true
		}
	}
	return false
}

// agentTools is what a named agent may use. A definition that names none
// gets a reviewer's read-only set. A definition from the repository gets no
// more than that whatever it names: a branch under review can add a file
// to .claude/agents, and that must not be a way to run things.
func agentTools(agent agents.Agent) []string {
	readOnly := review.ReadOnlyTools()
	if len(agent.Tools) == 0 {
		return readOnly
	}
	if agent.Scope != agents.ScopeRepo {
		return agent.Tools
	}
	allowed := map[string]bool{}
	for _, tool := range readOnly {
		allowed[tool] = true
	}
	var out []string
	for _, tool := range agent.Tools {
		if allowed[tool] {
			out = append(out, tool)
		}
	}
	if len(out) == 0 {
		return readOnly
	}
	return out
}

// sideRuns is what is in flight beside the main agent, for the page.
func (s *Server) sideRuns() []SideRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []SideRun
	for id, run := range s.sides {
		out = append(out, SideRun{ID: id, Name: run.name, StartedAt: run.started})
	}
	// Oldest first, and by id where two started in the same instant: a map's
	// order would shuffle the rows under the pointer between polls.
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// stopSide ends one named agent. Stopping a reviewer that has gone the wrong
// way should not take the rest of the batch with it, which is what the cancel
// button on the composer does.
func (s *Server) stopSide(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.sides[id]
	if !ok {
		return false
	}
	run.cancel()
	return true
}

// stopSides ends every named agent in flight, and says how many there were.
func (s *Server) stopSides() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.sides {
		run.cancel()
	}
	return len(s.sides)
}
func itoa(n int) string { return strconv.Itoa(n) }

// onBehalf names the agent a filing was made for, when it was not the filer.
func (f *filer) onBehalf() string {
	if f.author == f.name {
		return ""
	}
	return " as @" + f.author
}

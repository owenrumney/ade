package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/bridge/codex"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/review"
	"github.com/owenrumney/ade/internal/said"
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

// callSubagents starts each named agent on what was asked, at once, and
// says which it found. All or nothing: a message naming one agent that
// exists and one that does not goes to the main agent as written, rather
// than half of it happening. Too many for the cap is a note, not a run.
func (s *Server) callSubagents(names []string, message string, attached *chat.Context, files []chat.File) []string {
	list := s.agentsList()
	for _, name := range names {
		if _, ok := agents.Find(list, name); !ok {
			return nil
		}
	}
	from, text := "", ""
	if attached != nil {
		from, text = attached.From, attached.Text
	}
	note := design.FilesNote(attachedFiles(files))
	calls := make([]sideCall, 0, len(names))
	for _, name := range names {
		agent, _ := agents.Find(list, name)
		calls = append(calls, sideCall{agent: agent, prompt: design.SubagentPrompt(name, s.currentDocPath(), from, text, message, s.changedSummary(), s.thread(name)...) + note})
	}
	if !s.startSides(calls) {
		// Named, understood, and not run: the note in the conversation says
		// why, and the message must not go to the main agent instead.
		return []string{}
	}
	return names
}

// sideCall is a named agent and what to say to it. asker is the agent that
// called it, if one did rather than you, and hop how many hand-offs deep
// that is.
type sideCall struct {
	agent  agents.Agent
	prompt string
	asker  string
	hop    int

	parent context.Context
	result chan sideResult

	// handTo is an agent this reply goes to whether or not the reply asks
	// for it. A review reaches the diff through @commenter, and a reviewer
	// that opened with a heading rather than the hand-off used to lose
	// every point it made, silently.
	handTo string

	// batch groups the calls of one review — its reviewers and the
	// @commenter hand-offs they make — so the last of them can trigger the
	// rebuttal round. It cannot key on Server.sides emptying: two
	// overlapping reviews are indistinguishable there.
	batch string
}

type sideResult struct {
	reply string
	err   error
}

// handlePiAgents gives the Pi extension the names it may delegate to.
func (s *Server) handlePiAgents(w http.ResponseWriter, r *http.Request) {
	if !s.delegateAuthorized(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	type listed struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	all := s.agentsList()
	out := make([]listed, 0, len(all))
	for _, agent := range all {
		out = append(out, listed{Name: agent.Name, Description: agent.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// handlePiDelegate lets the Pi extension call a named agent and receive its
// answer as the tool result. It only accepts the per-process capability token.
func (s *Server) handlePiDelegate(w http.ResponseWriter, r *http.Request) {
	if !s.delegateAuthorized(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Name   string `json:"name"`
		Prompt string `json:"prompt"`
	}
	if !decodeUpTo(w, r, &req, maxChatBody) {
		return
	}
	reply, err := s.delegatePiAgent(r.Context(), strings.TrimSpace(req.Name), strings.TrimSpace(req.Prompt))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reply": reply})
}

// handleSaid is the main agent's way back to what a named agent said. Its own
// session holds none of this: it hears a one-shot relay and nothing more.
func (s *Server) handleSaid(w http.ResponseWriter, r *http.Request) {
	if !s.delegateAuthorized(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("agent"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "which agent? pass agent=<name>")
		return
	}
	writeJSON(w, http.StatusOK, said.Answer{
		Agent: name, Said: s.saidBy(name), Known: s.hasAgent(name),
	})
}

// saidBy is a named agent's replies and the notes it filed, oldest first. Its
// tool calls are left out: they are the working, not the answer.
func (s *Server) saidBy(name string) []said.Entry {
	out := []said.Entry{}
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.Author != name || (msg.Kind != chat.KindAgent && msg.Kind != chat.KindNote) {
			continue
		}
		if text := strings.TrimSpace(msg.Body); text != "" {
			out = append(out, said.Entry{Who: msg.Author, Kind: msg.Kind, At: msg.At, Text: text})
		}
	}
	return out
}

// hasAgent tells a name nobody is defined as from one that simply has not
// spoken, so the CLI can say which.
func (s *Server) hasAgent(name string) bool {
	_, ok := agents.Find(s.agentsList(), name)
	return ok
}

func (s *Server) delegateAuthorized(r *http.Request) bool {
	return s.delegateToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Ade-Delegate-Token")), []byte(s.delegateToken)) == 1
}

func (s *Server) delegatePiAgent(ctx context.Context, name, message string) (string, error) {
	if name == "" || message == "" {
		return "", errors.New("agent name and prompt are required")
	}
	agent, ok := agents.Find(s.agentsList(), name)
	if !ok {
		return "", fmt.Errorf("unknown agent %q", name)
	}
	result := make(chan sideResult, 1)
	call := sideCall{
		agent:  agent,
		prompt: design.SubagentPrompt(agent.Name, s.currentDocPath(), "", "", message, s.changedSummary(), s.thread(agent.Name)...),
		parent: ctx,
		result: result,
	}
	if !s.startSides([]sideCall{call}) {
		return "", errors.New("no room to run another agent")
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case reply := <-result:
		return reply.reply, reply.err
	}
}

// callSubagent starts a named agent on what was asked, beside whatever the
// main agent is doing. It returns false when there is no such agent, so the
// message can go to the main agent as written.
func (s *Server) callSubagent(name, message string, attached *chat.Context) bool {
	agent, ok := agents.Find(s.agentsList(), name)
	if !ok {
		return false
	}
	from, text := "", ""
	if attached != nil {
		from, text = attached.From, attached.Text
	}
	return s.startSides([]sideCall{{agent: agent, prompt: design.SubagentPrompt(agent.Name, s.currentDocPath(), from, text, message, s.changedSummary(), s.thread(agent.Name)...)}})
}

// A named agent's thread is what was said to it and what it said back, most
// recent last, capped so a long day's reviews do not become the prompt.
const (
	threadTurns = 12
	threadChars = 24000
)

// thread is a named agent's own exchanges in the current conversation: the
// messages addressed to it and its replies. Its tool calls and the rest of
// the conversation are left out — the one is noise, the other is the main
// agent's business.
func (s *Server) thread(name string) []design.Heard {
	var out []design.Heard
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		switch {
		case msg.Kind == chat.KindYou && addressedTo(msg.Body, name):
			out = append(out, design.Heard{Who: "you", Text: msg.Body})
		case msg.Kind == chat.KindAgent && msg.Author == name:
			out = append(out, design.Heard{Who: name, Text: msg.Body})
		case msg.Kind == chat.KindAgent && msg.Author != "" && (msg.For == name || addressedTo(msg.Body, name)):
			// Another agent asking it something, or answering it.
			out = append(out, design.Heard{Who: msg.Author, Text: msg.Body})
		}
	}
	if len(out) > threadTurns {
		out = out[len(out)-threadTurns:]
	}
	// Oldest first goes; the recent turns are the ones "the second one"
	// refers to.
	total := 0
	for i := len(out) - 1; i >= 0; i-- {
		total += len(out[i].Text)
		if total > threadChars {
			out = out[i+1:]
			break
		}
	}
	return out
}

// addressedTo is whether a message names the agent among its mentions.
func addressedTo(message, name string) bool {
	names, _, ok := agents.Mentions(message)
	if !ok {
		return false
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// currentDocPath is the open design document's path, or nothing.
func (s *Server) currentDocPath() string {
	if file := s.currentDoc(); file != "" {
		return s.docPath(file)
	}
	return ""
}

// sideDirs is what a named agent may read outside its checkout: in a worktree, the design documents.
func (s *Server) sideDirs() []string {
	docs, _ := s.designPlace()
	if docs == "" {
		return nil
	}
	rel, err := filepath.Rel(s.space().diffRoot(), docs)
	if err == nil && filepath.IsLocal(rel) {
		return nil
	}
	return []string{docs}
}

// reviewDocPath is the design a review is judged against: none when the diff is
// another repository's, since the session's design is not what it implements.
func (s *Server) reviewDocPath() string {
	if s.space().viewRoot != "" {
		return ""
	}
	return s.currentDocPath()
}

// sideProject is the system prompt a named agent runs under: the session's
// project memory, or where it is instead when the diff is somewhere else.
func (s *Server) sideProject() string {
	if note := s.elsewhereNote(); note != "" {
		return note
	}
	return s.agent.project(false)
}

// startSide runs a named agent on a prompt already built, beside whatever
// the main agent is doing.
func (s *Server) startSide(agent agents.Agent, prompt string) bool {
	return s.startSides([]sideCall{{agent: agent, prompt: prompt}})
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
		s.say(ChatMessage{Kind: chat.KindNote, Failed: true, Run: runSubagent,
			Body: fmt.Sprintf(
				"No room: this needs %d slot(s), %d agent(s) running, %d held for hand-offs, %s. Wait for one, or stop them.",
				want, running, heldNow, most)})
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

// noteInterruptedSides records, in the conversation, that a named agent was
// still running when the process went away. A side run lives only in memory:
// a rebuild on the dev server cancels it, and there is nothing on disk to
// reconcile against on the way back up. Without this the conversation keeps
// the question and never gains an answer, which reads as the agent having
// crashed rather than having been taken away from it.
func (s *Server) noteInterruptedSides() {
	s.mu.Lock()
	names := make([]string, 0, len(s.sides))
	for _, run := range s.sides {
		names = append(names, run.name)
	}
	s.mu.Unlock()

	sort.Strings(names)
	for _, name := range names {
		s.say(ChatMessage{Kind: chat.KindNote, Failed: true, Run: runSubagent, Author: name,
			Body: fmt.Sprintf("@%s was still running when ade restarted, so it never finished. Ask it again.", name)})
	}
}

// runSubagent is one cold call on a named agent. Its prose and tool calls
// stream into the conversation under its name; when it is done, what it said
// is put in front of the main agent — into the run in flight if there is one,
// otherwise ahead of the next turn.
func (s *Server) runSubagent(ctx context.Context, call sideCall) {
	agent, prompt := call.agent, call.prompt
	var reply strings.Builder
	var err error
	filer := s.newFiler(agent, call.asker, call.batch)
	onEvent := func(ev bridge.Event) {
		if t := strings.TrimSpace(ev.Text); t != "" {
			reply.WriteString(t)
			reply.WriteString("\n\n")
		}
		filer.push(ev.Text)
		s.onSubagentEventModel(agent.Name, agent.Model, call.asker, call.batch, ev)
	}
	switch agent.Provider {
	case agents.ProviderClaude:
		tools := agentTools(agent)
		// --add-dir is a working directory, not a read grant: an agent that
		// can write gets no reach outside the checkout.
		side := s.sideDirs()
		if writesCode(tools) {
			side = nil
		}
		_, err = s.coldClaude(ctx, bridge.OneShotOpts{
			Bin:    s.agent.bin,
			Dir:    s.space().diffRoot(),
			Model:  agent.Model,
			Agent:  agent.Name,
			Prompt: prompt,
			// The set it has, and the same set allowed without asking, so
			// a run with no terminal never stalls on a prompt and never
			// gets a tool the definition did not name. Never the main
			// agent's skip-everything: a definition is a file, and a
			// repository's files are not the user's say-so.
			Tools:          tools,
			AllowedTools:   tools,
			PermissionMode: "default",
			AddDirs:        side,
			SystemPrompt:   s.sideProject(),
			// Connecting to them costs a reviewer startup and window for
			// tools it was not given.
			NoMCP:   !wantsMCP(tools),
			OnEvent: onEvent,
		})
	case agents.ProviderCodex:
		err = s.runCodex(ctx, agent, prompt, onEvent)
	default:
		err = errProviderUnsupported(agent.Provider)
	}
	filer.done()
	if err != nil {
		if ctx.Err() != nil {
			s.say(ChatMessage{Kind: chat.KindNote, Author: agent.Name, For: call.asker, Run: runSubagent, Body: "@" + agent.Name + " was stopped"})
			call.respond("", ctx.Err())
			return
		}
		slog.Warn("named agent failed", "agent", agent.Name, "err", err)
		s.say(ChatMessage{Kind: chat.KindNote, Author: agent.Name, For: call.asker, Run: runSubagent, Failed: true, Body: "@" + agent.Name + ": " + err.Error()})
		call.respond("", err)
		return
	}
	said := review.StripStream(reply.String())
	// The blocks are gone from the reply; the ones that became no comment
	// have to travel as text or the main agent never hears them.
	if aside := filer.asideText(); aside != "" {
		said = strings.TrimSpace(said + "\n\n" + aside)
	}
	if said == "" {
		call.respond("", errors.New("agent returned no response"))
		return
	}
	if call.result == nil {
		s.relay(agent.Name, said)
		// A reply that starts "@name …" is a hand-off: the agent wants another
		// agent's answer, and gets it back in its thread.
		if names, rest, ok := onward(said, call.handTo); ok {
			s.handoff(agent, names, rest, call.hop, call.batch)
		}
	}
	call.respond(said, nil)
}

func (call sideCall) respond(reply string, err error) {
	if call.result == nil {
		return
	}
	select {
	case call.result <- sideResult{reply: reply, err: err}:
	default:
	}
}

// onward is who a reply goes on to: whoever it named, or the call's standing
// hand-off when it named nobody. A review reaches the diff through
// @commenter, and asking the reviewer to open its reply with the name put a
// formatting habit between a finding and the comment it should have become.
func onward(said, handTo string) (names []string, message string, ok bool) {
	if names, rest, ok := agents.Mentions(said); ok {
		return names, rest, true
	}
	if handTo == "" {
		return nil, "", false
	}
	return []string{handTo}, said, true
}

// handoff calls the agents a reply named, on behalf of the agent that
// replied. Past the hop limit it is a note; an agent naming itself, or one
// that does not exist, is skipped.
func (s *Server) handoff(from agents.Agent, names []string, message string, hop int, batch string) {
	if hop >= maxHops {
		s.say(ChatMessage{Kind: chat.KindNote, Author: from.Name, Run: runSubagent,
			Body: fmt.Sprintf("@%s asked for @%s, but that is %d hand-offs deep; ask directly if you want it", from.Name, strings.Join(names, " @"), hop+1)})
		return
	}
	// A review's hand-off is @commenter, which files findings. Resolving it
	// from the user scope only keeps a branch under review from shipping
	// .claude/agents/commenter.md and standing between a finding and the
	// comment it becomes.
	list := s.agentsList()
	if batch != "" {
		list = s.userAgentsList()
	}
	var calls []sideCall
	for _, name := range names {
		agent, ok := agents.Find(list, name)
		if !ok || name == from.Name {
			continue
		}
		calls = append(calls, sideCall{
			agent:  agent,
			prompt: design.HandoffPrompt(name, from.Name, s.currentDocPath(), message, s.changedSummary(), s.thread(name)...),
			asker:  from.Name,
			hop:    hop + 1,
			batch:  batch,
		})
	}
	if len(calls) > 0 {
		s.startSides(calls)
	}
}

// runCodex is a named agent on the Codex subscription: the codex CLI's login,
// its configured model unless the definition names one, and read-only tools
// over the checkout. The definition's prompt is the system prompt.
func (s *Server) runCodex(ctx context.Context, agent agents.Agent, prompt string, onEvent func(bridge.Event)) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	creds, err := codex.LoadCredentials(home)
	if err != nil {
		return err
	}
	model := agent.Model
	if model == "" {
		model = codex.DefaultModel(home)
	}
	_, err = codex.Run(ctx, codex.Opts{
		Model:        model,
		Effort:       codex.DefaultEffort(home),
		Instructions: withProject(agent.Prompt, s.sideProject()),
		Tools:        codex.RepoTools(s.space().diffRoot(), agentTools(agent), s.sideDirs()...),
		Credentials:  creds,
		Home:         home,
		OnLimits:     s.limits.NoteCodex,
		OnEvent:      onEvent,
	}, prompt)
	return err
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

	// held is how many findings went to a batch rather than to the diff. They
	// are filed, consolidated, once every reviewer has finished.
	held int

	// off is an agent that does not review: nothing of its reply is filed.
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

// push reads the next piece of the reply and files what it holds. The thread
// ids come back with it: the rebuttal round marks those threads, so the
// batch has to know which finding became which.
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
	// Held and consolidated when every reviewer is done. Design:
	// .git/ade/docs/enhancements/12-finding-consolidation.md.
	if f.s.recordFound(f.batch, f.author, onDiff) {
		f.held += len(onDiff)
		return
	}
	f.s.fileFindings(onDiff, f.author, func(line string) { f.filed = append(f.filed, line) })
	f.s.hub.Broadcast("state", nil)
}

// split separates what can become a comment from what cannot: a finding on a
// file the diff does not contain has nowhere on the diff to live.
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

// asideText is the findings that could not be filed, written out: the blocks
// themselves are stripped from the reply, so this is all that is left of them.
func (f *filer) asideText() string {
	var b strings.Builder
	for _, finding := range f.aside {
		fmt.Fprintf(&b, "**%s:%d** — %s %s\n\n%s\n\n", finding.File, finding.Line, finding.Severity, finding.Category, review.Body(finding))
	}
	return strings.TrimSpace(b.String())
}

// done says in the conversation what was filed, once, when anything was, and
// what was found somewhere the diff cannot show.
func (f *filer) done() {
	if f.held > 0 {
		f.s.say(ChatMessage{Kind: chat.KindNote, Author: f.name, Run: runSubagent,
			Body: fmt.Sprintf("@%s found %d point(s). They are filed when every reviewer has finished.", f.name, f.held)})
	}
	if len(f.filed) > 0 {
		f.s.say(ChatMessage{Kind: chat.KindNote, Author: f.name, Run: runSubagent,
			Body: fmt.Sprintf("@%s filed %d comment(s) on the diff as @%s:\n- %s", f.name, len(f.filed), f.author, strings.Join(f.filed, "\n- "))})
	}
	if aside := f.asideText(); aside != "" {
		body := fmt.Sprintf("@%s found %d point(s) outside the diff, so they are here rather than on it:\n\n%s", f.name, len(f.aside), aside)
		f.s.say(ChatMessage{Kind: chat.KindNote, Author: f.name, Run: runSubagent, Body: body, HTML: f.s.replyHTML(body)})
	}
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

// onSubagentEvent is onAgentEvent for a named agent: the same rows, under its
// name, and marked for the agent that asked when one did.
func (s *Server) onSubagentEvent(name, asker string, ev bridge.Event) {
	s.onSubagentEventModel(name, "", asker, "", ev)
}

// batch is the review these rows belong to, empty when the agent was called
// in the conversation rather than at a diff.
func (s *Server) onSubagentEventModel(name, model, asker, batch string, ev bridge.Event) {
	for _, tool := range ev.ToolUses {
		msg := s.say(ChatMessage{
			Kind: chat.KindTool, Author: name, For: asker, Run: runSubagent, Tool: tool.Name, ToolID: tool.ID,
			Target: toolTarget(tool.Input), Input: string(tool.Input),
		})
		s.mu.Lock()
		s.tools[tool.ID] = msg.ID
		s.mu.Unlock()
	}
	// The finding blocks are the comments they became, not prose.
	if text := review.StripStream(ev.Text); text != "" {
		s.say(ChatMessage{Kind: chat.KindAgent, Author: name, For: asker, Run: runSubagent, Model: model,
			Review: batch != "", Body: text, HTML: s.replyHTML(text)})
	}
	for _, result := range ev.ToolResults {
		s.answerTool(result)
	}
}

// relay puts what a named agent said in front of the main agent: injected
// into the turn in flight when there is one, otherwise held for the next.
func (s *Server) relay(name, said string) {
	heard := design.Heard{Who: name, Text: said}
	if s.agent.inject(design.TurnPrompt(design.Doc{}, "", "", "", heard), nil) {
		return
	}
	s.mu.Lock()
	s.heard = append(s.heard, heard)
	s.mu.Unlock()
}

// heardSoFar is what named agents said since the main agent last spoke, and
// leaves it: a prompt built and then dropped must not take it along.
func (s *Server) heardSoFar() []design.Heard {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]design.Heard(nil), s.heard...)
}

// dropHeard forgets what a sent prompt carried, and only that: an agent may
// have finished while that prompt was being built.
func (s *Server) dropHeard(carried []design.Heard) {
	if len(carried) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(carried) >= len(s.heard) {
		s.heard = nil
		return
	}
	s.heard = append([]design.Heard(nil), s.heard[len(carried):]...)
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

type providerError string

func (e providerError) Error() string { return "no driver for " + string(e) + " agents yet" }

func errProviderUnsupported(provider string) error { return providerError(provider) }

func itoa(n int) string { return strconv.Itoa(n) }

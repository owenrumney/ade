package web

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/term"
)

// agentAsk is the agent's first message, or one typed at its prompt.
type agentAsk struct {
	Prompt string `json:"prompt"`
}

// takeoverPane is the pane the agent's own TUI runs in. One per checkout: two
// would be two processes on one transcript.
const takeoverPane = "agent"

// errMidTurn is ade's own agent still holding the conversation.
var errMidTurn = errors.New("the agent is mid-turn; wait for it to finish or stop it first")

// handleAgentTerminal hands the conversation to the agent's own interface. The
// TUI replaces ade's agent rather than showing it. See the design doc.
func (s *Server) handleAgentTerminal(w http.ResponseWriter, r *http.Request) {
	var req agentAsk
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	if _, err := s.agentPane(r.Context(), req.Prompt); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMidTurn) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	// Not argv: a boxed one carries the box's tokens as -e values.
	writeJSON(w, http.StatusOK, map[string]any{"pane": takeoverPane})
}

// agentPane starts the agent's own interface, or types prompt into one already up.
func (s *Server) agentPane(ctx context.Context, prompt string) (*term.Session, error) {
	key := paneKeyFor(takeoverPane)
	s.termMu.Lock()
	held := s.terms[key]
	s.termMu.Unlock()

	if held != nil && held.Alive() {
		return held, typed(held, prompt)
	}
	// Made and not yet attached to: its argv already says how it starts.
	if held != nil && !held.Started() && strings.TrimSpace(prompt) == "" {
		return held, nil
	}
	if s.agent.Busy() {
		return nil, errMidTurn
	}
	argv, err := s.agent.takeoverCommand(ctx, prompt, held != nil && held.Started())
	if err != nil {
		return nil, err
	}
	// The bridge goes before the TUI starts, so the two never overlap.
	s.agent.releaseForTakeover()
	s.dropPane(key)

	sess, err := s.pane(key, argv...)
	if err != nil {
		// No pane means no interface holds the conversation, and a flag left set
		// would leave both refusing every turn.
		s.agent.endTakeover()
		return nil, err
	}
	s.hub.Broadcast("state", nil)
	return sess, nil
}

// typed pastes text at the agent's prompt and sends it, newlines and all.
func typed(sess *term.Session, text string) error {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\x1b", "")
	if text == "" {
		return nil
	}
	return sess.Write([]byte("\x1b[200~" + text + "\x1b[201~\r"))
}

func (s *Server) dropPane(key string) {
	s.termMu.Lock()
	sess := s.terms[key]
	delete(s.terms, key)
	s.termMu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// yieldAgentPane clears an exited agent terminal; one still running refuses the move.
func (s *Server) yieldAgentPane(w http.ResponseWriter) bool {
	if !s.agent.TakenOver() {
		return true
	}
	key := paneKeyFor(takeoverPane)
	s.termMu.Lock()
	held := s.terms[key]
	s.termMu.Unlock()
	if held != nil && held.Alive() {
		writeError(w, http.StatusConflict, "this checkout's agent is running in its terminal; quit it there before changing conversation")
		return false
	}
	s.dropPane(key)
	s.agent.endTakeover()
	return true
}

// handleAgentTerminalClose ends the takeover, so the next turn starts a bridge again.
func (s *Server) handleAgentTerminalClose(w http.ResponseWriter, r *http.Request) {
	s.dropPane(paneKeyFor(takeoverPane))
	// Killing a -t client leaves its container running on the conversation.
	s.agent.removeTUI(r.Context())
	s.agent.endTakeover()
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

func paneKeyFor(id string) string { return "pane:" + id }

// takeoverCommand is what the takeover pane runs. A sandboxed agent's TUI runs in
// its box: on a worker only the box has the binary and the hub's loopback.
func (a *agentRunner) takeoverCommand(ctx context.Context, prompt string, started bool) ([]string, error) {
	// Quit before its first message, a TUI left nothing to resume.
	started = started && a.tuiSpoke()
	argv, env, boxed, err := a.takeoverArgv(prompt, started)
	if err != nil {
		return nil, err
	}
	// A conversation the TUI opened is ade's as well, so the rail names it.
	if id := a.openedByTUI(); id != "" {
		a.note(id)
	}
	if !boxed {
		return argv, nil
	}
	spec, rt, err := a.sandboxOpts(ctx)
	if err != nil {
		return nil, fmt.Errorf("sandbox: %w", err)
	}
	box := bridge.BoxedTUI(*spec, argv, env)
	box.Name = sandbox.TUIContainerName(a.dir)
	sandbox.Remove(ctx, rt, box.Name)
	return append([]string{rt.Bin()}, rt.Args(box)...), nil
}

// takeoverArgv is the agent's own command line, resuming what ade was in.
// started is whether its pane has run before, which is a resume and not a start.
func (a *agentRunner) takeoverArgv(prompt string, started bool) (argv, env []string, boxed bool, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	bin := "claude"
	if a.provider == "codex" {
		bin = "codex"
	}
	boxed = a.sandbox
	if _, err := exec.LookPath(bin); err != nil && !boxed {
		return nil, nil, false, fmt.Errorf("%s is not on the PATH of the machine this session runs on", bin)
	}
	// Resume what ade was in, so the TUI opens on the same conversation.
	resume := a.current
	if resume == "" {
		resume = a.session
	}
	if bin == "codex" {
		argv = codexArgv(resume, started)
	} else {
		argv = a.claudeArgv(resume, started)
	}
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		// A message that opens with a dash is still a message.
		if strings.HasPrefix(prompt, "-") {
			argv = append(argv, "--")
		}
		argv = append(argv, prompt)
	}
	if boxed {
		env = a.delegateEnv()
	}
	return argv, env, boxed, nil
}

// codexArgv resumes by subcommand. Codex names its own conversations, so one
// started here and not yet read back is the last in this directory.
func codexArgv(resume string, started bool) []string {
	switch {
	case resume != "":
		return []string{"codex", "resume", resume}
	case started:
		return []string{"codex", "resume", "--last"}
	}
	return []string{"codex"}
}

// claudeArgv names a new conversation up front, so the id is known before
// the first message. Until its pane has run there is nothing to resume.
func (a *agentRunner) claudeArgv(resume string, started bool) []string {
	argv := []string{"claude"}
	if resume == "" && a.tuiID == "" {
		a.tuiID = newUUID()
	}
	if resume == "" {
		resume = a.tuiID
	}
	switch {
	case resume == "":
	case resume == a.tuiID && !started:
		argv = append(argv, "--session-id", resume)
	default:
		argv = append(argv, "--resume", resume)
	}
	if a.model != "" {
		argv = append(argv, "--model", a.model)
	}
	// Deny rules hold in every permission mode, which an allow rule does not.
	if deny := denyRules(a.rules); len(deny) > 0 {
		settings, err := json.Marshal(map[string]any{"permissions": map[string]any{"deny": deny}})
		if err == nil {
			argv = append(argv, "--settings", string(settings))
		}
	}
	return argv
}

// openedByTUI is the conversation a TUI started here named for itself.
func (a *agentRunner) openedByTUI() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tuiID
}

// tuiSpoke is whether the conversation a TUI named here has a transcript yet.
func (a *agentRunner) tuiSpoke() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tuiID == "" || providerTranscriptSize(a.dir, a.provider, a.tuiID) > 0
}

// newUUID is a random version 4 id, or empty, and claude then names its own.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// releaseForTakeover stops ade's own agent while keeping the session id, so
// the TUI resumes the conversation rather than starting beside it.
func (a *agentRunner) releaseForTakeover() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dropKeepingSession()
	a.takenOver = true
}

func (a *agentRunner) removeTUI(ctx context.Context) {
	a.mu.Lock()
	rt, dir, boxed := a.runtime, a.dir, a.sandbox
	a.mu.Unlock()
	if boxed {
		sandbox.Remove(ctx, rt, sandbox.TUIContainerName(dir))
	}
}

func (a *agentRunner) endTakeover() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.takenOver = false
	a.tuiID = ""
}

// TakenOver reports the agent's own interface holding the conversation.
func (a *agentRunner) TakenOver() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.takenOver
}

// Busy is a turn in flight, which a takeover must not interrupt.
func (a *agentRunner) Busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

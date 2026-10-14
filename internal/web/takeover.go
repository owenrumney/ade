package web

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/sandbox"
)

// takeoverPane is the pane the agent's own TUI runs in. One per checkout: two
// would be two processes on one transcript.
const takeoverPane = "agent"

// handleAgentTerminal hands the conversation to the agent's own interface. The
// TUI replaces ade's agent rather than showing it. See the design doc.
func (s *Server) handleAgentTerminal(w http.ResponseWriter, r *http.Request) {
	if s.agent.Busy() {
		writeError(w, http.StatusConflict, "the agent is mid-turn; wait for it to finish or stop it first")
		return
	}
	argv, err := s.agent.takeoverCommand(r.Context())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The bridge goes before the TUI starts, so the two never overlap.
	s.agent.releaseForTakeover()

	if _, err := s.pane(paneKeyFor(takeoverPane), argv...); err != nil {
		// No pane means no interface holds the conversation, and a flag left set
		// would leave both refusing every turn.
		s.agent.endTakeover()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("state", nil)
	// Not argv: a boxed one carries the box's tokens as -e values.
	writeJSON(w, http.StatusOK, map[string]any{"pane": takeoverPane})
}

// handleAgentTerminalClose ends the takeover, so the next turn starts a bridge
// again on whatever the TUI left behind.
func (s *Server) handleAgentTerminalClose(w http.ResponseWriter, r *http.Request) {
	key := paneKeyFor(takeoverPane)
	s.termMu.Lock()
	sess := s.terms[key]
	delete(s.terms, key)
	s.termMu.Unlock()
	if sess != nil {
		sess.Close()
	}
	// Killing a -t client leaves its container running on the conversation.
	s.agent.removeTUI(r.Context())
	s.agent.endTakeover()
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

func paneKeyFor(id string) string { return "pane:" + id }

// takeoverCommand is what the takeover pane runs. A sandboxed agent's TUI runs in
// its box: on a worker only the box has the binary and the hub's loopback.
func (a *agentRunner) takeoverCommand(ctx context.Context) ([]string, error) {
	argv, env, boxed, err := a.takeoverArgv()
	if err != nil || !boxed {
		return argv, err
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
func (a *agentRunner) takeoverArgv() (argv, env []string, boxed bool, err error) {
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
	argv = []string{bin}
	// Resume what ade was in, so the TUI opens on the same conversation.
	resume := a.current
	if resume == "" {
		resume = a.session
	}
	if resume != "" {
		argv = append(argv, "--resume", resume)
	}
	if a.model != "" && a.provider != "codex" {
		argv = append(argv, "--model", a.model)
	}
	if boxed {
		env = a.delegateEnv()
	}
	return argv, env, boxed, nil
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

package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/settings"
)

// How a terminal agent is on agentbus: its tools handed over at launch, an
// answer typed at its prompt, and its standing read off its own process.

// busTokenVar is where codex reads the token from, so it is on no command line.
const busTokenVar = "ADE_AGENTBUS_TOKEN"

// busEntry is the bus tool server, or nothing with the bus off. Lock held.
func (a *agentRunner) busEntry() json.RawMessage {
	if cfg, err := settings.Load(); err == nil && cfg.Agentbus.Off {
		return nil
	}
	return busServer(a.delegateURL, a.delegateToken)
}

// busConfig writes the one tool server claude's own interface is handed, and
// says where. Lock held.
func (a *agentRunner) busConfig(name string) string {
	entry := a.busEntry()
	if entry == nil {
		return ""
	}
	if name == "" {
		name = "agent"
	}
	path := filepath.Join(agentMCPDir(), "terminal-"+name+".json")
	config, err := writeAgentMCP(path, map[string]json.RawMessage{busServerName: entry})
	if err != nil {
		slog.Warn("the bus tools are not reachable from this agent", "err", err)
		return ""
	}
	return config
}

// codexBus is the same server as codex is told of it. Lock held.
func (a *agentRunner) codexBus() []string {
	if a.busEntry() == nil {
		return nil
	}
	at := "mcp_servers." + busServerName
	return []string{
		at + ".url=" + strconv.Quote(strings.TrimSuffix(a.delegateURL, "/")+BusMCPPath),
		at + ".bearer_token_env_var=" + strconv.Quote(busTokenVar),
	}
}

// busEnv is what the agent's process needs beside its tools: the machine's
// instance to join, and the token codex sends.
func (a *agentRunner) busEnv() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busEntry() == nil {
		return nil
	}
	return []string{agentbus.EnvInstance + "=" + busInstanceID(), busTokenVar + "=" + a.delegateToken}
}

// answered hands the agent an answer it asked the bus for: typed at its own
// terminal when it has one, a note in ade's chat when it does not.
func (s *Server) answered(ctx context.Context, line string) {
	if s.agentTerm() != nil {
		if _, err := s.agentPane(ctx, "From agentbus: "+line); err == nil {
			return
		}
	}
	slog.Warn("an answer from the bus found no agent terminal to go to", "answer", line)
}

// busStanding is what the bus is told of a run: a terminal agent is working
// or waiting on its human by its badge, since it has no turn of ade's.
func busStanding(run RunState) (running bool, asking int) {

	if run.Terminal == badgeNeeds {
		asking++
	}
	return run.Running || run.Terminal == badgeWorking, asking
}

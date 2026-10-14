package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/concierge"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/statedir"
	"github.com/owenrumney/ade/internal/wiki"
)

// busServerName is what the agent sees the bus as. The agentbus skills name
// the server, so renaming it breaks them.
const busServerName = "agentbus"

// busServer is ade's own bus tools, over http. No binary and no process per
// run: ade already speaks the protocol and already serves MCP.
func busServer(base, token string) json.RawMessage {
	if base == "" || token == "" {
		return nil
	}
	entry, err := json.Marshal(map[string]any{
		"type":    "http",
		"url":     strings.TrimSuffix(base, "/") + BusMCPPath,
		"headers": map[string]string{"X-Ade-Delegate-Token": token},
	})
	if err != nil {
		return nil
	}
	return entry
}

// agentServers is every MCP server a run gets, by name. Each is optional and
// left out when it cannot be reached, so one missing does not take the others.
func agentServers(ctx context.Context, a *agentRunner, cfg settings.Agentbus) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if draft, ok := wikiDraftFor(ctx, a.wiki, a.wikiDraft, a.dir); ok && a.delegateURL != "" {
		for name, entry := range wiki.ServerConfig(a.delegateURL, draft.Session) {
			out[name] = entry
		}
	}
	if !cfg.Off {
		if entry := busServer(a.delegateURL, a.delegateToken); entry != nil {
			out[busServerName] = entry
		}
	}
	if entry := conciergeServer(a.dir); entry != nil {
		out[concierge.ServerName] = entry
	}
	return out
}

// conciergeServer is the desk's own tools, from the launcher, and handed only
// to the desk's checkout. No coding session reads ade's state this way.
func conciergeServer(dir string) json.RawMessage {
	if !concierge.Is(dir) {
		return nil
	}
	at, err := url.Parse(launcherURL())
	if err != nil || at.Host == "" {
		return nil
	}
	token := at.Query().Get("t")
	if token == "" {
		return nil
	}
	entry, err := json.Marshal(map[string]any{
		"type":    "http",
		"url":     at.Scheme + "://" + at.Host + concierge.MCPPath,
		"headers": map[string]string{"X-Ade-Token": token},
	})
	if err != nil {
		return nil
	}
	return entry
}

// writeAgentMCP writes the --mcp-config for a run on the host, where ade owns
// no box to merge into. Empty means there is nothing to pass.
func writeAgentMCP(path string, servers map[string]json.RawMessage) (string, error) {
	if len(servers) == 0 {
		return "", nil
	}
	data, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// agentMCPDir is where a run's config is written: ade's own state, not a
// checkout's, because the bus instance it names is the machine's.
func agentMCPDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "ade-agent-mcp")
	}
	return filepath.Join(statedir.Home(home), "agent-mcp")
}

// agentMCP is the config file and the environment a run needs for its tools.
func agentMCP(ctx context.Context, a *agentRunner) (string, []string) {
	cfg, err := settings.Load()
	if err != nil {
		slog.Debug("read settings for the agent's tools", "err", err)
	}
	servers := agentServers(ctx, a, cfg.Agentbus)

	var pairs []string
	draft, ok := wikiDraftFor(ctx, a.wiki, a.wikiDraft, a.dir)
	if ok {
		_, env := wikiMounts(draft)
		for key, value := range env {
			pairs = append(pairs, key+"="+value)
		}
		sort.Strings(pairs)
	}
	if len(servers) == 0 {
		return "", pairs
	}

	name := a.current
	if name == "" {
		name = a.session
	}
	if name == "" {
		name = "run"
	}
	path := filepath.Join(agentMCPDir(), name+".json")
	config, err := writeAgentMCP(path, servers)
	if err != nil {
		slog.Warn("the agent's tools are not reachable from this run", "err", err)
		return "", pairs
	}
	return config, pairs
}

package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/statedir"
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

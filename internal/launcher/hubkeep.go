package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/owenrumney/ade/internal/hub"
	"github.com/owenrumney/ade/internal/remote"
)

// hubRetry is how often a machine without a running hub is tried again. The
// binary appearing, or the hub dying, are both rare.
const hubRetry = 30 * time.Second

// keepHub has this machine's hub up while the launcher is. Inside a worker
// there is no binary and a container hub instead, so it quietly does nothing.
func (s *Server) keepHub(ctx context.Context) {
	if os.Getenv("ADE_HUB_CONTAINER") != "" {
		return
	}
	try := func() {
		_, err := hub.Ensure(ctx)
		if err != nil && !errors.Is(err, hub.ErrNotInstalled) {
			slog.Warn("mcp-hub is not running and could not be started", "err", err)
		}
	}
	try()
	ticker := time.NewTicker(hubRetry)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			try()
		}
	}
}

// localHubState is the laptop's hub configuration, for a worker's hub to start
// from. The WAL rides along: the running hub's newest rows may be only there.
func localHubState() remote.HubState {
	dir := os.Getenv("MCP_HUB_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return remote.HubState{}
		}
		dir = filepath.Join(home, ".mcp-hub")
	}
	db, at := readStamped(filepath.Join(dir, "mcp-hub.db"))
	wal, _ := readStamped(filepath.Join(dir, "mcp-hub.db-wal"))
	return remote.HubState{DB: db, WAL: wal, At: at}
}

// localMCPServers is the laptop's hub-proxied servers: the loopback http entries
// of ~/.claude.json. A worker's agents get the same list, against their hub.
func localMCPServers() map[string]json.RawMessage {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return nil
	}
	var file struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil
	}
	out := map[string]json.RawMessage{}
	for name, server := range file.Servers {
		var entry struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(server, &entry) != nil || !loopbackURL(entry.URL) {
			continue
		}
		out[name] = server
	}
	return out
}

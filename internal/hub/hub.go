// Package hub finds a running mcp-hub, so ade can offer a way into it without
// owning it. Nothing here starts, stops or configures the hub: mcp-hub is a
// separate tool that works with ade absent, and this only reports whether it
// happens to be up.
package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// probeTimeout bounds the health check. It is a loopback request to a process
// that is either there or not, so this only has to cover a stalled one.
const probeTimeout = 2 * time.Second

const envHome = "MCP_HUB_HOME"

// Info is what the page needs: where the hub is, and whether it is worth
// offering. URL is empty unless Running.
type Info struct {
	URL     string `json:"url"`
	Running bool   `json:"running"`
}

// record is mcp-hub's side of the contract, written by its serve command.
type record struct {
	PID int    `json:"pid"`
	URL string `json:"url"`
}

// health is what mcp-hub's /healthz answers.
type health struct {
	MCPHub bool `json:"mcpHub"`
	PID    int  `json:"pid"`
}

// RecordPath is where mcp-hub says it is running, which defaults to
// ~/.mcp-hub/server.json and follows MCP_HUB_HOME when that is set.
func RecordPath() (string, error) {
	if home := os.Getenv(envHome); home != "" {
		return filepath.Join(home, "server.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home: %w", err)
	}
	return filepath.Join(home, ".mcp-hub", "server.json"), nil
}

// Detect reports the running hub, if there is one.
//
// Deliberately not a port probe. mcp-hub's --addr is configurable, so the
// default port is a false negative when it has moved and a false positive when
// something else has taken it. The record says where; the pid and the health
// answer together say whether it is really still that process.
//
// Costs one failed stat when no hub has ever run, which is the common case.
func Detect(ctx context.Context) Info {
	path, err := RecordPath()
	if err != nil {
		return Info{}
	}
	rec, ok := read(path)
	if !ok || !running(rec.PID) {
		return Info{}
	}
	found, err := probe(ctx, rec.URL)
	if err != nil || !found.MCPHub || found.PID != rec.PID {
		return Info{}
	}
	return Info{URL: rec.URL, Running: true}
}

func read(path string) (record, bool) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return record{}, false
	}
	var rec record
	if err := json.Unmarshal(data, &rec); err != nil || rec.PID <= 0 || rec.URL == "" {
		return record{}, false
	}
	// The record names a port that starts and stops processes. Anything but a
	// loopback address in it is not something to hand the browser.
	if !loopback(rec.URL) {
		return record{}, false
	}
	return rec, true
}

// loopback keeps the offered link local. A record is a file on disk, and this
// is the one thing worth checking before turning it into a link.
func loopback(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// running reports whether a process exists. Signal 0 checks for it without
// delivering anything.
func running(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func probe(ctx context.Context, base string) (health, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return health{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return health{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return health{}, fmt.Errorf("health returned %s", res.Status)
	}

	var found health
	if err := json.NewDecoder(res.Body).Decode(&found); err != nil {
		return health{}, err
	}
	return found, nil
}

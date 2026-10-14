package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotInstalled is Ensure finding no hub to start: mcp-hub is its own tool,
// and a machine without it simply has no hub.
var ErrNotInstalled = errors.New("mcp-hub is not installed")

// startWait bounds the wait for a hub Ensure started to write its record and
// answer. A var so a test can shorten it.
var startWait = 5 * time.Second

// lookPath finds the binary; a test points it at a stand-in.
var lookPath = exec.LookPath

// Ensure reports the running hub, starting one first when the binary is here
// and none is up. The worker's ade never gets here: its hub is a container.
func Ensure(ctx context.Context) (Info, error) {
	if found := Detect(ctx); found.Running {
		return found, nil
	}
	bin, err := binary()
	if err != nil {
		return Info{}, err
	}
	cmd := exec.CommandContext(ctx, bin, "serve", "--detached") // #nosec G204 -- a binary found on PATH, fixed args
	if out, err := cmd.CombinedOutput(); err != nil {
		return Info{}, fmt.Errorf("start mcp-hub: %w: %s", err, strings.TrimSpace(string(out)))
	}
	deadline := time.Now().Add(startWait)
	for time.Now().Before(deadline) {
		if found := Detect(ctx); found.Running {
			return found, nil
		}
		select {
		case <-ctx.Done():
			return Info{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return Info{}, fmt.Errorf("mcp-hub started but did not answer within %s", startWait)
}

// binary is mcp-hub on PATH, or where `go install` leaves it when PATH lacks it.
func binary() (string, error) {
	if path, err := lookPath("mcp-hub"); err == nil {
		return path, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, "go", "bin", "mcp-hub")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", ErrNotInstalled
}

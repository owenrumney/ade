// Package session records the ade server running for a checkout, so a second
// run can hand you the first one's link instead of starting a rival.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// probeTimeout bounds the health check. It is a loopback request to a process
// that is either there or not, so this only has to cover a stalled one.
const probeTimeout = 2 * time.Second

// Record is the running server, as written for the next invocation to find.
//
// URL carries the session token, which makes this file a credential: it is
// written 0600 inside the git dir, which is never committed, and the token dies
// with the process. Printing a link that works is the whole point, so the token
// has to be somewhere.
type Record struct {
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	Root      string    `json:"root"`
	StartedAt time.Time `json:"startedAt"`

	// Proxy is this session through its launcher, for a reader that cannot
	// reach URL. See the design doc: a worker publishes only the launcher's port.
	Proxy string `json:"proxy,omitempty"`
}

// Health is what /healthz answers, and what proves the port belongs to us.
// Agent and Branch ride along for the launcher's picture of the machine, and
// only for a probe that carries the server's token: the port is predictable,
// so what the agent is doing is not said to just anyone on loopback.
type Health struct {
	Ade  bool   `json:"ade"`
	Root string `json:"root"`
	PID  int    `json:"pid"`
	// Version is the binary serving this checkout: a 404 or a 405 does
	// not say that the window is newer than the server.
	Version string       `json:"version,omitempty"`
	Branch  string       `json:"branch,omitempty"`
	Agent   *AgentHealth `json:"agent,omitempty"`
}

// AgentHealth is what a server's agent is up to, in one line.
type AgentHealth struct {
	Running   bool      `json:"running"`
	Kind      string    `json:"kind,omitempty"`
	Model     string    `json:"model,omitempty"`
	Session   string    `json:"session,omitempty"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Queued    int       `json:"queued,omitempty"`
	Error     string    `json:"error,omitempty"`
	// LastEventAt is when the agent last said anything, which is the one
	// thing a reader outside the process cannot work out.
	LastEventAt time.Time `json:"lastEventAt,omitempty"`
	Activity    []int     `json:"activity,omitempty"`
	// Asking is how many questions the agent is waiting on answers to. A
	// server with one is paused, which from outside looks like a long run.
	Asking int `json:"asking,omitempty"`
	// SideRuns names the agents at work beside the main one.
	SideRuns []string `json:"sideRuns,omitempty"`
	// Children is the sub-agents the main agent called, so a reader outside
	// the process can draw the tree rather than one row per session.
	Children []AgentChild `json:"children,omitempty"`
	// Stacked is the queue in order. Queued is its length: a list that says
	// which session each turn is waiting on needs the turns, not a count.
	Stacked []AgentQueued `json:"stacked,omitempty"`
	// Sandbox is whether the agent runs in a container.
	Sandbox bool `json:"sandbox,omitempty"`
}

// AgentQueued is one turn waiting behind the run. No prompt: a row needs
// what a turn will do, not what it will say.
type AgentQueued struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	What string `json:"what,omitempty"`
}

// AgentChild is one sub-agent under the main one, as a list outside the
// session draws it.
type AgentChild struct {
	// Kind is "agent" for a sub-agent and "task" for a background command.
	Kind  string `json:"kind,omitempty"`
	Name  string `json:"name"`
	Doing string `json:"doing,omitempty"`
}

// Path is where the record lives for a checkout. A worktree has its own git dir,
// so each checkout gets its own server.
//
// The launcher serves no checkout and passes its own path instead, which is why
// everything below takes the path rather than working one out.
func Path(gitDir string) string {
	return filepath.Join(statedir.Git(gitDir), "server.json")
}

// Read loads the record. A missing or unreadable one reports absent rather than
// failing: the only sensible response either way is to start a server.
func Read(path string) (*Record, bool) {
	data, err := os.ReadFile(path) // #nosec G304 -- a path ade chose, not one from a request
	if err != nil {
		return nil, false
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil || rec.PID <= 0 || rec.URL == "" {
		return nil, false
	}
	return &rec, true
}

// HostGroupEnv names the group that may read a session's record besides its
// writer. A worker sets it: there ade is root in a container, the editor is not.
const HostGroupEnv = "ADE_HOST_GID"

// HostUserEnv names the user a sandbox should write as, for the same reason.
const HostUserEnv = "ADE_HOST_UID"

// Write records the running server.
func Write(path string, rec Record) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	share(dir, path)
	HandToHost(dir)
	return nil
}

// share lets the host's group into the state directory and read the record,
// and nothing else in it. A failure is logged, not returned: the session runs.
func share(dir, record string) {
	gid, err := strconv.Atoi(strings.TrimSpace(os.Getenv(HostGroupEnv)))
	if err != nil || gid < 0 {
		return
	}
	for path, mode := range map[string]os.FileMode{dir: 0o750, record: 0o640} {
		if err := os.Chown(path, -1, gid); err != nil {
			slog.Warn("could not share ade's state with the host's group", "path", path, "err", err)
			return
		}
		if err := os.Chmod(path, mode); err != nil { // #nosec G302 -- group read is the point; see HostGroupEnv
			slog.Warn("could not share ade's state with the host's group", "path", path, "err", err)
			return
		}
	}
}

// Remove clears the record. A missing file is success.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove session: %w", err)
	}
	return nil
}

// Live reports whether the recorded server is still there and still ours.
//
// Both checks are needed. A live PID alone proves nothing, since PIDs are reused
// and the record could name something else entirely; a health answer alone would
// accept whatever happens to hold the port now.
func Live(ctx context.Context, rec *Record) bool {
	_, ok := Probe(ctx, rec)
	return ok
}

// Probe is Live, and what the server said about itself when it is. The
// recorded URL carries the token, and the probe sends it, so the answer says
// what the agent is doing as well as whose the port is.
func Probe(ctx context.Context, rec *Record) (Health, bool) {
	if rec == nil || !running(rec.PID) {
		return Health{}, false
	}
	health, err := probe(ctx, rec.URL)
	if err != nil {
		return Health{}, false
	}
	if !health.Ade || health.PID != rec.PID || health.Root != rec.Root {
		return Health{}, false
	}
	return health, true
}

// Running reports whether a recorded process is still there, for a caller
// that has to tell "gone" from "did not answer".
func Running(pid int) bool { return running(pid) }

// running reports whether a process exists. Signal 0 checks for it without
// delivering anything.
func running(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func probe(ctx context.Context, recURL string) (Health, error) {
	base, err := baseOf(recURL)
	if err != nil {
		return Health{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	target := base + "/healthz"
	if parsed, err := url.Parse(recURL); err == nil {
		if token := parsed.Query().Get("t"); token != "" {
			target += "?t=" + url.QueryEscape(token)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return Health{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Health{}, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("health returned %s", res.Status)
	}

	var health Health
	if err := json.NewDecoder(res.Body).Decode(&health); err != nil {
		return Health{}, err
	}
	return health, nil
}

// baseOf strips the path and token from a recorded URL.
func baseOf(url string) (string, error) {
	const scheme = "http://"
	if len(url) <= len(scheme) || url[:len(scheme)] != scheme {
		return "", fmt.Errorf("not a ade url: %q", url)
	}
	rest := url[len(scheme):]
	for i, c := range rest {
		if c == '/' {
			return scheme + rest[:i], nil
		}
	}
	return scheme + rest, nil
}

// Stop asks the recorded server to shut down and waits for it to go.
func Stop(path string, wait time.Duration) (*Record, error) {
	rec, ok := Read(path)
	if !ok {
		return nil, errors.New("no ade is running for this repository")
	}
	proc, err := os.FindProcess(rec.PID)
	if err != nil {
		return rec, fmt.Errorf("find process %d: %w", rec.PID, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Already gone: clear the record rather than leaving it to confuse the
		// next run.
		_ = Remove(path)
		return rec, nil
	}

	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !running(rec.PID) {
			return rec, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return rec, fmt.Errorf("ade (pid %d) did not stop within %s", rec.PID, wait)
}

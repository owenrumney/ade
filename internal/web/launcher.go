package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/launcher"
	"github.com/owenrumney/ade/internal/session"
)

var launcherStartMu sync.Mutex

// throughLauncher is this session's address on the launcher — started if none
// is up — and whether a page is open on it.
func (s *Server) throughLauncher(ctx context.Context) (string, bool) {
	if s.noLauncher {
		return "", false
	}
	// Started whatever comes of it: the page moves itself over later.
	base, err := startLauncher(ctx)
	if err != nil {
		slog.Warn("could not start the launcher", "err", err)
		return "", false
	}
	watched := launcher.WatchedAt(ctx, base)
	go s.keepAnnouncing(ctx, s.railID())

	// The proxy address is only for a session that will keep its name: a
	// pending id is replaced the moment the agent names one, and the link dies.
	id := s.agent.Current()
	at, err := url.Parse(base)
	if id == "" || err != nil {
		return "", watched
	}
	return fmt.Sprintf("%s://%s/s/%s/?t=%s", at.Scheme, at.Host,
		url.PathEscape(id), url.QueryEscape(at.Query().Get("t"))), watched
}

// railID is what the launcher calls this server. The agent's session once it
// has named one, and until then a provisional id so the rail has a tile.
func (s *Server) railID() string {
	if id := s.agent.Current(); id != "" {
		return id
	}
	return s.pendingID
}

// announceTries and announceGap bound the retry. A session that never lands on
// the rail is invisible, so one dropped POST may not be the end of it.
const (
	announceTries = 10
	announceGap   = time.Second
)

// keepAnnouncing says it until the launcher takes it: the record it is checked
// against is written by another goroutine, so the first try can be early.
func (s *Server) keepAnnouncing(ctx context.Context, id string) {
	var err error
	for try := range announceTries {
		if err = s.announceServing(ctx, id); err == nil {
			return
		}
		if try == announceTries-1 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(announceGap):
		}
	}
	slog.Warn("the launcher never took this session, so it is off the rail",
		"id", id, "dir", s.startRoot, "port", s.listenPort, "err", err)
}

// announceServing tells the launcher which session this server holds: its scan
// only knows sessions that have left a transcript, and a new one has not.
func (s *Server) announceServing(ctx context.Context, id string) error {
	if s.noLauncher {
		return nil
	}
	if id == "" || s.listenPort == "" {
		return fmt.Errorf("nothing to announce: id %q, port %q", id, s.listenPort)
	}
	base := launcherURL()
	if base == "" {
		return errors.New("no launcher is recorded")
	}
	at, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("read the launcher's address: %w", err)
	}
	port, err := strconv.Atoi(s.listenPort)
	if err != nil {
		return fmt.Errorf("read this server's port: %w", err)
	}
	body, err := json.Marshal(map[string]any{"id": id, "dir": s.startRoot, "port": port})
	if err != nil {
		return err
	}
	ask, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, http.MethodPost,
		at.Scheme+"://"+at.Host+"/api/serving", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("tell the launcher what this serves: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return fmt.Errorf("the launcher refused this session: %s %s",
			res.Status, strings.TrimSpace(readErr(res.Body)))
	}
	return nil
}

// readErr is the message off a refusal, which is json with an error field.
func readErr(r io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(r, 1<<12))
	if err != nil {
		return ""
	}
	var shape struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &shape) == nil && shape.Error != "" {
		return shape.Error
	}
	return string(data)
}

func (s *Server) handleLauncher(w http.ResponseWriter, r *http.Request) {
	url, err := startLauncher(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

func startLauncher(ctx context.Context) (string, error) {
	launcherStartMu.Lock()
	defer launcherStartMu.Unlock()

	if url := launcherURL(); url != "" {
		return url, nil
	}
	home, err := launcher.Home()
	if err != nil {
		return "", err
	}
	record, err := launcher.RecordPath()
	if err != nil {
		return "", err
	}
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find the ade binary: %w", err)
	}
	log, err := os.OpenFile(filepath.Join(home, "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("open launcher log: %w", err)
	}
	defer func() { _ = log.Close() }()

	cmd := exec.Command(self, "launch", "--foreground", "--no-open") // #nosec G204 -- this binary, with fixed arguments
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start launcher: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return "", fmt.Errorf("release launcher: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := session.Read(record); ok && session.Running(rec.PID) {
			return rec.URL, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("launcher did not start; see %s", filepath.Join(home, "launcher.log"))
}

// askLauncher calls the launcher on this machine. The session knows how to
// reach it and holds its token; the browser holds neither.
var askLauncher = func(ctx context.Context, method, path, body string, wait time.Duration) ([]byte, error) {
	base := launcherURL()
	if base == "" {
		return nil, errors.New("no launcher is running on this machine")
	}
	at, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	ask, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, method, at.Scheme+"://"+at.Host+path, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		// The body goes back too: a hand-off that failed part way still says
		// where it left the session.
		return answer, errors.New(answerError(answer))
	}
	return answer, nil
}

// answerError is the message in ade's error shape, or the body as it stands.
func answerError(body []byte) string {
	var shape struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &shape); err == nil && strings.TrimSpace(shape.Error) != "" {
		return strings.TrimSpace(shape.Error)
	}
	return strings.TrimSpace(string(body))
}

// hostChoice is a machine as the page picks one. The launcher's own answer
// carries each worker's URL and token, which the page is not given.
type hostChoice struct {
	Name      string `json:"name"`
	Label     string `json:"label,omitempty"`
	Connected bool   `json:"connected"`
	Local     bool   `json:"local,omitempty"`
	State     string `json:"state"`
	Sessions  int    `json:"sessions"`
	Error     string `json:"error,omitempty"`
}

// handleHosts is the launcher's machine list, served to this session's page.
func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	hosts := []hostChoice{}
	answer, err := askLauncher(r.Context(), http.MethodGet, "/api/hosts", "", hostsWait)
	// An empty list, not an error: a session with no launcher can still build here.
	if err == nil {
		var got struct {
			Hosts []hostChoice `json:"hosts"`
		}
		if err := json.Unmarshal(answer, &got); err == nil && got.Hosts != nil {
			hosts = got.Hosts
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": hosts})
}

const hostsWait = 10 * time.Second

// record writes this server's record, with the launcher-proxied address when
// there is one: a reader on a worker's host cannot reach the container's port.
func (s *Server) record(proxy string) {
	s.mu.Lock()
	s.proxy = proxy
	s.mu.Unlock()
	rec := session.Record{
		PID: os.Getpid(), URL: s.direct, Root: s.startRoot,
		StartedAt: s.startedAt, Proxy: proxy,
	}
	for _, dir := range s.recordDirs() {
		if err := session.Write(session.Path(dir), rec); err != nil {
			slog.Warn("could not record this server", "dir", dir, "err", err)
		}
	}
}

// recordDirs is where this server is recorded: where it started, and the
// checkout it has moved into, which is the folder an editor opens.
func (s *Server) recordDirs() []string {
	dirs := []string{s.startGitDir}
	if ws := s.space(); ws != nil && ws.gitDir != "" && !sameFile(ws.gitDir, s.startGitDir) {
		dirs = append(dirs, ws.gitDir)
	}
	return dirs
}

// rerecord follows a move: the checkout left behind stops naming this server.
func (s *Server) rerecord(left string) {
	if s.direct == "" {
		return
	}
	if !slices.ContainsFunc(s.recordDirs(), func(d string) bool { return sameFile(d, left) }) {
		unrecord(left)
	}
	s.mu.RLock()
	proxy := s.proxy
	s.mu.RUnlock()
	s.record(proxy)
}

// unrecord removes this server's record from a git dir, unless another server
// has written its own there since.
func unrecord(gitDir string) {
	path := session.Path(gitDir)
	if rec, ok := session.Read(path); ok && rec.PID != os.Getpid() {
		return
	}
	if err := session.Remove(path); err != nil {
		slog.Debug("remove session record", "err", err)
	}
}

// reproxy writes the record again: the proxied path carries the session id,
// which a new conversation changes.
func (s *Server) reproxy(ctx context.Context) {
	if through, _ := s.throughLauncher(ctx); through != "" {
		s.record(through)
	}
}

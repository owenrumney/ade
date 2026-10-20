package launcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// hostAsk bounds one machine's answer: a host that has gone quiet must not
// hold up the list of sessions on this one.
const hostAsk = 3 * time.Second

// hostFresh is how long the last sweep stands, and hostQuiet how long a machine
// that did not answer is left alone: one dead forward would otherwise put
// hostAsk of latency on every poll of this machine's own rail.
const (
	hostFresh = time.Second
	hostQuiet = 15 * time.Second
)

// hostSessions is every connected machine's sessions, marked with where they
// are. No port: they are forwarded one to one, so none has to be remapped.
func (s *Server) hostSessions(ctx context.Context) []ActiveSession {
	s.mu.Lock()
	if s.hostRows != nil && time.Since(s.hostRowsAt) < hostFresh {
		rows := s.hostRows
		s.mu.Unlock()
		return rows
	}
	now := time.Now()
	live := make(map[string]string, len(s.hosts))
	for name, host := range s.hosts {
		if now.Before(s.quietUntil[name]) {
			continue
		}
		live[name] = host.url
	}
	s.mu.Unlock()
	if len(live) == 0 {
		return nil
	}

	var mu sync.Mutex
	out := []ActiveSession{}
	quiet := map[string]time.Time{}
	var wg sync.WaitGroup
	for name, url := range live {
		wg.Add(1)
		go func(name, url string) {
			defer wg.Done()
			rows, err := askServing(ctx, url)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				slog.Debug("could not list a host's sessions", "host", name, "err", err)
				quiet[name] = time.Now().Add(hostQuiet)
				return
			}
			for _, row := range rows {
				// A machine answers for itself, not for a third one.
				if row.Machine != "" {
					continue
				}
				row.Machine = name
				out = append(out, row)
			}
		}(name, url)
	}
	wg.Wait()

	sort.Slice(out, func(a, b int) bool {
		if out[a].Machine != out[b].Machine {
			return out[a].Machine < out[b].Machine
		}
		return out[a].Name < out[b].Name
	})
	s.mu.Lock()
	for name, until := range quiet {
		s.quietUntil[name] = until
	}
	s.hostRows, s.hostRowsAt = out, time.Now()
	s.mu.Unlock()
	return out
}

// openWait: a process launch behind an ssh forward, not a lookup.
const openWait = 30 * time.Second

const provisionWait = 2 * time.Minute

// readGitHubToken prefers ADE_GITHUB_TOKEN, which is where a scoped token goes:
// `gh auth token` is the whole gh login, every scope it was granted.
var readGitHubToken = func(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv("ADE_GITHUB_TOKEN")); token != "" {
		return token, nil
	}
	out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output()
	return strings.TrimSpace(string(out)), err
}

// readGitHubUser is the login the token belongs to. ghcr checks the pair for a
// private package, so a placeholder is not enough there.
var readGitHubUser = func(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "gh", "api", "user", "--jq", ".login").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// openOnHost asks the machine the session is on: only that launcher can read
// that checkout's token, and the port it names is open here too.
func (s *Server) openOnHost(w http.ResponseWriter, r *http.Request, req openRequest) {
	machine, dir, brief := req.Machine, req.Dir, req.Brief
	live := s.connection(machine)
	if live == nil {
		writeError(w, http.StatusBadRequest, "not connected to "+machine)
		return
	}
	branch := ""
	if !strings.HasPrefix(dir, "/") && !strings.HasPrefix(dir, "~") {
		token, err := readGitHubToken(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "read the session GitHub token: run gh auth login on this machine")
			return
		}
		request := map[string]string{"repository": dir, "token": token}
		if brief != nil {
			request["branch"] = brief.Name
		}
		asked, _ := json.Marshal(request)
		answer, err := sessions.AskFor(r.Context(), live.url, http.MethodPost, "/api/provision", string(asked), provisionWait)
		if err != nil {
			writeError(w, http.StatusBadGateway, "provision "+dir+" on "+machine+": "+err.Error())
			return
		}
		var cloned struct {
			Path   string `json:"path"`
			Branch string `json:"branch"`
		}
		if err := json.Unmarshal(answer, &cloned); err != nil || cloned.Path == "" {
			writeError(w, http.StatusBadGateway, machine+" did not report the cloned checkout")
			return
		}
		dir, branch = cloned.Path, cloned.Branch
	}
	on := req
	on.Machine, on.Dir = "", dir
	body, err := json.Marshal(on)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A brief is pushed and built inside that request: two more calls than a
	// process launch, and giving up early leaves a session running over there.
	wait := openWait
	if brief != nil {
		wait += 2 * firstMessageWait
	}
	answer, err := sessions.AskFor(r.Context(), live.url, http.MethodPost, "/api/open", string(body), wait)
	if err != nil {
		// It may have opened the session before it failed, and that address is
		// the only one the caller will get for it.
		failed := map[string]any{"error": err.Error()}
		// The worktree is made before the brief is pushed, so a failed brief
		// still names the folder a window opens on.
		if at, made := openedAt(answer); at != "" {
			waitForward(r.Context(), at)
			failed["url"] = at
			if made != "" {
				failed["path"] = made
			}
		}
		writeJSON(w, http.StatusBadGateway, failed)
		return
	}
	var res struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.URL == "" {
		writeError(w, http.StatusBadGateway, machine+" did not say where it got to")
		return
	}
	// A named session is a worktree over there, and that is the folder a window
	// opens on. Only the plain open has nothing to report but the checkout.
	if res.Path != "" {
		dir = res.Path
	}
	// A forwarded port or nothing: the caller turns this into an origin, and a
	// machine naming anywhere else is not naming a forward.
	if !loopbackURL(res.URL) {
		writeError(w, http.StatusBadGateway, machine+" named an address that is not a forwarded port")
		return
	}
	waitForward(r.Context(), res.URL)
	// dir is that machine's own path, cloned or not: what a window opens on.
	opened := map[string]any{"url": res.URL, "path": dir}
	if branch != "" {
		opened["branch"] = branch
	}
	writeJSON(w, http.StatusOK, opened)
}

// scratchOnHost asks the selected backend to create its own scratch checkout.
func (s *Server) scratchOnHost(w http.ResponseWriter, r *http.Request, req scratchRequest) {
	machine := req.Machine
	live := s.connection(machine)
	if live == nil {
		writeError(w, http.StatusBadRequest, "not connected to "+machine)
		return
	}
	// The whole request, machine cleared so it cannot loop: every default the
	// screen showed has to reach the machine that acts on it.
	on := req
	on.Machine = ""
	body, err := json.Marshal(on)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	answer, err := sessions.AskFor(r.Context(), live.url, http.MethodPost, "/api/scratch", string(body), openWait)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var res struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.URL == "" || res.Path == "" {
		writeError(w, http.StatusBadGateway, machine+" did not say where it made the scratch checkout")
		return
	}
	if !loopbackURL(res.URL) {
		writeError(w, http.StatusBadGateway, machine+" named an address that is not a forwarded port")
		return
	}
	waitForward(r.Context(), res.URL)
	writeJSON(w, http.StatusOK, map[string]any{"url": res.URL, "path": res.Path})
}

// openedAt is the session address in an answer, and the worktree it got to: a
// forwarded port or nothing.
func openedAt(answer []byte) (string, string) {
	var res struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.URL == "" || !loopbackURL(res.URL) {
		return "", ""
	}
	return res.URL, res.Path
}

func loopbackURL(address string) bool {
	parsed, err := neturl.Parse(address)
	if err != nil {
		return false
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		return ip.IsLoopback()
	}
	return parsed.Hostname() == "localhost"
}

// forwardWait bounds waiting for the forward to the port the host named. The
// far launcher asked for it on its port stream, which arrives separately.
const forwardWait = 5 * time.Second

// waitForward holds the answer back until something is listening here, so the
// page does not navigate to a port the ssh forward has not reached yet.
func waitForward(ctx context.Context, address string) {
	parsed, err := neturl.Parse(address)
	if err != nil || parsed.Host == "" {
		return
	}
	until := time.Now().Add(forwardWait)
	for time.Now().Before(until) {
		if open, err := net.DialTimeout("tcp", parsed.Host, 300*time.Millisecond); err == nil {
			_ = open.Close()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func askServing(ctx context.Context, launcherURL string) ([]ActiveSession, error) {
	// local=1: that machine's own hosts are its business, and two that name
	// each other would otherwise ask in a circle.
	answer, err := sessions.AskFor(ctx, launcherURL, http.MethodGet, "/api/serving?local=1", "", hostAsk)
	if err != nil {
		return nil, err
	}
	var rows []ActiveSession
	if err := json.Unmarshal(answer, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// MachineRepos is one connected machine's checkouts, named by the machine they
// are on. Recent sessions are drawn from this.
type MachineRepos struct {
	Machine string          `json:"machine"`
	Label   string          `json:"label,omitempty"`
	Repos   []sessions.Repo `json:"repos"`
}

// hostReposAsk is longer than hostAsk: this is a scan of the other machine's
// roots, not a reading it already holds.
const hostReposAsk = 10 * time.Second

// hostReposFresh is how long a sweep stands: the dash reloads on every
// launcher event, and none is about another machine's checkouts.
const hostReposFresh = 5 * time.Second

// hostRepos is every connected machine's checkouts. One that did not answer
// goes quiet on the timer the session sweep uses.
func (s *Server) hostRepos(ctx context.Context) []MachineRepos {
	s.mu.Lock()
	if s.hostCheckouts != nil && time.Since(s.hostCheckoutsAt) < hostReposFresh {
		out := s.hostCheckouts
		s.mu.Unlock()
		return out
	}
	now := time.Now()
	live := map[string]string{}
	for name, host := range s.hosts {
		if now.Before(s.quietUntil[name]) {
			continue
		}
		live[name] = host.url
	}
	s.mu.Unlock()
	if len(live) == 0 {
		return []MachineRepos{}
	}

	labels := map[string]string{}
	if cfg, err := settings.Load(); err == nil {
		for _, host := range cfg.Hosts {
			labels[host.Name] = host.Label
		}
	}

	var mu sync.Mutex
	out := []MachineRepos{}
	quiet := map[string]time.Time{}
	var wg sync.WaitGroup
	for name, url := range live {
		wg.Add(1)
		go func(name, url string) {
			defer wg.Done()
			repos, err := askRepos(ctx, url)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				slog.Debug("could not list a host's checkouts", "host", name, "err", err)
				quiet[name] = time.Now().Add(hostQuiet)
				return
			}
			out = append(out, MachineRepos{Machine: name, Label: labels[name], Repos: repos})
		}(name, url)
	}
	wg.Wait()

	sort.Slice(out, func(a, b int) bool { return out[a].Machine < out[b].Machine })
	s.mu.Lock()
	for name, until := range quiet {
		s.quietUntil[name] = until
	}
	s.hostCheckouts, s.hostCheckoutsAt = out, time.Now()
	s.mu.Unlock()
	return out
}

func askRepos(ctx context.Context, launcherURL string) ([]sessions.Repo, error) {
	answer, err := sessions.AskFor(ctx, launcherURL, http.MethodGet, "/api/repos", "", hostReposAsk)
	if err != nil {
		return nil, err
	}
	var body struct {
		Repos []sessions.Repo `json:"repos"`
	}
	if err := json.Unmarshal(answer, &body); err != nil {
		return nil, err
	}
	return body.Repos, nil
}

// tellHost stops or cancels an ade on a worker. The checkout is checked
// there: this machine has no such path.
func (s *Server) tellHost(w http.ResponseWriter, r *http.Request, machine, dir, path, verb string) {
	live := s.connection(machine)
	if live == nil {
		writeError(w, http.StatusBadRequest, "not connected to "+machine)
		return
	}
	body, err := json.Marshal(map[string]string{"dir": dir})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := sessions.AskFor(r.Context(), live.url, http.MethodPost, path, string(body), openWait); err != nil {
		writeError(w, http.StatusBadGateway, verb+" that session on "+machine+": "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// graphOnHost is a worker's graph, asked of the worker: only it can probe
// the servers it runs.
func (s *Server) graphOnHost(w http.ResponseWriter, r *http.Request, machine string) {
	live := s.connection(machine)
	if live == nil {
		writeError(w, http.StatusBadRequest, "not connected to "+machine)
		return
	}
	answer, err := sessions.AskFor(r.Context(), live.url, http.MethodGet, "/api/graph", "", openWait)
	if err != nil {
		writeError(w, http.StatusBadGateway, "list what is running on "+machine+": "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(answer)
}

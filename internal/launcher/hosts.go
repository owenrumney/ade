package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/models"
	"github.com/owenrumney/ade/internal/remote"
	"github.com/owenrumney/ade/internal/roles"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// connectTimeout bounds dialling, the capability check and starting the launcher
// over there. The page is waiting on all three.
const connectTimeout = 90 * time.Second

// streamWait bounds getting the port stream's headers. The body then stays
// open for the life of the connection, so this is not a read deadline.
const streamWait = 20 * time.Second

// hostView is one machine on the list, as the page sees it.
type hostView struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	// Connected says the master is up and its ports are being followed. URL is
	// where that machine's launcher is, reachable here through the tunnel.
	Connected bool   `json:"connected"`
	URL       string `json:"url,omitempty"`
	// Local is this machine. It is in the same list because a session picks a
	// machine and this is one, but it cannot be connected or disconnected.
	Local bool `json:"local,omitempty"`
	// State is connected, quiet (up, but its last answer failed), or off.
	// Sessions is how many ades it is serving; Error what the last connect said.
	State    string `json:"state"`
	Sessions int    `json:"sessions"`
	Error    string `json:"error,omitempty"`
	// Warning is a connect that worked on an old worker image.
	Warning string `json:"warning,omitempty"`
	// Both come from the server: the work outlives the request for it.
	Working string `json:"working,omitempty"`
	Failed  string `json:"failed,omitempty"`
}

const (
	hostConnected  = "connected"
	hostQuietState = "quiet"
	hostOff        = "off"
)

// conn is one dialled host: the ssh master, the forwards held open over it,
// and the goroutine following that machine's port stream.
type conn struct {
	host *remote.Host
	fwd  *remote.Forwarder
	// url is where it is reachable from here, not the port it serves on.
	url  string
	stop context.CancelFunc

	// cmu holds everything below: the port stream and a window resolving an
	// authority both change what is wanted.
	cmu sync.Mutex
	// launcher is where this machine reaches that one's launcher, ports what
	// it last published, and editor its editor server. Zero until one asks.
	launcher     remote.Mapping
	ports        []int
	editor       remote.Mapping
	editorCommit string
	// What `uname` said. A machine does not change arch.
	system, machine string
}

// want is the whole forwarded set, rebuilt rather than patched: the port
// stream publishes the whole set every frame.
func (c *conn) want() []remote.Mapping {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return mappings(c.ports, c.launcher, c.editor)
}

func (c *conn) setPorts(ports []int) {
	c.cmu.Lock()
	c.ports = ports
	c.cmu.Unlock()
}

func (c *conn) setEditor(mapping remote.Mapping, commit string) {
	c.cmu.Lock()
	c.editor, c.editorCommit = mapping, commit
	c.cmu.Unlock()
}

// editorServer is the forward and the build running there, or a zero mapping
// when no window ever asked for one.
func (c *conn) editorServer() (remote.Mapping, string) {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	return c.editor, c.editorCommit
}

// arch is the host's own names for the tarball it can run.
func (c *conn) arch(ctx context.Context) (string, string, error) {
	c.cmu.Lock()
	system, machine := c.system, c.machine
	c.cmu.Unlock()
	if system != "" {
		return system, machine, nil
	}
	system, machine, err := c.host.Arch(ctx)
	if err != nil {
		return "", "", err
	}
	c.cmu.Lock()
	c.system, c.machine = system, machine
	c.cmu.Unlock()
	return system, machine, nil
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[string]int{}
	for _, row := range s.activeSessions(r.Context()) {
		counts[row.Machine]++
	}
	// This machine leads the list: it is the one always available, and the
	// ordering says so without a separate control.
	out := []hostView{{
		Name: remote.LocalName, Label: "This Mac", Connected: true, Local: true,
		State: hostConnected, Sessions: counts[""],
	}}
	now := time.Now()
	for _, host := range cfg.Hosts {
		name := strings.TrimSpace(host.Name)
		if name == "" {
			continue
		}
		view := hostView{Name: name, Label: host.Label, State: hostOff, Sessions: counts[name]}
		s.mu.Lock()
		live, quiet, fault := s.hosts[name], s.quietUntil[name], s.hostErrors[name]
		s.mu.Unlock()
		if live != nil {
			view.Connected, view.URL, view.State = true, live.url, hostConnected
			if stale := live.host.Stale(); stale != nil {
				view.Warning = stale.Error()
			}
			if now.Before(quiet) {
				view.State = hostQuietState
			}
		}
		view.Error = fault
		view.Working, view.Failed = s.workingOn(name), s.workFailure(name)
		out = append(out, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": out})
}

// noteHost keeps what connecting to a machine last said, so the list can show
// why one is off rather than leaving it a silent button.
func (s *Server) noteHost(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		delete(s.hostErrors, name)
		return
	}
	s.hostErrors[name] = err.Error()
}

// handleConnect brings a machine up and hands back its launcher's URL. The
// same move as opening a checkout, one level higher — except the somewhere is
// another machine, so a tunnel has to exist first.
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}

	// Only a machine on the list. The name is handed to ssh, so it has to be
	// one that was offered rather than one off a request.
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	host, ok := cfg.Host(req.Host)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of your hosts; add it under Settings → Hosts")
		return
	}

	if live := s.connection(host.Name); live != nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": live.url})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), connectTimeout)
	defer cancel()
	url, err := s.connect(ctx, host.Name)
	s.noteHost(host.Name, err)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// handleHostRepos is what the connected machines have checked out. Nothing is
// dialled for it: a machine that is not already connected is not on this list.
func (s *Server) handleHostRepos(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hosts": s.hostRepos(r.Context())})
}

// handleTarget returns the choices from one explicitly selected machine. The
// browser stays on this launcher; only its selected session is remote.
func (s *Server) handleTarget(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Machine string `json:"machine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	machine := strings.TrimSpace(req.Machine)
	if machine == "" {
		repos, err := s.repos()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"repos": repos, "models": models.Load().Models})
		return
	}

	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, ok := cfg.Host(machine); !ok {
		writeError(w, http.StatusBadRequest, "that is not one of your hosts; add it under Settings → Hosts")
		return
	}
	live := s.connection(machine)
	if live == nil {
		if _, err := s.Connect(r.Context(), machine); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		live = s.connection(machine)
	}
	if live == nil {
		writeError(w, http.StatusBadGateway, "could not connect to "+machine)
		return
	}

	answer, err := sessions.AskFor(r.Context(), live.url, http.MethodGet, "/api/repos", "", openWait)
	if err != nil {
		writeError(w, http.StatusBadGateway, "read repositories from "+machine+": "+err.Error())
		return
	}
	var repos struct {
		Repos []sessions.Repo `json:"repos"`
	}
	if err := json.Unmarshal(answer, &repos); err != nil {
		writeError(w, http.StatusBadGateway, machine+" did not return its repositories")
		return
	}
	answer, err = sessions.AskFor(r.Context(), live.url, http.MethodGet, "/api/models", "", openWait)
	if err != nil {
		writeError(w, http.StatusBadGateway, "read models from "+machine+": "+err.Error())
		return
	}
	var catalogue struct {
		Models []models.Model `json:"models"`
	}
	if err := json.Unmarshal(answer, &catalogue); err != nil {
		writeError(w, http.StatusBadGateway, machine+" did not return its models")
		return
	}
	if local, err := s.repos(); err == nil {
		repos.Repos = append(repos.Repos, laptopCatalogue(r.Context(), local, repos.Repos)...)
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos.Repos, "models": catalogue.Models})
}

// originWait bounds one `git remote get-url`. A checkout on a stalled network
// mount must not hold up the picker the rest of the list is for.
const originWait = 2 * time.Second

var readOrigin = func(ctx context.Context, dir string) (string, error) {
	ask, cancel := context.WithTimeout(ctx, originWait)
	defer cancel()
	out, err := exec.CommandContext(ask, "git", "-C", dir, "remote", "get-url", "origin").Output() // #nosec G204 -- scanned checkout
	return strings.TrimSpace(string(out)), err
}

// laptopCatalogue offers this Mac's repositories to a worker as clone URLs.
// One the worker already holds is dropped, so no name appears twice.
// originsFresh is how long a checkout's origin stands. Picking a machine reads
// one per laptop checkout, and a laptop has hundreds; they change rarely.
const originsFresh = 10 * time.Minute

type originAt struct {
	url string
	at  time.Time
}

//nolint:gochecknoglobals // a process-wide cache, not configuration
var (
	originsMu   sync.Mutex
	originsSeen = map[string]originAt{}
)

// cachedOrigin is readOrigin with the fork skipped when it was asked recently.
// Without it, picking a worker costs a git process per checkout, every time.
func cachedOrigin(ctx context.Context, dir string) string {
	originsMu.Lock()
	held, ok := originsSeen[dir]
	originsMu.Unlock()
	if ok && time.Since(held.at) < originsFresh {
		return held.url
	}
	url, _ := readOrigin(ctx, dir)
	originsMu.Lock()
	originsSeen[dir] = originAt{url: url, at: time.Now()}
	originsMu.Unlock()
	return url
}

func laptopCatalogue(ctx context.Context, repos, already []sessions.Repo) []sessions.Repo {
	there := make(map[string]bool, len(already))
	for _, repo := range already {
		there[strings.ToLower(repo.Name)] = true
	}
	// One git process per checkout, and a laptop has hundreds: concurrent, or
	// picking a host costs a fork apiece in series.
	found := make([]string, len(repos))
	var wg sync.WaitGroup
	tokens := make(chan struct{}, max(runtime.NumCPU(), 2))
	for i, repo := range repos {
		if there[strings.ToLower(repo.Name)] {
			continue
		}
		wg.Add(1)
		go func(i int, dir string) {
			defer wg.Done()
			tokens <- struct{}{}
			defer func() { <-tokens }()
			found[i] = cachedOrigin(ctx, dir)
		}(i, repo.Path)
	}
	wg.Wait()

	out := make([]sessions.Repo, 0, len(repos))
	for i, repo := range repos {
		if found[i] == "" {
			continue
		}
		out = append(out, sessions.Repo{Path: found[i], Name: repo.Name, Owner: repo.Owner, Branch: repo.Branch})
	}
	return out
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	s.disconnect(strings.TrimSpace(req.Host))
	w.WriteHeader(http.StatusNoContent)
}

// handleWorkerStop takes a connected machine's worker down, sessions and all,
// then drops the tunnel: there is nothing left over there to follow.
func (s *Server) handleWorkerStop(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	name := strings.TrimSpace(req.Host)
	s.start(w, r, name, workerAction{
		Verb: workStopping,
		Wait: openWait,
		Do: func(ctx context.Context, live *conn) error {
			if err := live.host.StopWorker(ctx); err != nil {
				return err
			}
			s.disconnect(name)
			return nil
		},
	})
}

// handleWorkerCredentials sends this laptop's provider logins to a connected
// worker now, rather than at the next connect. The newer copy still wins.
func (s *Server) handleWorkerCredentials(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	name := strings.TrimSpace(req.Host)
	s.start(w, r, name, workerAction{
		Verb: workSyncing,
		Wait: openWait,
		Do: func(ctx context.Context, live *conn) error {
			roots, err := remote.DetectRoots(ctx, live.host)
			if err != nil {
				return err
			}
			if err := live.host.PrepareCredentials(ctx, roots, localCredentials(ctx)); err != nil {
				return err
			}
			if err := live.host.PreparePrompt(ctx, localPrompt(live.host.Name)); err != nil {
				slog.Warn("the worker keeps the prompt it had", "host", live.host.Name, "err", err)
			}
			return nil
		},
	})
}

// updateWait bounds an update. It is a registry pull over someone else's
// network, then a worker start, so it is minutes rather than seconds.
const updateWait = 10 * time.Minute

// retryWait is how long an update waits before its second connect. A var so a
// test does not sit through it.
var retryWait = 2 * time.Second

// handleWorkerUpdate pulls the images this release runs, and replaces the
// worker only when it is behind: one already current keeps its sessions.
func (s *Server) handleWorkerUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	name := strings.TrimSpace(req.Host)
	s.start(w, r, name, workerAction{
		Verb: workUpdating,
		Wait: updateWait,
		Do:   func(ctx context.Context, live *conn) error { return s.update(ctx, name, live) },
	})
}

func (s *Server) update(ctx context.Context, name string, live *conn) error {
	roots, err := remote.DetectRoots(ctx, live.host)
	if err != nil {
		return err
	}
	token, err := readGitHubToken(ctx)
	if err != nil {
		return errors.New("read the GitHub token: run gh auth login on this machine")
	}
	behind, err := live.host.RefreshImages(ctx, roots, remote.Login{User: readGitHubUser(ctx), Token: token})
	if err != nil || !behind {
		return err
	}
	if err := live.host.StopWorker(ctx); err != nil {
		return err
	}
	s.disconnect(name)
	_, err = s.connect(ctx, name)
	if err != nil {
		// The worker is stopped by now, so one dropped connection must not be the
		// end of it: the second try is what puts the machine back.
		select {
		case <-ctx.Done():
		case <-time.After(retryWait):
			_, err = s.connect(ctx, name)
		}
	}
	s.noteHost(name, err)
	if err != nil {
		return fmt.Errorf("%s: its worker is stopped; press Connect to start it again", err.Error())
	}
	return nil
}

// Connect brings a host up outside a request, for `ade launch --host`.
func (s *Server) Connect(ctx context.Context, name string) (string, error) {
	cfg, err := settings.Load()
	if err != nil {
		return "", err
	}
	host, ok := cfg.Host(name)
	if !ok {
		return "", fmt.Errorf("%s is not one of your hosts; add it under Settings → Hosts, or to %s", name, settingsPath())
	}
	if live := s.connection(host.Name); live != nil {
		return live.url, nil
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	url, err := s.connect(ctx, host.Name)
	s.noteHost(host.Name, err)
	return url, err
}

func (s *Server) connect(ctx context.Context, name string) (string, error) {
	host, err := remote.New(name)
	if err != nil {
		return "", err
	}
	if err := host.Dial(ctx); err != nil {
		return "", err
	}
	roots, err := remote.DetectRoots(ctx, host)
	if err != nil {
		_ = host.Close()
		return "", err
	}
	if err := remote.Check(ctx, host, roots); err != nil {
		_ = host.Close()
		return "", err
	}
	if err := host.PrepareCredentials(ctx, roots, localCredentials(ctx)); err != nil {
		_ = host.Close()
		return "", err
	}
	// The hub's configuration and the agents' server list go over before the
	// hub starts; neither failing is a reason to refuse the machine.
	if err := host.PrepareHub(ctx, roots, localHubState()); err != nil {
		slog.Warn("the worker's hub starts without this laptop's servers", "host", name, "err", err)
	}

	// Not StablePort("launcher"): that is the port a native ade binds, and a
	// worker machine may be running one. -p loses to it without failing.
	port := httpx.StablePort("ade-worker")
	githubToken, err := readGitHubToken(ctx)
	if err != nil {
		slog.Warn("no GitHub token for the worker; it will not be able to clone", "host", name, "err", err)
	}
	login := remote.Login{User: readGitHubUser(ctx), Token: githubToken}
	url, err := host.StartWorker(ctx, roots, port, login)
	if err != nil {
		_ = host.Close()
		return "", err
	}
	if stale := host.Stale(); stale != nil {
		slog.Warn("the worker runs the image it already had", "host", name, "err", stale)
	}
	// After the worker: its HOME is inside the container, and only the worker
	// can write there.
	if err := host.PrepareMCPServers(ctx, localMCPServers()); err != nil {
		slog.Warn("the worker's agents get no mcp servers", "host", name, "err", err)
	}
	// A session there without the role is worse than one with it, and better
	// than no machine at all, so this never refuses the connection.
	if err := host.PreparePrompt(ctx, localPrompt(name)); err != nil {
		slog.Warn("the worker starts without this machine's prompt", "host", name, "err", err)
	}
	if err := host.PrepareEditorConfig(ctx, localEditorConfig()); err != nil {
		slog.Warn("the worker's editor pane starts without your configuration", "host", name, "err", err)
	}
	if err := host.PrepareGitConfig(ctx, localGitConfigs(), localHome(), localScanRoot(), roots.Work); err != nil {
		slog.Warn("the worker commits without your git configuration", "host", name, "err", err)
	}
	if err := host.PrepareGitIdentity(ctx, localGitIdentity(ctx)); err != nil {
		slog.Warn("the worker cannot commit as you; git will refuse an unnamed author", "host", name, "err", err)
	}
	if err := host.PrepareGitCredentials(ctx); err != nil {
		slog.Warn("the worker cannot push; git will ask for a login nobody can type", "host", name, "err", err)
	}
	if err := host.PrepareModels(ctx, localModelCatalogue()); err != nil {
		slog.Warn("the worker keeps its own model list; a model synced here may be refused there", "host", name, "err", err)
	}
	if err := host.PrepareAgents(ctx); err != nil {
		slog.Warn("no named agents on the worker; @security and the rest will not answer", "host", name, "err", err)
	}
	// Before the hub, which mounts them. Failing is not refusing the machine.
	cloud := localCloud(name)
	if err := host.PrepareCloud(ctx, roots, cloud); err != nil {
		slog.Warn("the worker's hub starts without your cloud logins", "host", name, "err", err)
		cloud = remote.Cloud{}
	}
	if err := host.StartHub(ctx, roots, login, cloud); err != nil {
		slog.Warn("no mcp hub on the worker; its agents have no mcp servers", "host", name, "err", err)
	}
	if !strings.HasPrefix(url, "http://") || port <= 0 {
		_ = host.Close()
		return "", fmt.Errorf("ade on %s did not report a url", name)
	}

	// Every launcher binds the same port, so ours is always on top of theirs.
	mapping := remote.Mapping{Local: localLauncherPort(name), Remote: port}
	fwd := remote.NewForwarder(host)
	if err := fwd.Sync(ctx, []remote.Mapping{mapping}); err != nil {
		_ = host.Close()
		return "", err
	}
	url = reachableAt(url, mapping.Local)

	// Opened here, not in the goroutine: a stream that fails must fail the
	// connect rather than silently drop the forward the page was sent through.
	// On the launcher's context, or returning from this request cancels it.
	follow, stop := context.WithCancel(s.ctx)
	stream, err := portsStream(follow, url)
	if err != nil {
		stop()
		_ = fwd.Close(ctx)
		_ = host.Close()
		return "", fmt.Errorf("%s answered, but its port stream did not: %w", name, err)
	}

	live := &conn{host: host, fwd: fwd, url: url, stop: stop, launcher: mapping}
	s.mu.Lock()
	s.hosts[name] = live
	s.mu.Unlock()

	go s.follow(follow, name, live, stream)
	return url, nil
}

// localEditorConfig is this laptop's ~/.config/nvim, or nothing. A config that
// cannot be read is not a reason to refuse the machine.
// localGitConfigs are this machine's ~/.gitconfig and the files it includes,
// which is where a per-directory identity lives.
func localGitConfigs() []remote.GitConfig {
	home := localHome()
	if home == "" {
		return nil
	}
	names, err := filepath.Glob(filepath.Join(home, ".gitconfig*"))
	if err != nil {
		return nil
	}
	var out []remote.GitConfig
	for _, path := range names {
		data, err := os.ReadFile(path) // #nosec G304 -- this machine's own git configuration
		if err != nil {
			continue
		}
		out = append(out, remote.GitConfig{Name: filepath.Base(path), Data: data})
	}
	return out
}

func localHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// localScanRoot is the directory this machine keeps checkouts under, which is
// what an includeIf rule is written against. The first, when there are several.
func localScanRoot() string {
	cfg, err := settings.Load()
	if err != nil {
		return ""
	}
	if roots := cfg.Roots(); len(roots) > 0 {
		return roots[0]
	}
	return ""
}

// localGitIdentity is who this machine commits as, for the worker to match.
// The signing key rides along only when this machine signs with ssh: a gpg
// keyring is a different shape and is not sent.
var localGitIdentity = func(ctx context.Context) remote.Identity {
	read := func(key string) string {
		out, err := exec.CommandContext(ctx, "git", "config", "--get", key).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	who := remote.Identity{Name: read("user.name"), Email: read("user.email")}
	if read("commit.gpgsign") != "true" || read("gpg.format") != "ssh" {
		return who
	}
	pub := expandHome(read("user.signingkey"))
	if pub == "" {
		return who
	}
	who.SigningPub, _ = os.ReadFile(pub)                             // #nosec G304 -- the path this machine signs with
	who.SigningKey, _ = os.ReadFile(strings.TrimSuffix(pub, ".pub")) // #nosec G304 -- its private half
	who.KnownHosts = githubHostKeys(ctx)
	return who
}

// githubHostKeys is what this machine already trusts for github.com. Taken
// from here rather than scanned over there, which would trust whatever answers.
func githubHostKeys(ctx context.Context) []byte {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out, err := exec.CommandContext(ctx, "ssh-keygen", "-F", "github.com",
		"-f", filepath.Join(home, ".ssh", "known_hosts")).Output()
	if err != nil {
		return nil
	}
	var kept []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return nil
	}
	return []byte(strings.Join(kept, "\n") + "\n")
}

// expandHome resolves a leading ~, which git config stores literally.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, path[2:])
}

func localEditorConfig() []byte {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	config, err := remote.EditorConfig(home)
	if err != nil {
		slog.Warn("could not read your editor configuration", "err", err)
		return nil
	}
	return config
}

// localModelCatalogue is the synced list as it sits on disk, or nothing when
// this machine has never synced: the worker's built-in list is then the same.
func localModelCatalogue() []byte {
	path, err := models.Path()
	if err != nil {
		return nil
	}
	body, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return nil
	}
	return body
}

// PushModels hands the catalogue to every connected worker, so a sync here
// reaches the machines a session can start on.
func (s *Server) PushModels(ctx context.Context) {
	catalogue := localModelCatalogue()
	if len(catalogue) == 0 {
		return
	}
	s.mu.Lock()
	live := make(map[string]*conn, len(s.hosts))
	for name, one := range s.hosts {
		live[name] = one
	}
	s.mu.Unlock()
	for name, one := range live {
		if err := one.host.PrepareModels(ctx, catalogue); err != nil {
			slog.Warn("could not send the synced model list to the worker", "host", name, "err", err)
		}
	}
}

// localPrompt is how this machine says work should be done: the role picked
// here and the standing instruction typed here. A worker has neither of its own.
func localPrompt(host string) remote.Prompt {
	cfg, _ := settings.Load()
	out := remote.Prompt{Instructions: cfg.Instructions, Dashboard: workerDashboard(cfg, host),
		DesignsRemote: cfg.DesignsRemote}
	list, err := roles.Load()
	if err != nil {
		return out
	}
	for _, role := range list {
		if role.Name == cfg.DefaultRole {
			out.RoleName, out.Role = role.Name, role.Prompt
			break
		}
	}
	return out
}

// workerDashboard is this machine's kiosk, under the worker's name: one screen
// for every machine, and a worker reporting as this one would merge the two.
func workerDashboard(cfg settings.Settings, host string) settings.Dashboard {
	out := cfg.Dashboard
	if !out.On() {
		return settings.Dashboard{}
	}
	out.Host = dashboardName(cfg, host)
	return out
}

// dashboardName is what the kiosk calls that machine: the label you gave it,
// or the ssh name without the login, which is what the Machines tree shows.
func dashboardName(cfg settings.Settings, host string) string {
	host = strings.TrimSpace(host)
	if found, ok := cfg.Host(host); ok && strings.TrimSpace(found.Label) != "" {
		return strings.TrimSpace(found.Label)
	}
	if _, after, found := strings.Cut(host, "@"); found && after != "" {
		return after
	}
	return host
}

func localCredentials(ctx context.Context) remote.Credentials {
	home, err := os.UserHomeDir()
	if err != nil {
		return remote.Credentials{}
	}
	var out remote.Credentials
	if path, err := sandbox.Credentials(ctx, home); err == nil {
		out.Claude, out.ClaudeAt = readStamped(path)
	}
	out.Codex, out.CodexAt = readStamped(filepath.Join(home, ".codex", "auth.json"))
	return out
}

// localCloud is read only for a host whose bookmark asked for it.
func localCloud(name string) remote.Cloud {
	cfg, _ := settings.Load()
	wanted := false
	for _, host := range cfg.Hosts {
		if host.Name == name {
			wanted = host.ShipCloudCredentials
			break
		}
	}
	if !wanted {
		return remote.Cloud{}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return remote.Cloud{}
	}
	var out remote.Cloud
	out.Profile = strings.TrimSpace(os.Getenv("AWS_PROFILE"))
	out.AWSConfig, out.At = readStamped(filepath.Join(home, ".aws", "config"))
	creds, at := readStamped(filepath.Join(home, ".aws", "credentials"))
	out.AWSCredentials = creds
	if at.After(out.At) {
		out.At = at
	}
	kube, at := readStamped(filepath.Join(home, ".kube", "config"))
	out.Kubeconfig = kube
	if at.After(out.At) {
		out.At = at
	}
	cache := filepath.Join(home, ".aws", "sso", "cache")
	entries, err := os.ReadDir(cache)
	if err != nil {
		return out
	}
	out.SSOCache = map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, at := readStamped(filepath.Join(cache, entry.Name()))
		if len(data) == 0 {
			continue
		}
		out.SSOCache[entry.Name()] = data
		if at.After(out.At) {
			out.At = at
		}
	}
	return out
}

// readStamped is a credential and when it was written: the worker compares that
// against its own copy rather than overwriting a token refreshed there.
func readStamped(path string) ([]byte, time.Time) {
	data, err := os.ReadFile(path) // #nosec G304 -- a credential path built here
	if err != nil {
		return nil, time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return data, time.Time{}
	}
	return data, info.ModTime()
}

// portsStream opens the far side's port stream and checks it answered. The
// deadline is on the headers alone: the body is meant to stay open for as long
// as the host is connected.
func portsStream(ctx context.Context, launcherURL string) (*http.Response, error) {
	target, err := portsURL(launcherURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: streamWait}}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		_ = res.Body.Close()
		return nil, fmt.Errorf("it answered %s", res.Status)
	}
	return res, nil
}

// follow keeps this machine's forwards in step with that one's. The stream
// ending is how a remote launcher that idled out says so, and the connection
// goes with it.
func (s *Server) follow(ctx context.Context, name string, live *conn, stream *http.Response) {
	defer s.disconnect(name)
	if err := s.readPorts(ctx, live, stream); err != nil && ctx.Err() == nil {
		slog.Info("stopped following a host", "host", name, "err", err)
	}
}

func (s *Server) readPorts(ctx context.Context, live *conn, res *http.Response) error {
	defer func() { _ = res.Body.Close() }()

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var frame struct {
			Ports []int `json:"ports"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			continue
		}
		live.setPorts(frame.Ports)
		if err := live.fwd.Sync(ctx, live.want()); err != nil {
			slog.Warn("could not forward every port", "err", err)
		}
	}
	return scanner.Err()
}

func (s *Server) connection(name string) *conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hosts[name]
}

func (s *Server) disconnect(name string) {
	s.mu.Lock()
	live, ok := s.hosts[name]
	delete(s.hosts, name)
	s.mu.Unlock()
	if !ok {
		return
	}
	live.stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Before the forwards: they are the only way into that server.
	if _, commit := live.editorServer(); commit != "" {
		if err := live.host.StopEditorServer(ctx, commit); err != nil {
			slog.Debug("stop the editor server", "host", name, "err", err)
		}
	}
	if err := live.fwd.Close(ctx); err != nil {
		slog.Debug("drop forwards", "host", name, "err", err)
	}
	if err := live.host.Close(); err != nil {
		slog.Debug("close host", "host", name, "err", err)
	}
}

// closeHosts drops every connection. The launcher going away takes the tunnels
// with it: nothing else is holding them open.
func (s *Server) closeHosts() {
	s.mu.Lock()
	names := make([]string, 0, len(s.hosts))
	for name := range s.hosts {
		names = append(names, name)
	}
	s.mu.Unlock()
	for _, name := range names {
		s.disconnect(name)
	}
}

// connected reports whether any host is up, which is what stops the launcher
// idling out from under a tunnel.
func (s *Server) connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.hosts) > 0
}

// mappings keeps the launcher's even when a frame omits it.
// mappings is the fixed forwards this side chose a local port for, then every
// port the far side published that none of them holds.
func mappings(ports []int, fixed ...remote.Mapping) []remote.Mapping {
	var out []remote.Mapping
	held := map[int]bool{}
	for _, m := range fixed {
		if m.Local <= 0 || m.Remote <= 0 || held[m.Remote] {
			continue
		}
		out = append(out, m)
		held[m.Remote] = true
	}
	for _, port := range ports {
		if !held[port] {
			out = append(out, remote.Same(port))
		}
	}
	return out
}

// localLauncherPort is stable per host, so a browser keeps its settings.
func localLauncherPort(host string) int {
	stable := httpx.StablePort("launcher@" + host)
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", stable)); err == nil {
		_ = ln.Close()
		return stable
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return stable
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func reachableAt(remoteURL string, local int) string {
	parsed, err := neturl.Parse(remoteURL)
	if err != nil {
		return remoteURL
	}
	parsed.Host = fmt.Sprintf("127.0.0.1:%d", local)
	return parsed.String()
}

// portsURL turns a launcher URL into its port stream, token and all.
func portsURL(launcherURL string) (string, error) {
	parsed, err := neturl.Parse(launcherURL)
	if err != nil {
		return "", err
	}
	token := parsed.Query().Get("t")
	return fmt.Sprintf("http://%s/api/ports?t=%s", parsed.Host, neturl.QueryEscape(token)), nil
}

func settingsPath() string {
	path, err := settings.Path()
	if err != nil {
		return "~/.ade/settings.json"
	}
	return path
}

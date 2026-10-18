// Package launcher serves the session list on its own, with no checkout behind
// it. It is how you start ade when you have not decided what to work on yet.
package launcher

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/owenrumney/ade/internal/browse"
	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/netcheck"

	"github.com/owenrumney/ade/internal/concierge"
	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/hub"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/models"
	"github.com/owenrumney/ade/internal/provision"
	"github.com/owenrumney/ade/internal/reviewer"
	"github.com/owenrumney/ade/internal/scratch"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/settingsapi"
	"github.com/owenrumney/ade/internal/ui"
	"github.com/owenrumney/ade/internal/wiki"
	"github.com/owenrumney/ade/internal/worktrees"
)

// idleCheck is how often an unattended launcher notices it has been left.
const idleCheck = 30 * time.Second

// Server is the launcher. It holds no repository state at all: everything it
// shows comes from the session scan, and everything it does is start a ade
// somewhere else.
type Server struct {
	token string

	// probe asks a checkout's recorded server about itself, for the graph.
	// nil is the real loopback request.
	probe sessions.Prober
	// open starts a ade in a checkout. nil is the real one; a test stands one
	// in so it can watch what happens around it without spawning a process.
	open func(ctx context.Context, dir, agent, model, sessionID string) (string, error)
	// create stands in for `ade create <name>`, as open does for a plain start.
	create func(ctx context.Context, dir, name, agent, model string) (string, string, error)
	// locate resolves a session id to the checkout and port serving it, for
	// the proxy. nil is the real scan.
	locate func(ctx context.Context, id string) (string, int, bool)
	home   string
	idle   time.Duration
	ctx    context.Context

	// version is shown in the launcher and included in diagnostics.
	version string
	// connectTo is a host to bring up as soon as this launcher is serving,
	// for `ade launch --host`.
	connectTo string

	// every is how often the idle check runs, and liveEvery how often what is
	// running is swept. Fields so a test does not have to wait one out.
	every     time.Duration
	liveEvery time.Duration

	// board is what is waiting on you across every repository. The launcher
	// serves it because none of it belongs to a checkout.
	board *dash.Board

	// limits is the plan usage. The launcher is where the question is asked —
	// what to start next — so it holds a reading whether or not a ade is up.
	limits *limits.Cache

	// spend is the last breakdown of where the short window went.
	spend *spendCache

	// stats is the last machine-wide transcript summary.
	stats     *statsCache
	repoPages *repoCache

	// disk is the last reading of what ade has left on disk.
	disk *diskCache

	// sweep is the last machine-wide worktree scan, for the purge panel.
	sweep *sweepCache

	// load is the last reading of what the connected machines are using.
	load *loadCache

	// jobs is the scheduler: the clock nothing else in ade holds.
	jobs *jobsClock

	// wantPort overrides the launcher's stable port. Zero takes the usual one.
	wantPort int

	// bus is this machine's one agentbus connection. The launcher holds it so
	// a person is one row, and so a claim and its verdict share a connection.
	bus *busnode

	// reviewer turns the person a review was asked of into the reviewer on
	// the pull request. It caches handles, so it is held rather than made.
	reviewer *reviewer.Resolver

	// wait is the last reading of what is waiting on you.
	wait *waitingCache
	wiki *wiki.Store
	// wikiSyncing keeps the background refresh to one worker.
	wikiSyncing atomic.Bool

	// quiet is one pending distillation per repository, armed by a session
	// going idle or leaving. quietMu holds it. See distillquiet.go.
	quiet   map[string]*time.Timer
	quietMu sync.Mutex

	// cards are what the concierge proposed and nobody pressed. In memory
	// only: see cards.go.
	cards  map[string]Card
	cardMu sync.Mutex

	// announced is which sessions were waiting at the last sweep, so only a
	// new one is notified about. Nil until the first sweep.
	announced   map[string]bool
	announcedMu sync.Mutex

	hub *httpx.Hub

	mu   sync.Mutex
	used time.Time
	port int
	// hosts are the dialled ssh masters, by name, and watchers the open port
	// streams to wake when a server starts.
	hosts    map[string]*conn
	watchers map[chan struct{}]struct{}
	// served is what each checkout's ade says it is serving, by session id.
	served  map[string]Serving
	exposed map[int]bool
	// active is the last answer /api/serving gave, and when. activeMu holds a
	// sweep to one at a time.
	active   []ActiveSession
	activeAt time.Time
	activeMu sync.Mutex
	// syncing is the repositories with a sync in flight, keyed by main checkout:
	// two runs over one would rebase the same worktrees at once, and a run over
	// another repository is no reason to refuse.
	syncing map[string]bool
	syncMu  sync.Mutex
	// hostRows is the last sweep of the connected machines, and quietUntil when
	// each may be asked again: one that did not answer is left alone for a while.
	hostRows   []ActiveSession
	hostRowsAt time.Time
	quietUntil map[string]time.Time
	// hostCheckouts is the last sweep of what those machines have checked out:
	// a scan rather than a reading, so it is held longer.
	hostCheckouts   []MachineRepos
	hostCheckoutsAt time.Time
	// hostErrors is what the last connect to each machine said, until it works.
	hostErrors map[string]string
	// editorLocks hold a machine's install-and-start to one window at a time.
	editorLocks map[string]*sync.Mutex

	// Both outlive the request that started the work.
	workMu     sync.Mutex
	hostWork   map[string]string
	workFailed map[string]string
}

// Home is ade's own state directory, which is where the launcher records
// itself: it serves no checkout, so it has no git dir to live in.
func Home() (string, error) { return settings.Dir() }

// RecordPath is that record.
func RecordPath() (string, error) { return settings.RecordPath() }

// writeRecord puts this launcher's address and token where every session in
// this home reads them. Fatal: a stale one names a token none of them can use.
func (s *Server) writeRecord(record, url string) error {
	if err := session.Write(record, session.Record{
		PID: os.Getpid(), URL: url, Root: s.home, StartedAt: time.Now(),
	}); err != nil {
		return fmt.Errorf("record this launcher at %s: %w", record, err)
	}
	return nil
}

// New builds the launcher. idle is how long it waits before giving the port
// back; zero keeps it up. There is no page: the editor is its only front end.
func New(idle time.Duration, version string) (*Server, error) {
	home, err := Home()
	if err != nil {
		return nil, err
	}
	s := &Server{
		token:     newToken(),
		home:      home,
		idle:      idle,
		every:     idleCheck,
		liveEvery: liveInterval,
		version:   version,
		ctx:       context.Background(),
		limits:    limits.NewCache(),
		spend:     &spendCache{},
		stats:     &statsCache{},
		repoPages: &repoCache{},
		disk:      &diskCache{},
		sweep:     &sweepCache{},
		load:      &loadCache{},
		jobs:      &jobsClock{},
		wait:      &waitingCache{},
		bus:       newBusnode(),
		reviewer:  reviewer.New(),
		used:      time.Now(),
		hub:       httpx.NewHub(),
		hosts:     map[string]*conn{},
		watchers:  map[chan struct{}]struct{}{},
		served:    map[string]Serving{},
		exposed:   map[int]bool{},
		syncing:   map[string]bool{},

		quietUntil:  map[string]time.Time{},
		hostErrors:  map[string]string{},
		editorLocks: map[string]*sync.Mutex{},
		hostWork:    map[string]string{},
		workFailed:  map[string]string{},
	}
	if config, err := settings.Load(); err == nil {
		s.wiki = wiki.New(home, config.WikiRemote)
	}
	s.recall()
	// No Dir: gh runs wherever this process is. The launcher serves no
	// checkout, and none of these queries is about one.
	s.board = dash.New(dash.Deps{
		Repos: s.repos,
		Open: func(ctx context.Context, dir, sessionID string) (string, error) {
			return s.openIn(ctx, dir, "", sessionID)
		},
		Create:  sessions.CreateIn,
		Changed: func() { s.hub.Broadcast("dash", nil) },
	})
	return s, nil
}

// ServeOn fixes the port this launcher binds. `make shots` uses it to run a
// launcher over a fixture home beside the real one; nothing else should.
func (s *Server) ServeOn(port int) { s.wantPort = port }

// listen binds the launcher's port: the stable one, so a browser's per-origin
// decisions hold, or the one ServeOn named.
func (s *Server) listen() (net.Listener, string, error) {
	if s.wantPort == 0 {
		return httpx.ListenFixed("launcher")
	}
	return httpx.ListenOn(s.wantPort)
}

// ConnectTo names a host to bring up once this launcher is serving. The
// browser is then pointed at that machine's launcher rather than this one,
// which stays up only to hold the tunnel.
func (s *Server) ConnectTo(name string) { s.connectTo = name }

// Run serves until the context is cancelled or the launcher is left alone for
// long enough.
func (s *Server) Run(ctx context.Context) error {
	s.ctx = ctx
	if s.wiki != nil && s.wiki.Enabled() {
		go func() { _ = s.wiki.Sync(ctx) }()
	}
	go s.keepHub(ctx)

	ln, base, err := s.listen()
	if err != nil {
		return err
	}
	// Serve closes it on the way out, so this only covers the returns before
	// then. Closing twice is an error nobody reads.
	defer func() { _ = ln.Close() }()
	url := base + "/?t=" + s.token
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		s.setPort(addr.Port)
	}
	// The tunnels are this process's; nothing else holds them open.
	defer s.closeHosts()

	// Recorded under the config directory rather than a git dir, which is what
	// makes a second `ade launch` hand you this one instead of a rival.
	record, err := RecordPath()
	if err != nil {
		return err
	}
	if err := s.writeRecord(record, url); err != nil {
		return err
	}
	defer func() {
		if err := session.Remove(record); err != nil {
			slog.Debug("remove launcher record", "err", err)
		}
	}()

	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stopIdle := context.WithCancel(ctx)
	defer stopIdle()
	go s.watchIdle(ctx, stopIdle)
	go s.watchDash(ctx)
	go s.watchLive(ctx)
	go s.watchLimits(ctx)
	go s.watchDashboard(ctx)
	go s.watchWaiting(ctx)
	go s.watchJobs(ctx)
	go s.watchMachine(ctx)
	go s.watchBus(ctx)
	go s.warmStats(ctx)
	defer s.hub.Close()
	go func() {
		<-ctx.Done()
		s.stopDistilling()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	// What is printed and opened is the far side's launcher when one was
	// asked for: this process is then only the thing holding the tunnel.
	target := url
	if s.connectTo != "" {
		remoteURL, err := s.Connect(ctx, s.connectTo)
		if err != nil {
			return err
		}
		target = remoteURL
	}

	fmt.Println(target)

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /icon.svg", handleIcon)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("GET /api/wiki/search", s.guard(s.handleWikiSearch))
	mux.HandleFunc("GET /api/wiki/page/{path...}", s.guard(s.handleWikiPage))
	s.mountProxy(mux)
	mux.HandleFunc("GET /api/repos", s.guard(s.handleRepos))
	mux.HandleFunc("GET /api/graph", s.guard(s.handleGraph))
	mux.HandleFunc("POST /api/graph/cancel", s.guard(s.handleGraphCancel))
	mux.HandleFunc("POST /api/graph/stop", s.guard(s.handleGraphStop))
	mux.HandleFunc("POST /api/open", s.guard(s.handleOpen))
	mux.HandleFunc("POST /api/editor", s.guard(s.handleEditor))
	mux.HandleFunc("POST /api/scratch", s.guard(s.handleScratch))
	mux.HandleFunc("POST /api/archive", s.guard(s.handleArchive))
	mux.HandleFunc("POST /api/remember", s.guard(s.handleRemember))
	mux.HandleFunc("POST /api/sessions/delete", s.guard(s.handleDeleteSessions))
	mux.HandleFunc("POST /api/sessions/rename", s.guard(s.handleRenameSession))
	mux.HandleFunc("POST /api/sessions/combine", s.guard(s.handleCombineSessions))
	mux.HandleFunc("POST /api/sessions/uncombine", s.guard(s.handleUncombineSessions))
	mux.HandleFunc("POST /api/sessions/suggestion/dismiss", s.guard(s.handleDismissSessionSuggestion))
	mux.HandleFunc("GET /api/browse", s.guard(s.handleBrowse))
	mux.HandleFunc("GET /api/worktrees", s.guard(s.handleWorktrees))
	mux.HandleFunc("GET /api/worktrees/all", s.guard(s.handleWorktreesAll))
	mux.HandleFunc("POST /api/prune", s.guard(s.handlePrune))
	mux.HandleFunc("POST /api/sync", s.guard(s.handleSync))
	mux.HandleFunc("GET /api/events", s.guard(s.hub.HandleSSE))
	mux.HandleFunc("GET /api/dash", s.guard(s.handleDash))
	mux.HandleFunc("GET /api/limits", s.guard(s.handleLimits))
	mux.HandleFunc("GET /api/limits/window", s.guard(s.handleWindowSpend))
	mux.HandleFunc("GET /api/stats", s.guard(s.handleStats))
	mux.HandleFunc("GET /api/repo", s.guard(s.handleRepo))
	mux.HandleFunc("GET /api/disk", s.guard(s.handleDisk))
	mux.HandleFunc("GET /api/disk/detail", s.guard(s.handleDiskDetail))
	mux.HandleFunc("POST /api/disk/purge", s.guard(s.handleDiskPurge))
	mux.HandleFunc("POST /api/concierge", s.guard(s.handleConcierge))
	mux.HandleFunc("GET /api/concierge/cards", s.guard(s.handleCards))
	mux.HandleFunc("POST /api/concierge/cards/{id}", s.guard(s.handleCard))
	mux.HandleFunc("POST "+concierge.MCPPath, s.guard(s.handleConciergeMCP))
	mux.HandleFunc("GET /api/todos", s.guard(s.handleTodos))
	mux.HandleFunc("POST /api/todos", s.guard(s.handleAddTodo))
	mux.HandleFunc("PATCH /api/todos/{id}", s.guard(s.handleEditTodo))
	mux.HandleFunc("DELETE /api/todos/{id}", s.guard(s.handleDeleteTodo))
	mux.HandleFunc("GET /api/jobs", s.guard(s.handleJobs))
	mux.HandleFunc("POST /api/jobs/run", s.guard(s.handleJobRun))
	mux.HandleFunc("POST /api/jobs/pause", s.guard(s.handleJobPause))
	mux.HandleFunc("POST /api/jobs/seen", s.guard(s.handleJobSeen))
	mux.HandleFunc("POST /api/jobs/save", s.guard(s.handleJobSave))
	mux.HandleFunc("POST /api/jobs/delete", s.guard(s.handleJobDelete))
	mux.HandleFunc("GET /api/waiting", s.guard(s.handleWaiting))
	mux.HandleFunc("GET /api/watching", s.guard(s.handleWatching))
	mux.HandleFunc("POST /api/reveal", s.guard(s.handleReveal))
	mux.HandleFunc("GET /api/serving", s.guard(s.handleActive))
	mux.HandleFunc("POST /api/serving", s.guard(s.handleServing))
	mux.HandleFunc("POST /api/quiet", s.guard(s.handleQuiet))
	mux.HandleFunc("POST /api/waiting/seen", s.guard(s.handleSeen))
	mux.HandleFunc("POST /api/say", s.guard(s.handleSay))
	mux.HandleFunc("POST /api/reviews/open", s.guard(s.handleOpenReview))
	mux.HandleFunc("POST /api/reviews/mute", s.guard(s.handleMuteReview))
	mux.HandleFunc("POST /api/notifications/dismiss", s.guard(s.handleDismissNotification))
	mux.HandleFunc("POST /api/dash/refresh", s.guard(s.handleDashRefresh))
	mux.HandleFunc("POST /api/linear/start", s.guard(s.handleStartTicket))
	mux.HandleFunc("GET /api/bus", s.guard(s.handleBusState))
	mux.HandleFunc("GET /api/bus/who", s.guard(s.handleBusWho))
	mux.HandleFunc("POST /api/bus/checkout", s.guard(s.handleBusCheckout))
	mux.HandleFunc("POST /api/bus/ask", s.guard(s.handleBusAsk))
	mux.HandleFunc("POST /api/bus/presence", s.guard(s.handleBusPresence))
	mux.HandleFunc("POST /api/bus/answer", s.guard(s.handleBusAnswer))
	mux.HandleFunc("POST /api/bus/claim", s.guard(s.handleBusClaim))
	mux.HandleFunc("POST /api/bus/result", s.guard(s.handleBusResult))
	mux.HandleFunc("POST /api/bus/review", s.guard(s.handleBusRequestReview))
	mux.HandleFunc("POST /api/bus/reviewed", s.guard(s.handleBusReviewed))
	mux.HandleFunc("POST /api/bus/dismiss", s.guard(s.handleBusDismiss))
	mux.HandleFunc("POST /api/bus/refresh", s.guard(s.handleBusRefresh))
	mux.HandleFunc("GET /api/ports", s.guard(s.handlePorts))
	mux.HandleFunc("POST /api/ports/expose", s.guard(s.handleExposePort))
	// The launcher is the process that reads Linear and GitHub; a dead network
	// here is not answerable from a checkout's server.
	mux.HandleFunc("POST /api/diagnose/net", s.guard(netcheck.Handler()))
	mux.HandleFunc("GET /api/hosts", s.guard(s.handleHosts))
	mux.HandleFunc("GET /api/hosts/repos", s.guard(s.handleHostRepos))
	mux.HandleFunc("GET /api/hosts/load", s.guard(s.handleHostLoad))
	mux.HandleFunc("POST /api/hosts", s.guard(s.handleAddHost))
	mux.HandleFunc("PATCH /api/hosts/{name}", s.guard(s.handleLabelHost))
	mux.HandleFunc("DELETE /api/hosts/{name}", s.guard(s.handleRemoveHost))
	mux.HandleFunc("POST /api/connect", s.guard(s.handleConnect))
	mux.HandleFunc("POST /api/disconnect", s.guard(s.handleDisconnect))
	mux.HandleFunc("POST /api/remote/editor-server", s.guard(s.handleEditorServer))
	mux.HandleFunc("POST /api/worker/stop", s.guard(s.handleWorkerStop))
	mux.HandleFunc("POST /api/worker/credentials", s.guard(s.handleWorkerCredentials))
	mux.HandleFunc("POST /api/worker/update", s.guard(s.handleWorkerUpdate))
	mux.HandleFunc("POST /api/target", s.guard(s.handleTarget))
	mux.HandleFunc("POST /api/provision", s.guard(s.handleProvision))
	// The same settings the checkout servers edit: one file, one set of
	// handlers, and no reason the launcher should be the page you cannot.
	(&settingsapi.API{
		Tickets: func() { s.board.RefreshTickets(s.ctx) },
		Synced:  func() { s.PushModels(s.ctx) },
		Changed: func() {
			s.board.RefreshTickets(s.ctx)
			s.board.RefreshReviews(s.ctx)
			s.board.RefreshNotifications(s.ctx)
		},
	}).Mount(mux, s.guard)
	return noReferrer(s.sameHost(mux))
}

// noReferrer stops the token leaving with a click, the same as the checkout
// server: it rides in the query for the stream, so a link out would carry it.
func noReferrer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// sameHost rejects a request that did not address us by a name we bound, so a
// rebound name cannot reach the settings. The port differs over a tunnel.
func (s *Server) sameHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Zero before the listener is up, which is also how the tests call in.
		if s.ownPort() == 0 {
			next.ServeHTTP(w, r)
			return
		}
		name := r.Host
		if host, _, err := net.SplitHostPort(r.Host); err == nil {
			name = host
		}
		if !httpx.HostAllowed(name) {
			slog.Warn("refused a request for another host", "host", r.Host)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guard requires the session token, the same way the server does: requiring a
// header on the mutation means a cross-origin page cannot drive this without
// passing CORS preflight.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.hasToken(r) {
			writeError(w, http.StatusForbidden, "bad or missing token")
			return
		}
		s.touch()
		next(w, r)
	}
}

// hasToken is the guard's own test, apart so the proxy can ask it.
func (s *Server) hasToken(r *http.Request) bool {
	got := r.Header.Get("X-Ade-Token")
	if got == "" {
		got = r.URL.Query().Get("t")
	}
	return sameToken(got, s.token)
}

func sameToken(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// handleSession hands back the live token. A tab left open across a restart has
// a good address and a dead token, and this is how it repairs itself.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	// It gives the token away, so only down the loopback listener.
	if !httpx.OnLoopback(r) {
		writeError(w, http.StatusForbidden, "the token is not given out over the network")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.token})
}

// handleWatching stops a second ade opening a tab over the one already up.
func (s *Server) handleWatching(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"watching": s.hub.Clients() > 0})
}

// handleReveal hands a terminal's session to whatever is watching. Nobody took
// it means nothing was shown, and the caller still owes the user a window.
func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var req Reveal
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Dir, req.ID = strings.TrimSpace(req.Dir), strings.TrimSpace(req.ID)
	if !filepath.IsAbs(req.Dir) {
		writeError(w, http.StatusBadRequest, "name the checkout to show")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revealed": s.hub.Broadcast("reveal", req) > 0})
}

// handleIcon is the mark in the tab. Outside the guard, like the page it sits
// on: a browser fetches a favicon with no token and no header.
func handleIcon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=3600")
	_, _ = w.Write([]byte(ui.Icon))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, session.Health{Ade: true, Root: s.home, PID: os.Getpid()})
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	// ?all=1 is the search's read: the shortlist is where it already looked.
	window := sessions.Recent()
	if r.URL.Query().Get("all") != "" {
		window = sessions.Everything()
	}
	repos, err := s.reposIn(window)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The hub rides along on the list the page already fetches. It is
	// machine-wide, like this launcher and unlike a checkout, which is why it
	// is offered here rather than inside a repository window.
	writeJSON(w, http.StatusOK, map[string]any{
		"repos": s.markRemembered(repos), "hub": hub.Detect(r.Context()),
	})
}

// repos is the whole of what this launcher offers, and the whole of what it
// will act on.
func (s *Server) repos() ([]sessions.Repo, error) {
	return s.reposIn(sessions.Recent())
}

func (s *Server) reposIn(window sessions.Window) ([]sessions.Repo, error) {
	cfg, err := settings.Load()
	if err != nil {
		return nil, err
	}
	return sessions.ReposIn(cfg.Roots(), cfg.Ignored, window), nil
}

// openIn starts or reuses the ade serving a checkout. The real one unless a
// test stood one in.
func (s *Server) openIn(ctx context.Context, dir, agent, sessionID string) (string, error) {
	return s.openOn(ctx, dir, agent, "", sessionID)
}

func (s *Server) openOn(ctx context.Context, dir, agent, model, sessionID string) (string, error) {
	if s.open != nil {
		return s.open(ctx, dir, agent, model, sessionID)
	}
	return sessions.OpenOn(ctx, dir, agent, model, sessionID)
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
		ID  string `json:"id"`
		// Direct is the session's own address rather than this launcher's
		// proxy. A browser wants one origin and does not send it.
		Direct bool `json:"direct"`
		// Machine is the launcher that can read that checkout's token.
		Machine string `json:"machine"`
		// Agent and Model are what a new session starts on. Both empty is the
		// configured default, which is what every other door here asks for.
		Agent string `json:"agent"`
		Model string `json:"model"`
		// Fresh starts a conversation rather than reopening the last one. An
		// empty id means "whatever this was last in", which New session is not.
		Fresh bool `json:"fresh,omitempty"`
		// Prompt is the first message, sent once the session is serving.
		Prompt string `json:"prompt,omitempty"`
		// Brief is a design document to write into the session, and whether to
		// build from it. How a build reaches a machine that has no copy of it.
		Brief *Brief `json:"brief,omitempty"`
		// Name makes a workspace for the session rather than using this
		// checkout: a document, and a worktree on the branch it names.
		Name string `json:"name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	if req.Machine != "" {
		s.openOnHost(w, r, req.Machine, req.Dir, req.ID, req.Agent, req.Model, req.Direct, req.Fresh, req.Prompt, req.Brief)
		return
	}

	// Only somewhere the scan actually found. The request names a directory to
	// start a process in, so it has to be one that was offered.
	all, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, listed := sessions.FindCheckout(all, req.Dir)
	if !listed {
		// Not on the list, but the picker reaches outside the scan roots on
		// purpose. It still has to be a real checkout: "start a process here"
		// cannot mean any path off a request.
		found, ok := browsed(req.Dir)
		if !ok {
			writeError(w, http.StatusBadRequest, "that is not one of the checkouts on this machine")
			return
		}
		dir = found
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		s.openWorkspace(w, cmp.Or(dir.Main, dir.Path), name, req.Agent, req.Model, req.Prompt)
		return
	}

	// An empty id opens the checkout without a conversation, which is how you
	// start something new somewhere you have never run an agent. A directory
	// that was not on the list has no sessions to name, so it only opens that
	// way.
	id, agent, model, looked := "", "", "", ""
	if row, up := s.announcedServer(req.ID); req.ID != "" && up && row.Dir == dir.Path {
		// An ade that announced itself is authority enough: a session with no
		// transcript yet, or a checkout outside the scan roots, has nothing else.
		id, looked = req.ID, req.ID
		// The scan still names the agent when it knows the session; resuming a
		// codex session as claude is a resume the chain refuses.
		if match, ok := sessions.Find(dir.Sessions, dir.Path, req.ID); ok {
			agent, model = match.Agent, match.Model
		}
	} else if req.ID != "" {
		match, ok := sessions.Find(dir.Sessions, dir.Path, req.ID)
		if !ok || !listed {
			writeError(w, http.StatusBadRequest, "that is not one of that checkout's sessions")
			return
		}
		// Looked at either way: one that cannot be resumed is still read, and
		// otherwise it never leaves the waiting list.
		looked = match.ID
		if match.Resumable {
			id, agent, model = match.ID, match.Agent, match.Model
		}
	}

	// A picked agent applies only to a session being started: resuming one
	// carries the agent it already has.
	if id == "" {
		if agent, model, err = picked(req.Agent, req.Model); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if id == "" && req.Fresh {
		id = sessions.None
	}
	// One server per checkout. ade itself finds the one already serving this
	// directory and hands the session to it, so this is the same either way.
	url, err := s.openOn(s.ctx, dir.Path, agent, model, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Before the first message: the brief is what the session is for, and a
	// message answered without it is answered against an empty document.
	briefErr := sendBrief(s.ctx, url, req.Brief)
	if briefErr == nil {
		sendFirstMessage(s.ctx, url, req.Prompt)
	}
	// Opening it is looking at it, which is what stops it waiting on you.
	if looked != "" {
		if err := settings.MarkSeen(looked, time.Now()); err != nil {
			slog.Debug("mark the session seen", "err", err)
		}
		s.wait.forget()
		s.hub.Broadcast("waiting", nil)
	}
	// Through this launcher when there is a session to key on. One origin for
	// every session is what lets one tab switch between them.
	// A worker's session ports are the container's own, so even the shell's direct ask is proxied.
	if !req.Direct || httpx.Wide() {
		// A fresh session has no id yet, so the one it announces stands in: no
		// proxy on a worker means a container port nothing outside can reach.
		key := looked
		if key == "" {
			key = s.announcedFor(dir.Path)
		}
		via, ok := s.proxyURL(r, key)
		if !ok && httpx.Wide() {
			writeError(w, http.StatusBadGateway, "the session started but never announced itself, so there is no address for it")
			return
		}
		if ok {
			url = via
			// The shell parses what it gets, so it needs the scheme a browser would infer.
			if req.Direct {
				url = "http:" + via
			}
		}
	}
	// Before the reply, not after. A ade watching this machine from another
	// one has to have the forward up by the time the page navigates, and the
	// page cannot navigate until it has read this response.
	s.notifyPorts()
	if briefErr != nil {
		// The session is open either way, so the failure says where it is.
		writeJSON(w, http.StatusBadGateway, map[string]any{"url": url, "error": briefErr.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// openWorkspace is `ade create <name>` from the tree: the document, the
// worktree on its branch, and the session, which is told what to start on.
func (s *Server) openWorkspace(w http.ResponseWriter, dir, name, agent, model, prompt string) {
	agent, model, err := picked(agent, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	create := s.create
	if create == nil {
		create = sessions.CreateNamed
	}
	url, path, err := create(s.ctx, dir, name, agent, model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sendFirstMessage(s.ctx, url, prompt)
	s.hub.Broadcast("repos", nil)
	s.notifyPorts()
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path})
}

// handleScratch makes a checkout for work with nowhere to live yet and opens
// a ade in it. Everywhere else here starts a session in a directory you named;
// this is the one that has none to name.
func (s *Server) handleScratch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Machine string `json:"machine"`
		// Agent and Model are what the new session starts on, as /api/open
		// takes them. Both empty is the configured default.
		Agent string `json:"agent"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	agent, model, err := picked(req.Agent, req.Model)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if machine := strings.TrimSpace(req.Machine); machine != "" {
		s.scratchOnHost(w, r, machine, req.Name, agent, model)
		return
	}

	dir, err := scratch.New(s.ctx, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	url, err := s.openOn(s.ctx, dir, agent, model, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if httpx.Wide() {
		via, ok := s.proxyURL(r, s.announcedFor(dir))
		if !ok {
			writeError(w, http.StatusBadGateway, "the scratch session started but never announced itself")
			return
		}
		url = via
	}
	s.notifyPorts()
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": dir})
}

// handleArchive puts a session away, or brings it back. Sessions are derived
// fresh from transcripts, so the flag lives beside the settings rather than on
// the session.
// handleArchive puts sessions away, or brings them back. One id or a list: the
// row toggle sends one, clearing a pile-up sends the lot.
func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string   `json:"id"`
		IDs      []string `json:"ids"`
		Archived bool     `json:"archived"`
		Machine  string   `json:"machine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	if s.onMachine(w, r, req.Machine, "/api/archive",
		map[string]any{"id": req.ID, "ids": req.IDs, "archived": req.Archived}) {
		return
	}
	ids := req.IDs
	if id := strings.TrimSpace(req.ID); id != "" {
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "name a session to archive")
		return
	}
	changed, err := settings.SetArchivedMany(ids, req.Archived)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": changed})
}

// handleRenameSession gives a session the name you typed, which is final.
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		writeError(w, http.StatusBadRequest, "name a session to rename")
		return
	}
	if err := settings.SetTitleByYou(id, req.Title); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"title": strings.TrimSpace(req.Title)})
}

// handleDeleteSessions takes sessions out of the lists for good. This page
// is where the pile-up shows, so it is where clearing it belongs.
func (s *Server) handleDeleteSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs     []string `json:"ids"`
		Machine string   `json:"machine"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	if s.onMachine(w, r, req.Machine, "/api/sessions/delete", map[string]any{"ids": req.IDs}) {
		return
	}
	gone, err := settings.Forget(req.IDs...)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": gone})
}

// repoOf picks a repository out of the scan by its main checkout, so a
// request can only name one that was offered.
func (s *Server) handleCombineSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string   `json:"dir"`
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	group, err := sessions.Combine(repos, req.Dir, req.IDs)
	if err != nil {
		writeError(w, groupStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group})
}

// groupStatus tells a bad selection from a store that would not take it.
func groupStatus(err error) int {
	if errors.Is(err, settings.ErrStore) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func (s *Server) handleUncombineSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sessions.Uncombine(repos, req.ID); err != nil {
		writeError(w, groupStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissSessionSuggestion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir         string `json:"dir"`
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, ok := sessions.FindCheckout(repos, req.Dir)
	if !ok || dir.Suggestion == nil || dir.Suggestion.Fingerprint != req.Fingerprint {
		writeError(w, http.StatusBadRequest, "that is not a current session suggestion")
		return
	}
	if err := settings.SetSessionGroupSuggestionDismissed(dir.Path, req.Fingerprint); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) repoOf(path string) (sessions.Repo, bool, error) {
	all, err := s.repos()
	if err != nil {
		return sessions.Repo{}, false, err
	}
	for _, repo := range all {
		if repo.Path == path {
			return repo, true, nil
		}
	}
	return sessions.Repo{}, false, nil
}

// handleWorktrees lists a repository's linked worktrees and what stands in
// the way of removing each. The launcher is where the pile-up is visible,
// so it is where they are cleared from, though it is in none of them.
func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	repo, ok, err := s.repoOf(r.URL.Query().Get("repo"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the repositories on this machine")
		return
	}
	found, err := worktrees.Scan(r.Context(), repo.Path, r.URL.Query().Get("force") == "true")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if found == nil {
		found = []worktrees.Worktree{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"worktrees": found})
}

// handlePrune removes the worktrees named, by the same rules as ade prune:
// each is checked again against a fresh scan, and only one the scan calls
// removable goes.
func (s *Server) handlePrune(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo  string   `json:"repo"`
		Paths []string `json:"paths"`
		Force bool     `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	repo, ok, err := s.repoOf(req.Repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the repositories on this machine")
		return
	}
	// A session clone has no worktrees, and is itself the row offered.
	if _, cloned := provision.OwnedDir(repo.Path); cloned {
		s.pruneClone(w, r, repo.Path, req.Paths, req.Force)
		return
	}
	found, err := worktrees.Scan(r.Context(), repo.Path, req.Force)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	removed := []string{}
	left := []kept{}
	for _, path := range req.Paths {
		wt, ok := worktrees.Find(found, path)
		if !ok {
			left = append(left, kept{Path: path, Why: "not one of that repository's worktrees"})
			continue
		}
		if err := worktrees.Remove(r.Context(), repo.Path, wt); err != nil {
			left = append(left, kept{Path: path, Why: err.Error()})
			continue
		}
		removed = append(removed, path)
	}
	if len(removed) > 0 {
		_ = gitdiff.PruneWorktrees(r.Context(), repo.Path)
		s.sweep.forget()
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed, "kept": left})
}

type kept struct {
	Path string `json:"path"`
	Why  string `json:"why"`
}

// pruneClone removes a session clone and the directory around it. Judged
// again here: the sweep behind the panel's row stands for two minutes.
func (s *Server) pruneClone(w http.ResponseWriter, r *http.Request, path string, paths []string, force bool) {
	removed := []string{}
	left := []kept{}
	for _, want := range paths {
		if !worktrees.SamePath(want, path) {
			left = append(left, kept{Path: want, Why: "not that clone"})
			continue
		}
		if err := worktrees.RemoveClone(r.Context(), worktrees.Clone(r.Context(), path, force)); err != nil {
			left = append(left, kept{Path: want, Why: err.Error()})
			continue
		}
		removed = append(removed, want)
	}
	if len(removed) > 0 {
		s.sweep.forget()
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed, "kept": left})
}

// handleSync rebases a repository's worktrees onto a freshly fetched base:
// the launcher is where drift is visible, so it is where it is cleared from.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		DryRun bool   `json:"dryRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return
	}
	repo, ok, err := s.repoOf(req.Repo)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the repositories on this machine")
		return
	}
	// Refused rather than queued: a second sync would read worktrees the first
	// is part way through rebasing.
	if !s.claimSync(repo.Path) {
		writeError(w, http.StatusConflict, "a sync of this repository is already running")
		return
	}
	defer s.releaseSync(repo.Path)
	report, err := worktrees.Sync(r.Context(), repo.Path, req.DryRun)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A rebase moves every count the purge panel prints.
	if !req.DryRun {
		s.sweep.forget()
	}
	writeJSON(w, http.StatusOK, report)
}

// claimSync takes one repository's sync slot, or reports that it is taken. It
// only covers this process: an ade sync in a terminal is not held off by it.
func (s *Server) claimSync(repo string) bool {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if s.syncing[repo] {
		return false
	}
	s.syncing[repo] = true
	return true
}

func (s *Server) releaseSync(repo string) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	delete(s.syncing, repo)
}

// handleBrowse lists directories, so the page can open a checkout the scan
// roots do not cover. Directories only, behind the same token as the rest.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	view, err := browse.List(r.URL.Query().Get("path"))
	if errors.Is(err, browse.ErrRelative) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// browsed is a checkout named by the picker rather than found by the scan. An
// absolute path that really holds a repository, and nothing else.
func browsed(path string) (sessions.Dir, bool) {
	path = strings.TrimSpace(path)
	// The field completes a path the way a shell does, so it can be typed
	// with a tilde and never expanded by one.
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, "~/") {
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		return sessions.Dir{}, false
	}
	path = filepath.Clean(path)
	if !browse.IsCheckout(path) {
		return sessions.Dir{}, false
	}
	return sessions.Dir{Path: path, Name: filepath.Base(path)}, true
}

// watchIdle gives the port back when the launcher has been left. You pick a
// session and go, and the page you came through is not worth a process.
func (s *Server) watchIdle(ctx context.Context, stop context.CancelFunc) {
	if s.idle <= 0 {
		return
	}
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			quiet := time.Since(s.used)
			s.mu.Unlock()
			// A connected host or an open page is somebody still here; a fire in
			// flight is the launcher still working. See the design doc.
			if quiet >= s.idle && !s.connected() && s.hub.Clients() == 0 && !s.jobs.busy() {
				slog.Info("launcher idle, shutting down", "for", quiet.Round(time.Second))
				stop()
				return
			}
		}
	}
}

func (s *Server) touch() {
	s.mu.Lock()
	s.used = time.Now()
	s.mu.Unlock()
}

func newToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Without a token the page is open to anything else on the machine.
		panic(fmt.Sprintf("read random: %v", err))
	}
	return hex.EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Debug("write json", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// picked validates an agent and model named by the page. Both go onto a
// command line, so the model has to be one the catalogue offers for that agent.
func picked(agent, model string) (string, string, error) {
	agent = strings.ToLower(strings.TrimSpace(agent))
	model = strings.TrimSpace(model)
	switch agent {
	case "", "claude", "codex":
	default:
		return "", "", fmt.Errorf("%q is not an agent ade runs", agent)
	}
	if model == "" {
		return agent, "", nil
	}
	for _, one := range models.Load().Models {
		if one.ID != model {
			continue
		}
		// A model with no agent named picks its own: claude on a gpt id is a
		// turn that fails at the first request rather than at the door.
		if agent == "" {
			return one.Agent, model, nil
		}
		if one.Agent == agent {
			return agent, model, nil
		}
	}
	return "", "", fmt.Errorf("%q is not a model on the list; sync the models in settings", model)
}

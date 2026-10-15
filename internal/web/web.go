// Package web serves the diff and its review threads to the browser.
package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/editor"
	"github.com/owenrumney/ade/internal/httpx"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/estimate"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/highlight"
	"github.com/owenrumney/ade/internal/hub"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/lineage"
	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/overlap"
	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/settingsapi"
	"github.com/owenrumney/ade/internal/term"
	"github.com/owenrumney/ade/internal/verify"
	"github.com/owenrumney/ade/internal/whisper"
	"github.com/owenrumney/ade/internal/wiki"
	"github.com/owenrumney/ade/internal/words"
	"path/filepath"
)

// pollInterval is how often the diff is recomputed. Re-running git diff and
// hashing it catches edits, commits and branch switches alike.
const pollInterval = 1500 * time.Millisecond

// maxBody caps request bodies so a stray client cannot exhaust memory.
const maxBody = 1 << 20

// maxChatBody is the one exception: chat.MaxTotalBytes of attachments, plus
// the third base64 adds. Below that, chat.Check's message is unreachable.
const maxChatBody = 56 << 20

// workspace is everything tied to one checkout. It is never mutated once built;
// relocating into a worktree swaps the whole struct, so a reader sees either the
// old checkout or the new one and never a mixture of the two.
type workspace struct {
	homeRoot string // the session's own checkout; a view switch never moves it
	branch   string
	gitDir   string
	opts     gitdiff.Options
	store    *comments.Store

	// viewRoot is the repository the Diff tab is showing when it is not the
	// home one. Empty is the ordinary case and behaves exactly as before.
	viewRoot string

	// source yields the current diff. A checked-out PR uses git so local edits
	// show up; an unchecked-out one is a fixed snapshot fetched from GitHub.
	source func(context.Context) (*gitdiff.Diff, error)

	// pr is the pull request being served, when one is. snapshot means the diff
	// came from GitHub rather than git, so there is nothing local to act on.
	// Both live here because opening a pull request swaps them together with
	// everything else, and a reader must not catch one without the other.
	pr       *PRInfo
	snapshot bool
}

// Server owns the diff, the thread store and the browser connection.
type Server struct {
	hub   *httpx.Hub
	token string

	// wiki is the shared wiki, or nil when no remote is configured.
	wiki          *wiki.Store
	delegateToken string

	// hookToken authorizes the agent's PreToolUse hook, which is a different
	// caller from the page and from the agent's own tools.
	hookToken string
	agent     *agentRunner

	// docsDir is where design documents live. Empty when ade is serving a
	// snapshot, which has no working tree to design against.
	docsDir string

	// started divides a document already on disk from one written during the
	// conversation, which is how a design is adopted on an unmatched branch.
	started time.Time

	// remembered is what ade knows about the repository, across every worktree
	// of it. Resolved once: the identity is a git remote.
	remembered     *memory.Store
	rememberedOnce sync.Once
	// debt is the last count of unread transcripts, so the state poll does not
	// scan them. Refreshed when a run settles, which is when it can change.
	debt *ProjectDebt

	// goingReason is why the server is shutting down, told to the pages on the
	// way out. Empty for a shutdown nobody explained.
	goingReason string

	// docDeclined is a document the user closed. The state poll looks for one
	// belonging to the branch, and without this it would reopen the one they
	// had just put away.
	docDeclined string

	// startRoot and startGitDir are where this process began. They identify the
	// process, so they do not move when it relocates into a worktree — otherwise
	// the record would be orphaned under a directory nothing looks in.
	startRoot   string
	startGitDir string
	// direct and startedAt are this server's own record, kept so it can be
	// written again when the launcher's proxied address turns up.
	direct       string
	startedAt    time.Time
	proxy        string
	commonGitDir string

	// startBase is the base this run was launched with, so closing a pull
	// request can put it back rather than leaving its base in place.
	startBase string

	// pendingID is what this server is called on the rail until the agent names
	// a session. Without it a checkout just opened is on no rail at all.
	pendingID string

	// idle is how long with no browser attached before shutting down.
	idle time.Duration

	// seedTicket drafts the open document from a ticket once serving starts.
	seedTicket string

	// seedIssue does the same from a GitHub issue, read here rather than by the
	// agent, so the draft does not need a tool that can reach GitHub.
	seedIssue int

	// seedInvestigate is the ticket to open on, skillName the skill it runs
	// under. The skill's text lives on the agent.
	seedInvestigate string
	skillName       string

	// ctx outlives any single request, since an agent run does too.
	ctx context.Context

	// termMu guards the terminal alone. The shell is nothing to do with the
	// diff, and a browser typing into it must not queue behind a poll.
	// terms is one shell per pane, keyed by the browser's pane id.
	termMu   sync.Mutex
	terms    map[string]*term.Session
	termRoot string

	// rowPath is each session's transcript, found once.
	rowMu   sync.Mutex
	rowPath map[string]string

	// stop ends this server. The idle watcher has always had it; Exit is the
	// same thing asked for rather than waited for.
	stop context.CancelFunc

	// bg is every goroutine a turn starts, so Close can wait for the writes
	// they make under .git rather than leave them racing whatever comes next.
	bg sync.WaitGroup

	mu       sync.RWMutex
	ws       *workspace
	diff     *gitdiff.Diff
	diffHash string
	draft    *CommitDraft
	// quiz is the questions written about the diff, held so reopening the
	// panel does not pay for them again. Nothing about how you did is kept.
	quiz *Quiz

	// roots is snapshotted from the sandbox file; homeOpts is what home showed.
	roots    []DiffRoot
	homeOpts gitdiff.Options

	worktree     *gitdiff.Status
	worktreeHash string
	// overlap caches the commit read of each worktree, keyed by its tip: the
	// patch-id pass walks every diff on a branch. A zero Cache is ready.
	overlap overlap.Cache
	created *CreatedPR
	docFile string
	// drift is the last design-drift pass. In memory only: it is a reading of
	// the working tree at a moment, so a stale one on disk is worse than none.
	drift *DriftState
	// chat is the checkout's conversations, written through as they happen so
	// a restart costs you nothing but the run that was in flight. tools maps a
	// tool call in flight to the message it was written as, so its result can
	// land on the same row.
	chat  *chat.Store
	tools map[string]string
	// ruled is a rule that decided a call before its row was written.
	ruled map[string]string
	// children is the sub-agents at work, followed through their transcripts.
	children *children
	// wakeWas is the tree as the turn nobody sent began.
	wakeWas string
	// turns is the exchanges in flight and the files each has written, so a
	// finished turn can be credited with the lines it changed. lineages is the
	// lineage store of each workspace that has been opened, keyed by path,
	// since the workspace itself is swapped for a worktree or a pull request.
	turns          []*turn
	lineages       map[string]*lineage.Store
	previewPending bool
	// verify is the repository's own checks after a turn: what is running,
	// what the last run said, and a detected list waiting on your yes.
	verifyRunning bool
	verifyCancel  context.CancelFunc
	verifyLast    *verify.Outcome
	verifyOffer   []verify.Check
	verifyGen     int
	// attaching serialises appending pictures to a finished turn's reply. The
	// preview capture and the outbox both do it, off the turn and at once.
	attaching sync.Mutex
	// recording is the one commit-record publisher: a commit, an amend and a
	// merge all write the same wiki draft.
	recording sync.Mutex
	// sharpening guards sharpenStop, which ends the look the next one replaces.
	sharpening  sync.Mutex
	sharpenStop context.CancelFunc
	// welded is the prompts the running chat turn was made of, under mu.
	welded *weldedTurn
	// runChatTurn is the chat turn drain starts. Tests replace it.
	runChatTurn func(ctx context.Context, attached *chat.Context, message string, files []chat.File)
	// recorded is the merge and keys last published for a pull request, so the
	// poll behind it does not clone and submit the same thing on every tick.
	recorded map[int]string

	// attachment is a selection waiting to go on the next message, under mu
	// because an editor writes it and every state read serves it.
	attachment *Attachment
	attachSeq  int64
	// sides is the named agents in flight beside the main one, and heard what
	// they said since the main agent last spoke, relayed into its next turn.
	sides    map[string]sideRun
	nextSide int
	// held is slots a review batch keeps back for the @commenter hand-offs
	// its reviewers will make, so a review of N reviewers is not capped at
	// two by the all-or-none claim in startJobs. batches is what each review
	// filed, until its rebuttal round has settled it.
	held      map[string]int
	batches   map[string]*reviewBatch
	nextBatch int
	heard     []design.Heard
	// limits is the last reading of the plan windows.
	limits    *limits.Cache
	estimates estimate.History
	avatars   *avatarCache
	branchPR  *BranchPR
	openPRs   []ghpr.Listed
	// board is what is waiting on you across every repository. Not one
	// checkout's, which is why the launcher serves the same lists.
	board      *dash.Board
	docVersion string

	// mcpHub is the separately-running MCP control plane, if there is one. ade
	// only ever links to it.
	mcpHub hub.Info

	// bus is the connection to agentbus and what it has offered this ade.
	bus *bus
	// busHeld is the launcher's bus state as last read, so drawing the page is
	// not an HTTP call. busMu guards it alone.
	busMu   sync.Mutex
	busHeld *BusState
	// busSaid is the answers already put in the conversation, so a poll that
	// sees the same one again does not say it twice.
	busSaid map[string]bool
	// busRun is the session connection held while an agent is working here.
	busRun busSession

	// version is ade's own, told to the bus so a `who` row says what is running.
	version string

	// queue holds chat turns sent while the agent was busy, in the order you
	// sent them. nextQueued names them, so removing one does not depend on a
	// position that shifts as the queue drains.
	queue      []queued
	nextQueued int

	// projectName is owner/repo, and projectFor is the checkout it was read
	// for, so moving into a worktree asks again.
	projectName string
	projectFor  string

	// viewer is your GitHub login, asked for once. viewerAsked separates "not
	// looked up yet" from "looked up and could not tell".
	viewer      string
	viewerAsked bool

	listenPort string

	noLauncher bool

	// openIn starts or finds the ade for another checkout, and ask speaks to
	// one. Fields so a test can stand in for them: the real ones run this
	// binary, which under go test is the test itself.
	openIn func(ctx context.Context, dir, sessionID string) (string, error)
	// createIn starts work on a ticket in another repository: ade create
	// --linear, run there. A field so a test can stand one in.
	createIn func(ctx context.Context, dir, ticket string) (string, error)
	ask      func(ctx context.Context, url, method, path, body string) error
	// checkOutPR puts a pull request in its own worktree. A field for the same
	// reason: the real one runs git and gh.
	checkOutPR func(ctx context.Context, main string, number int) (string, error)
}

// Config describes what a server serves and which agent it talks to.
type Config struct {
	Root      string
	Branch    string
	Session   string
	ClaudeBin string
	CodexBin  string
	Provider  string
	Model     string

	// AutoCompact is the window Claude Code compacts at ("auto" or a token
	// count); empty leaves Claude Code's default.
	AutoCompact string

	// Sandbox runs the agent in a container, the checkout mounted, and
	// SandboxExplicit is it having been typed rather than defaulted.
	Sandbox         bool
	SandboxExplicit bool

	// Idle is how long to wait, with no browser attached, before shutting down.
	// Zero stays up.
	Idle time.Duration
	Diff gitdiff.Options

	// GitDir is this checkout's git directory, where its comment store lives.
	GitDir string

	// CommonGitDir is the repository's main git directory, shared with every
	// worktree. Design documents live under it so one design is visible from
	// the checkout that wrote it and from any worktree built off it.
	CommonGitDir string

	// PR and Snapshot are set only for --pr. A non-empty Snapshot means the PR
	// branch is not checked out, so the diff cannot come from local git.
	PR       *PRInfo
	Snapshot string

	// DocsDir overrides where design documents live.
	DocsDir string

	// DocFile is the document to open on, if any.
	DocFile string

	// SeedTicket drafts that document from a tracker ticket on startup. Set only
	// when the document was just created, so an existing design is never
	// overwritten by one.
	SeedTicket string

	// SeedIssue drafts it from a GitHub issue of this repository instead. Set
	// by create --github, and never together with SeedTicket.
	SeedIssue int

	// Skill is a skill's body, prepended to the main chat's system prompt, and
	// SkillName what it is called. Set by `ade investigate`.
	Skill     string
	SkillName string

	// SeedInvestigation is a ticket to investigate under that skill on startup.
	SeedInvestigation string

	// NoLauncher prints this server's own address and starts no launcher.
	NoLauncher bool

	// Version is ade's version, for anything that announces this server.
	Version string
}

// New builds a server for a repo. The diff is loaded on Run.
func New(cfg Config, store *comments.Store) (*Server, error) {
	// An unset scope behaves as the branch view, so name it: the browser shows
	// which one is active, and an empty string would show none of them.
	if cfg.Diff.Scope == "" {
		cfg.Diff.Scope = gitdiff.ScopeBranch
	}
	s := &Server{
		hub:             httpx.NewHub(),
		token:           newToken(),
		agent:           newProviderAgentRunner(cfg.Root, cfg.Session, cfg.ClaudeBin, cfg.CodexBin, cfg.Provider, cfg.Model, cfg.AutoCompact, sandboxConfig{On: cfg.Sandbox, Explicit: cfg.SandboxExplicit, GitDir: cfg.CommonGitDir, Workspace: sandbox.WorkspacePath(cfg.CommonGitDir)}, runlog.Open(runlog.Path(cfg.GitDir))),
		ctx:             context.Background(),
		idle:            cfg.Idle,
		seedTicket:      cfg.SeedTicket,
		seedIssue:       cfg.SeedIssue,
		seedInvestigate: cfg.SeedInvestigation,
		skillName:       cfg.SkillName,
		startBase:       cfg.Diff.Base,
		pendingID:       "pending-" + newToken(),
		startRoot:       cfg.Root,
		startGitDir:     cfg.GitDir,
		commonGitDir:    cfg.CommonGitDir,
		tools:           map[string]string{},
		ruled:           map[string]string{},
		children:        newChildren(),
		bus:             newBus(),
		busSaid:         map[string]bool{},
		version:         cfg.Version,
		limits:          limits.NewCache(),
		started:         time.Now(),

		avatars:  newAvatarCache(),
		openIn:   sessions.OpenIn,
		createIn: sessions.CreateIn,
		ask:      sessions.Ask,

		checkOutPR: dash.ReviewWorktree,
		ws: &workspace{
			homeRoot: cfg.Root,
			branch:   cfg.Branch,
			gitDir:   cfg.GitDir,
			opts:     cfg.Diff,
			store:    store,
			pr:       cfg.PR,
		},
	}
	s.noLauncher = cfg.NoLauncher
	s.wiki = wikiStore()
	s.agent.wiki = s.wiki
	// Only with a wiki to draft into: an id allocated otherwise is a write to
	// the runlog for a mount that never appears.
	if s.wiki != nil && s.wiki.Enabled() {
		s.agent.ensureWikiDraft(newToken())
	}
	// The board is the same three lists the launcher shows. Its gh runs in
	// this checkout only because there is one; the queries are account-wide.
	s.board = dash.New(dash.Deps{
		Dir:     cfg.Root,
		Repos:   s.repos,
		Open:    func(ctx context.Context, dir, id string) (string, error) { return s.openIn(ctx, dir, id) },
		Create:  func(ctx context.Context, dir, ticket string) (string, error) { return s.createIn(ctx, dir, ticket) },
		Changed: func() { s.hub.Broadcast("state", nil) },
	})

	// Set before anything can run, so a finished run always finds them.
	s.runChatTurn = s.runChat
	s.agent.onIdle = s.drainAfter
	s.agent.onWake = s.afterWake
	s.agent.onWakeStart = s.beforeWake
	s.children.done = func() <-chan struct{} { return s.ctx.Done() }
	s.children.spawn = s.spawn
	s.children.dir = s.subagentsDir
	s.children.alive = func() bool { return !s.agent.gone() }
	s.children.touch = s.agent.touch
	s.children.amend = s.amendChild
	s.children.tell = func() { s.hub.Broadcast("agent", nil) }
	s.agent.onStep = s.onStep
	s.agent.onSession = s.onSession
	s.agent.onLimits = s.limits.NoteCodex
	s.agent.onAsk = s.onAsk
	s.agent.docsDir = func() string { dir, _ := s.designPlace(); return dir }
	s.agent.outbox = s.outboxEnv
	s.agent.projectPrompt = s.projectPrompt
	s.agent.skill = strings.TrimSpace(cfg.Skill)
	// What was waiting when the last server stopped is still waiting, and
	// stays waiting: the store it comes back from is a file anything writing
	// the git dir can plant into, so nothing runs until the user says so.
	// Read once the store is open, below.
	defer func() {
		if n := s.loadQueue(); n > 0 {
			s.say(ChatMessage{Kind: chat.KindNote, Body: fmt.Sprintf("%d queued turn(s) restored from last time — send anything, or run them from the queue", n)})
		}
	}()

	// The conversation is read back off disk, so a restart picks up where you
	// left off rather than opening on an empty pane. A server with no checkout
	// behind it has nowhere of its own, so its conversation goes in the temp
	// dir rather than in whatever directory it happened to start in.
	chatDir := cfg.GitDir
	if chatDir == "" {
		chatDir = filepath.Join(os.TempDir(), fmt.Sprintf("ade-%d", os.Getpid()))
	}
	conversations, err := chat.Open(chat.Path(chatDir))
	if err != nil {
		return nil, err
	}
	s.chat = conversations

	// A snapshot has no working tree to design against, so the Design tab is
	// left off rather than offering a build that could not run.
	if cfg.Snapshot == "" {
		dir, err := docs.Dir(cfg.Root, cfg.CommonGitDir, cfg.DocsDir)
		if err != nil {
			return nil, err
		}
		s.docsDir = dir

		switch {
		case cfg.DocFile != "":
			// Recorded, so a later start without the flag keeps it.
			s.docFile = cfg.DocFile
			if err := docs.SetOpened(cfg.CommonGitDir, cfg.Branch, cfg.Session, cfg.DocFile); err != nil {
				slog.Debug("record open document", "file", cfg.DocFile, "err", err)
			}
		default:
			// Otherwise it has to belong to this branch. Nothing matching means
			// no Design tab, rather than opening whatever was touched last.
			dirs := docs.SearchDirs(cfg.Root, cfg.CommonGitDir, cfg.DocsDir)
			if found, file, ok := docs.Find(dirs, cfg.CommonGitDir, cfg.Branch, cfg.Session); ok {
				s.docsDir, s.docFile = found, file
			}
		}
	}

	if cfg.Snapshot != "" {
		base, head := "base", "PR"
		if cfg.PR != nil {
			head = cfg.PR.HeadRef
		}
		fixed := gitdiff.FromText(base, head, cfg.Snapshot)
		s.ws.snapshot = true
		s.ws.source = func(context.Context) (*gitdiff.Diff, error) { return fixed, nil }
	} else {
		s.ws.source = gitSource(cfg.Diff, cfg.PR != nil)
	}
	return s, nil
}

// Close releases what the server holds on disk. Run does this on the way out;
// a test that opens a second server over the same checkout has to as well,
// since the conversation store is locked to one process.
func (s *Server) Close() {
	// Before the store closes: an agent still running is about to be cancelled
	// with the process, and the conversation should say so.
	s.noteInterruptedSides()
	s.closeTerminal()
	s.settle(closeWait)
	if err := s.chatStore().Close(); err != nil {
		slog.Debug("close the chat store", "err", err)
	}
}

// closeWait bounds how long Close waits for a turn's goroutines: a check still
// running is not worth holding the exit for.
const closeWait = 5 * time.Second

// spawn runs f on its own goroutine, counted so Close can wait for it.
func (s *Server) spawn(f func()) {
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		f()
	}()
}

// settle waits for the goroutines spawn started, or for most, and says which.
func (s *Server) settle(most time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.bg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(most):
		slog.Debug("closing with background work still running")
		return false
	}
}

// gitSource reads the diff from git, so local edits show up as they are made.
func gitSource(opts gitdiff.Options, isPR bool) func(context.Context) (*gitdiff.Diff, error) {
	return func(ctx context.Context) (*gitdiff.Diff, error) {
		diff, err := gitdiff.Load(ctx, opts)
		if err == nil && isPR {
			diff.Mode = gitdiff.ModePR
		}
		return diff, err
	}
}

// diffRoot is the repository the Diff tab is showing: a sandbox root the user
// selected, or the session's own. Everything else takes homeRoot.
func (w *workspace) diffRoot() string {
	if w.viewRoot != "" {
		return w.viewRoot
	}
	return w.homeRoot
}

// space returns the current checkout. The struct behind it is immutable, so a
// caller that takes it once is safe against a relocation part way through.
func (s *Server) space() *workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ws
}

// Run serves until ctx is cancelled, optionally opening a browser. Any upstream
// review threads are imported once the diff is loaded so they can anchor to it.
func (s *Server) Run(ctx context.Context, openBrowser bool, imported []ghpr.Thread) error {
	s.ctx = ctx
	if err := s.refresh(ctx); err != nil {
		return err
	}
	if n := s.importComments(imported); n > 0 {
		slog.Info("imported review comments", "count", n)
	}

	ln, base, err := s.listen()
	if err != nil {
		return err
	}
	s.listenPort = portOf(base)
	// Kept when a rebuild handed one on, so a run in flight is not cut off.
	if s.delegateToken == "" {
		s.delegateToken = newToken()
	}
	if s.hookToken == "" {
		s.hookToken = newToken()
	}
	s.agent.setDelegate(base, s.delegateToken)
	s.agent.setHook(base, s.hookToken)
	// Not Close: a page is owed a reason, and everything that reaches this
	// defer is a real shutdown. A dev rebuild execs over the process, so it
	// never gets here and a restarting page is left to reconnect.
	defer s.hub.Bye(s.goingWhy())
	defer s.agent.Close()
	defer s.Close()

	url := base + "/?t=" + s.token

	// Recorded here because this is the first point the URL exists, and cleared
	// on the way out so the next run does not chase a dead server.
	s.direct, s.startedAt = url, time.Now()
	s.record("")
	defer func() {
		for _, dir := range s.recordDirs() {
			unrecord(dir)
		}
	}()
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}

	ctx, stopIdle := context.WithCancel(ctx)
	defer stopIdle()
	s.stop = stopIdle

	s.rememberWorkspace()
	s.snapshotRoots(ctx)
	s.watchLogs()

	// Before anything runs: a container from a run that outlived its server is
	// still mounted read-write on this checkout.
	go s.agent.sweepOrphan(ctx)

	go s.installCommitHook(ctx)
	go s.poll(ctx)
	go s.pollBranchPR(ctx)
	go s.watchMCPHub(ctx)
	// The rail is the announced set, so a restart has to say so itself.
	go s.announceServing(ctx, s.railID())
	go s.watchActivity(ctx)
	go s.watchMachine(ctx)
	go s.watchIdle(ctx, stopIdle)
	go s.watchBus(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	// After the listener is up, so the browser opens on a document already being
	// written rather than waiting for the agent first.
	switch {
	case s.seedTicket != "":
		s.draftFromTicket(s.seedTicket)
	case s.seedIssue > 0:
		// Off this goroutine: reading the issue is a gh call, and the URL below
		// it should not wait on the network.
		go s.draftFromIssue(ctx, s.seedIssue)
	case s.seedInvestigate != "":
		s.startInvestigation(s.seedInvestigate, s.skillName)
	}

	// The rail is on the launcher's origin, not this one.
	through, _ := s.throughLauncher(ctx)
	if through != "" {
		url = through
		s.record(through)
	}

	fmt.Println(url)
	// The editor is the front end: a window already on this checkout comes
	// forward, and with no editor cli the address above is what there is.
	if openBrowser {
		if err := editor.Open(s.startRoot); err != nil {
			fmt.Printf("%v; open %s in your editor, where the ade extension finds this session\n", err, s.startRoot)
		}
	}

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("OPTIONS /api/", s.handlePreflight)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST "+wiki.MCPPath, s.handleWikiMCP)
	mux.HandleFunc("POST "+BusMCPPath, s.handleBusMCP)

	mux.HandleFunc("GET /api/state", s.guard(s.handleState))
	mux.HandleFunc("POST /api/launcher", s.guard(s.handleLauncher))
	mux.HandleFunc("GET /api/diff", s.guard(s.handleDiff))
	mux.HandleFunc("GET /api/blob", s.guard(s.handleBlob))
	mux.HandleFunc("GET /api/blame", s.guard(s.handleBlame))
	mux.HandleFunc("GET /api/history", s.guard(s.handleHistory))
	mux.HandleFunc("GET /api/file/at", s.guard(s.handleFileAt))
	mux.HandleFunc("GET /api/lineage", s.guard(s.handleLineage))
	mux.HandleFunc("GET /api/lineage/marks", s.guard(s.handleLineageMarks))
	mux.HandleFunc("POST /api/attach", s.guard(s.handleAttach))
	mux.HandleFunc("POST /api/transcribe", s.guard(whisper.Handler()))
	mux.HandleFunc("DELETE /api/attach", s.guard(s.handleUnattach))
	mux.HandleFunc("POST /api/threads", s.guard(s.handleCreateThread))
	mux.HandleFunc("POST /api/threads/{id}/reply", s.guard(s.handleReply))
	mux.HandleFunc("POST /api/threads/{id}/resolve", s.guard(s.handleThreadResolve))
	mux.HandleFunc("DELETE /api/threads/{id}", s.guard(s.handleDelete))
	mux.HandleFunc("POST /api/threads/reset", s.guard(s.handleResetThreads))
	mux.HandleFunc("DELETE /api/threads", s.guard(s.handleResetThreads))
	mux.HandleFunc("POST /api/resolve", s.guard(s.handleAgentResolve))
	mux.HandleFunc("POST /api/threads/{id}/draft-reply", s.guardHome(s.handleDraftReply))
	mux.HandleFunc("POST /api/threads/{id}/send-reply", s.guardHome(s.handleSendReply))
	mux.HandleFunc("POST /api/threads/send-replies", s.guardHome(s.handleSendReplies))
	mux.HandleFunc("POST /api/draft-replies", s.guardHome(s.handleDraftReplies))
	mux.HandleFunc("POST /api/review", s.guard(s.handleReview))
	mux.HandleFunc("POST /api/explain", s.guard(s.handleExplain))
	mux.HandleFunc("POST /api/answer", s.guard(s.handleAnswer))
	mux.HandleFunc("POST /api/cancel", s.guard(s.handleCancel))
	mux.HandleFunc("GET /api/verify", s.guard(s.handleVerify))
	mux.HandleFunc("POST /api/verify", s.guard(s.handleVerifySave))
	mux.HandleFunc("POST /api/verify/run", s.guardHome(s.handleVerifyRun))
	mux.HandleFunc("GET /api/turns/{id}/undo", s.guard(s.handleUndoPlan))
	mux.HandleFunc("POST /api/turns/{id}/undo", s.guardHome(s.handleUndo))
	mux.HandleFunc("POST /api/turns/{id}/fixture", s.guardHome(s.handleSaveFixture))
	mux.HandleFunc("POST /api/sides/{id}/cancel", s.guard(s.handleCancelSide))
	mux.HandleFunc("PUT /api/queue/{id}", s.guard(s.handleEditQueued))
	mux.HandleFunc("DELETE /api/queue/{id}", s.guard(s.handleDropQueued))
	mux.HandleFunc("POST /api/queue/run", s.guard(s.handleRunQueued))
	mux.HandleFunc("POST /api/queue/{id}/urgent", s.guard(s.handlePromoteQueued))
	mux.HandleFunc("POST /api/sharpen", s.guard(s.handleSharpen))
	mux.HandleFunc("POST /api/commit-message", s.guard(s.handleCommitMessage))
	mux.HandleFunc("POST /api/quiz", s.guard(s.handleQuiz))
	mux.HandleFunc("POST /api/commit", s.guard(s.handleCommit))
	mux.HandleFunc("POST /api/stage", s.guard(s.handleStage))
	mux.HandleFunc("POST /api/unstage", s.guard(s.handleUnstage))
	mux.HandleFunc("POST /api/push", s.guard(s.handlePush))
	mux.HandleFunc("POST /api/branch", s.guard(s.handleBranch))
	mux.HandleFunc("POST /api/base/pull", s.guard(s.handlePullBase))
	mux.HandleFunc("POST /api/base/rebase", s.guard(s.handleRebaseBase))
	mux.HandleFunc("GET /api/commit/head", s.guard(s.handleHeadMessage))
	mux.HandleFunc("POST /api/pr-create", s.guardHome(s.handlePRCreate))
	mux.HandleFunc("POST /api/scope", s.guard(s.handleScope))
	mux.HandleFunc("POST /api/diff/root", s.guard(s.handleDiffRoot))
	mux.HandleFunc("GET /api/repos", s.guard(s.handleRepos))
	mux.HandleFunc("GET /api/graph", s.guard(s.handleGraph))
	mux.HandleFunc("POST /api/graph/cancel", s.guard(s.handleGraphCancel))
	mux.HandleFunc("POST /api/graph/stop", s.guardHome(s.handleGraphStop))
	mux.HandleFunc("GET /api/recent", s.guard(s.handleRecent))
	mux.HandleFunc("POST /api/sessions/open", s.guard(s.handleOpenSession))
	mux.HandleFunc("GET /api/prs", s.guard(s.handleListPRs))
	mux.HandleFunc("GET /api/prs/{number}/checks", s.guard(s.handleChecksFor))
	mux.HandleFunc("POST /api/prs/{number}", s.guardHome(s.handleOpenPR))
	mux.HandleFunc("POST /api/reviews/open", s.guard(s.handleOpenReview))
	mux.HandleFunc("POST /api/reviews/mute", s.guard(s.handleMuteReview))
	mux.HandleFunc("POST /api/notifications/dismiss", s.guard(s.handleDismissNotification))
	mux.HandleFunc("GET /api/sandbox", s.guard(s.handleSandbox))
	mux.HandleFunc("GET /api/sandbox/browse", s.guard(s.handleBrowse))
	mux.HandleFunc("PUT /api/sandbox", s.guard(s.handleSaveSandbox))
	mux.HandleFunc("PUT /api/sandbox/on", s.guard(s.handleSandboxOn))
	mux.HandleFunc("GET /api/preview", s.guard(s.handlePreview))
	mux.HandleFunc("PUT /api/preview", s.guard(s.handleSavePreview))
	(&settingsapi.API{
		Tickets: func() { s.board.RefreshTickets(s.ctx) },
		Changed: func() {
			s.board.RefreshTickets(s.ctx)
			s.board.RefreshReviews(s.ctx)
			s.board.RefreshNotifications(s.ctx)
		},
	}).Mount(mux, s.guard)
	mux.HandleFunc("POST /api/linear/refresh", s.guard(s.handleLinearRefresh))
	mux.HandleFunc("POST /api/linear/start", s.guard(s.handleLinearStart))
	mux.HandleFunc("DELETE /api/prs", s.guardHome(s.handleClosePR))
	mux.HandleFunc("PUT /api/pr", s.guardHome(s.handleEditPR))
	mux.HandleFunc("POST /api/pr/checkout", s.guardHome(s.handlePRCheckout))
	mux.HandleFunc("POST /api/pr/merge", s.guardHome(s.handleMergePR))
	mux.HandleFunc("POST /api/pr/approve", s.guardHome(s.handleApprovePR))
	mux.HandleFunc("POST /api/pr/request-changes", s.guardHome(s.handleRequestChangesPR))
	mux.HandleFunc("POST /api/threads/{id}/convert", s.guardHome(s.handleConvert))
	mux.HandleFunc("POST /api/github/send", s.guardHome(s.handleSend))
	mux.HandleFunc("PUT /api/threads/{id}/outgoing", s.guardHome(s.handleSetOutgoing))
	mux.HandleFunc("POST /api/checks/refresh", s.guardHome(s.handleRefreshChecks))
	mux.HandleFunc("POST /api/checks/fix", s.guardHome(s.handleFixChecks))
	mux.HandleFunc("GET /api/avatar/{login}", s.guard(s.handleAvatar))
	mux.HandleFunc("GET /api/files", s.guard(s.handleFiles))
	mux.HandleFunc("GET /api/files/raw", s.guard(s.handleFileRaw))
	mux.HandleFunc("GET /api/files/render", s.guard(s.handleFileRender))
	mux.HandleFunc("GET /api/files/uses", s.guard(s.handleFileUses))
	mux.HandleFunc("DELETE /api/files", s.guard(s.handleFileDelete))
	mux.HandleFunc("GET /api/design", s.guard(s.handleDesignDoc))
	mux.HandleFunc("POST /api/design", s.guard(s.handleDesignCreate))
	mux.HandleFunc("PUT /api/design", s.guard(s.handleDesignSave))
	mux.HandleFunc("DELETE /api/design", s.guard(s.handleDesignClose))
	mux.HandleFunc("DELETE /api/design/doc", s.guard(s.handleDesignDelete))
	mux.HandleFunc("POST /api/design/push", s.guard(s.handleDesignPush))
	mux.HandleFunc("GET /api/hosts", s.guard(s.handleHosts))
	mux.HandleFunc("POST /api/design/seed", s.guard(s.handleDesignSeed))
	mux.HandleFunc("POST /api/design/fold", s.guard(s.handleDesignFold))
	mux.HandleFunc("POST /api/design/condense", s.guard(s.handleDesignCondense))
	mux.HandleFunc("POST /api/design/go", s.guardHome(s.handleDesignGo))
	mux.HandleFunc("GET /api/design/estimate", s.guard(s.handleBuildEstimate))
	mux.HandleFunc("GET /api/project", s.guard(s.handleProject))
	mux.HandleFunc("PUT /api/project", s.guard(s.handleProjectSave))
	mux.HandleFunc("POST /api/project/entry", s.guard(s.handleProjectEntry))
	mux.HandleFunc("POST /api/project/distil", s.guard(s.handleProjectDistil))
	mux.HandleFunc("GET /api/drift", s.guard(s.handleDrift))
	mux.HandleFunc("POST /api/drift", s.guardHome(s.handleDriftRun))
	mux.HandleFunc("POST /api/drift/dismiss", s.guardHome(s.handleDriftDismiss))
	mux.HandleFunc("GET /api/logs", s.guard(s.handleLogs))
	mux.HandleFunc("POST /api/diagnose/net", s.guard(s.handleNetCheck))
	mux.HandleFunc("GET /api/terminal", s.guard(s.handleTerminal))
	mux.HandleFunc("POST /api/terminal/close", s.guard(s.handleTerminalClose))
	mux.HandleFunc("POST /api/agent/terminal", s.guard(s.handleAgentTerminal))
	mux.HandleFunc("POST /api/agent/terminal/close", s.guard(s.handleAgentTerminalClose))
	mux.HandleFunc("GET /api/agents", s.guard(s.handleAgents))
	mux.HandleFunc("GET /api/internal/delegate/agents", s.handlePiAgents)
	mux.HandleFunc("POST /api/internal/delegate", s.handlePiDelegate)
	mux.HandleFunc("GET /api/internal/delegate/said", s.handleSaid)
	mux.HandleFunc("POST /api/internal/delegate/doc", s.handleDelegateDoc)
	mux.HandleFunc("GET /api/internal/delegate/drift", s.handleDelegateDrift)
	mux.HandleFunc("GET /api/chat", s.guard(s.handleChatList))
	mux.HandleFunc("POST /api/chat", s.guard(s.handleChat))
	mux.HandleFunc("POST /api/chat/new", s.guard(s.handleChatNew))
	mux.HandleFunc("POST /api/chat/clear", s.guard(s.handleChatClear))
	mux.HandleFunc("POST /api/port", s.guardHome(s.handlePort))
	mux.HandleFunc("POST /api/chat/resume", s.guard(s.handleChatResume))
	mux.HandleFunc("POST /api/chat/model", s.guard(s.handleChatModel))
	mux.HandleFunc("POST /api/chat/answer", s.guard(s.handleChatAnswer))
	mux.HandleFunc("POST /api/chat/decide", s.guard(s.handleChatDecide))
	mux.HandleFunc("POST /api/chat/{id}/separate", s.guard(s.handleChatSeparate))
	mux.HandleFunc("GET /api/bus/who", s.guard(s.handleBusWho))
	mux.HandleFunc("POST /api/bus/answer", s.guard(s.handleBusAnswer))
	mux.HandleFunc("POST /api/bus/claim", s.guard(s.handleBusClaim))
	mux.HandleFunc("POST /api/bus/result", s.guard(s.handleBusResult))
	mux.HandleFunc("POST /api/bus/request-review", s.guard(s.handleBusRequestReview))
	mux.HandleFunc("POST /api/bus/dismiss", s.guard(s.handleBusDismiss))
	mux.HandleFunc("POST /api/bus/here", s.guard(s.handleBusHere))
	mux.HandleFunc("POST /api/bus/refresh", s.guard(s.handleBusRefresh))
	mux.HandleFunc("POST /api/bus/ask", s.guard(s.handleBusAsk))
	mux.HandleFunc("POST /api/bus/presence", s.guard(s.handleBusPresence))
	// Its own token, not s.guard: the caller is the agent's hook, not the page.
	mux.HandleFunc("POST /api/hook/pretooluse", s.handleHook)
	mux.HandleFunc("POST /api/chat/free", s.guard(s.handleFree))
	mux.HandleFunc("GET /permissions", s.guard(s.handleRulesPage))
	mux.HandleFunc("GET /api/permissions", s.guard(s.handleRules))
	mux.HandleFunc("POST /api/permissions", s.guard(s.handleRuleChange))
	mux.HandleFunc("POST /api/chat/switch", s.guard(s.handleChatSwitch))
	mux.HandleFunc("POST /api/investigate", s.guard(s.handleInvestigate))
	mux.HandleFunc("POST /api/sessions/archive", s.guard(s.handleArchive))
	mux.HandleFunc("POST /api/sessions/delete", s.guard(s.handleDeleteSessions))
	mux.HandleFunc("POST /api/sessions/combine", s.guard(s.handleCombineSessions))
	mux.HandleFunc("POST /api/sessions/uncombine", s.guard(s.handleUncombineSessions))
	mux.HandleFunc("POST /api/sessions/suggestion/dismiss", s.guard(s.handleDismissSessionSuggestion))
	mux.HandleFunc("POST /api/exit", s.guard(s.handleExit))
	mux.HandleFunc("GET /api/worktrees", s.guard(s.handleListWorktrees))
	mux.HandleFunc("POST /api/worktrees/prune", s.guardHome(s.handlePruneWorktrees))
	mux.HandleFunc("POST /api/worktrees/push", s.guardHome(s.handlePushWorktree))
	mux.HandleFunc("POST /api/worktrees/close", s.guardHome(s.handleCloseWorktree))
	mux.HandleFunc("POST /api/worktrees/merge", s.guardHome(s.handleMergeWorktree))
	mux.HandleFunc("GET /api/worktrees/overlap", s.guard(s.handleOverlap))
	mux.HandleFunc("GET /api/worktrees/switch", s.guard(s.handleSwitchableWorktrees))
	mux.HandleFunc("POST /api/worktrees/switch", s.guardHome(s.handleSwitchWorktree))
	mux.HandleFunc("GET /assets/mermaid.min.js", s.handleMermaid)
	mux.HandleFunc("GET /design-assets/{name}", s.handleDesignAssets)
	mux.HandleFunc("GET /design-images/{name}", s.guard(s.handleDocImage))
	mux.HandleFunc("GET /attachments/{name}", s.guard(s.handleAttachment))
	mux.HandleFunc("GET /api/events", s.guard(s.hub.HandleSSE))
	return noReferrer(s.sameHost(mux))
}

// noReferrer stops the token leaving with a click: it rides in the query, so
// a link out of a document would carry the whole url.
func noReferrer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// sameHost rejects a request that did not address us by name. Loopback stops the
// network; this stops a rebound name reaching us from a page in your browser.
func (s *Server) sameHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Empty before the listener is up, which is also how the tests call in.
		// The wiki tool carries its own bearer capability; a box names us however it can.
		exempt := r.Method == http.MethodPost && r.URL.Path == wiki.MCPPath
		if s.listenPort != "" && !exempt && !localHost(r.Host, s.listenPort) {
			slog.Warn("refused a request for another host", "host", r.Host)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func localHost(host, port string) bool {
	name, got, err := net.SplitHostPort(host)
	if err != nil {
		// No port means port 80, which is never where ade listens.
		return false
	}
	return name != "" && (httpx.HostAllowed(name) || containerName(name)) && got == port
}

// containerName is what an agent's box calls this server: never a name DNS can rebind.
func containerName(name string) bool {
	return name == "host.docker.internal" || name == strings.TrimSpace(os.Getenv("ADE_WORKER_NAME"))
}

// guard requires the session token. Requiring a header on mutations means a
// cross-origin page cannot drive the API without passing CORS preflight.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Before the check, so a refusal is one the caller can read rather than
		// a CORS error standing in front of it.
		allowEditor(w, r)
		if !s.authorized(r) {
			slog.Warn("request not authorized", "method", r.Method, "path", r.URL.Path, "from", r.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		started := time.Now()
		rec := &logged{ResponseWriter: w, status: http.StatusOK}
		next(rec, r)
		rec.report(r)
		// Not the log's own fetch: logging it wakes the viewer, which fetches again.
		if r.URL.Path != "/api/logs" {
			slog.Debug("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "ms", time.Since(started).Milliseconds())
		}
	}
}

// guardHome is guard for a route that acts on the session's own checkout.
func (s *Server) guardHome(next http.HandlerFunc) http.HandlerFunc {
	return s.guard(func(w http.ResponseWriter, r *http.Request) {
		ws := s.space()
		if ws.viewRoot == "" {
			next(w, r)
			return
		}
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"the diff is showing %s; switch back to %s to do that",
			filepath.Base(ws.viewRoot), filepath.Base(ws.homeRoot)))
	})
}

// logged is the ResponseWriter the handlers are given, so that a failure the
// browser is told about is also written down. Until this was here the only
// record of a refused request was the status bar, which is no record at all:
// a gh command ade would not run read as "send worked" for a whole morning.
type logged struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (l *logged) WriteHeader(status int) {
	l.status = status
	l.ResponseWriter.WriteHeader(status)
}

// Write keeps the start of a failure's body, which is where writeError puts
// the message worth reading. A success is passed straight through.
func (l *logged) Write(b []byte) (int, error) {
	if l.status >= 400 && len(l.body) < 1024 {
		l.body = append(l.body, b...)
	}
	return l.ResponseWriter.Write(b)
}

// Flush passes through. The event stream asks for an http.Flusher and refuses
// to start without one, so a wrapper that swallowed it would stop every page
// updating.
func (l *logged) Flush() {
	if f, ok := l.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap is for http.ResponseController, which looks through wrappers.
func (l *logged) Unwrap() http.ResponseWriter { return l.ResponseWriter }

// report writes down a request that was refused. 5xx is ade's own fault and
// 4xx the caller's, but both are worth a line: which of the two it was is
// exactly what you cannot tell from the browser afterwards.
func (l *logged) report(r *http.Request) {
	if l.status < 400 {
		return
	}
	args := []any{"method", r.Method, "path", r.URL.Path, "status", l.status}
	if msg := errorMessage(l.body); msg != "" {
		args = append(args, "err", msg)
	}
	if l.status >= 500 {
		slog.Error("request failed", args...)
		return
	}
	slog.Warn("request refused", args...)
}

// errorMessage is what writeError put in the body, or the body itself when it
// is not one of those.
func errorMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var wire struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error != "" {
		return wire.Error
	}
	return strings.TrimSpace(string(body))
}

// authorized is whether a request carries the session token, in the header
// or — for EventSource, which cannot set headers — in the query.
func (s *Server) authorized(r *http.Request) bool {
	got := r.Header.Get("X-Ade-Token")
	if got == "" {
		got = r.URL.Query().Get("t")
	}
	return sameToken(got, s.token)
}

// sameToken never matches an empty key: no token is not an open door.
func sameToken(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// handleHealth proves the port belongs to this ade, for the next invocation
// deciding whether to reuse it. It is outside the token guard because that check
// happens before the caller has a token; without one it gives away the
// repository path and nothing else. The port is predictable from the checkout
// path, so what the agent is doing — branch, model, session, queue — is only
// said to a caller that already holds the token, as the graph's probe does.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := session.Health{Ade: true, Root: s.startRoot, PID: os.Getpid()}
	if s.authorized(r) {
		state := s.agentState()
		agent := &session.AgentHealth{
			Running: state.Running, Kind: state.Kind, Model: state.Model, Session: state.Session,
			StartedAt: state.StartedAt, Queued: state.Queued, Error: state.Error, Sandbox: state.Sandbox,
			LastEventAt: state.LastEventAt, Activity: state.Activity, Asking: len(state.Asking),
		}
		for _, side := range state.SideRuns {
			agent.SideRuns = append(agent.SideRuns, side.Name)
		}
		health.Branch = s.space().branch
		health.Agent = agent
	}
	writeJSON(w, http.StatusOK, health)
}

// handleSession hands back the live token. A tab left open across a restart has
// a good address and a dead token, and this is how it repairs itself.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	// It gives the token away, so only down the loopback listener.
	if !httpx.OnLoopback(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": s.token})
}

type stateResponse struct {
	// The diff itself is fetched separately. It is by far the largest thing the
	// server holds, it changes far less often than the run does, and sending it
	// with every agent step made the page re-render every open file per tool
	// call. The client refetches /api/diff only when this version moves.
	DiffVersion   string              `json:"diffVersion"`
	Threads       []comments.Thread   `json:"threads"`
	Meta          meta                `json:"meta"`
	Agent         RunState            `json:"agent"`
	Draft         *CommitDraft        `json:"draft,omitempty"`
	Quiz          *Quiz               `json:"quiz,omitempty"`
	PR            *PRInfo             `json:"pr,omitempty"`
	Worktree      *gitdiff.Status     `json:"worktree,omitempty"`
	Created       *CreatedPR          `json:"created,omitempty"`
	Design        *DesignState        `json:"design,omitempty"`
	BranchPR      *BranchPR           `json:"branchPr,omitempty"`
	OpenPRs       []ghpr.Listed       `json:"openPrs,omitempty"`
	Reviews       []dash.Review       `json:"reviews,omitempty"`
	Notifications []dash.Notification `json:"notifications,omitempty"`
	// Hub is mcp-hub when it happens to be running. Absent rather than a
	// disabled button: there is nothing to offer and nothing to explain.
	Hub *hub.Info `json:"hub,omitempty"`

	// Verify is the repository's own checks: the last run, or an offer to
	// start running them. Absent until there is something to say.
	Verify *VerifyState `json:"verify,omitempty"`

	// Limits is how full the subscription windows are, when known.
	Limits *LimitsState `json:"limits,omitempty"`

	// Linear is the issues assigned to you, when a key is set.
	Linear *dash.Tickets `json:"linear,omitempty"`

	// Project is the project's memory, as a count rather than its contents:
	// how much is waiting to be reviewed and how far behind distillation is.
	Project *ProjectDebt `json:"project,omitempty"`

	// Chat is which conversation the agent is in and how long it is, so a
	// page whose copy disagrees knows to read it again — a stream frame it
	// missed, a fetch that crossed a message on the way — without the
	// messages themselves, which are fetched on their own.
	Chat ChatInfo `json:"chat"`

	// Attachment is a selection an editor put on the next message, absent when
	// the composer has nothing waiting.
	Attachment *Attachment `json:"attachment,omitempty"`

	// Bus is what agentbus has offered this ade, and whether it is joined.
	// Absent when there is no bus and nothing waiting.
	Bus *BusState `json:"bus,omitempty"`
}

// ChatInfo is the conversation's session and its length.
type ChatInfo struct {
	Session string `json:"session"`
	Count   int    `json:"count"`
}

type meta struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Base is the ref the diff is against, and what a rebase replays onto.
	Base  string `json:"base,omitempty"`
	Mode  string `json:"mode"`
	Store string `json:"store"`
	Scope string `json:"scope"`

	// Project is owner/repo from the origin remote — which project you are in,
	// as against which directory it happens to be checked out in.
	Project string `json:"project,omitempty"`

	// Scratch is what puts porting on the palette, and only there.
	Scratch bool `json:"scratch,omitempty"`

	// Roots is what the Diff tab can be pointed at; View is the one on screen.
	Roots []DiffRoot `json:"roots,omitempty"`
	View  string     `json:"view,omitempty"`

	// The page holds it, so the closed screen has a way back once this is gone.
	Launcher string `json:"launcher,omitempty"`

	// Files is how many the diff touches. The shell's title bar says it and
	// has no reason to fetch a whole diff for one number.
	Files int `json:"files"`
}

// launcherURL is the running launcher, token and all. Read on every state
// poll, so the PID is checked and not probed.
func launcherURL() string {
	path, err := settings.RecordPath()
	if err != nil {
		return ""
	}
	rec, ok := session.Read(path)
	if !ok || !session.Running(rec.PID) {
		return ""
	}
	return rec.URL
}

// handleState returns everything about the session except the diff, which
// /api/diff serves against the version named here. Threads are still anchored
// to diff lines, so the client applies a state only once it holds the diff that
// version names — otherwise a comment could be drawn against a line that has
// moved.
func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	ws := s.space()
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	// Run refreshes before serving, so this is only nil if that failed. The
	// browser needs a diff object either way.
	if diff == nil {
		diff = &gitdiff.Diff{}
	}

	writeJSON(w, http.StatusOK, stateResponse{
		DiffVersion: s.currentHash(),
		Threads:     ws.store.List(),
		Meta: meta{
			Repo:     ws.homeRoot,
			Branch:   ws.branch,
			Base:     ws.opts.Base,
			Mode:     diff.Mode,
			Store:    ws.store.Path(),
			Scope:    ws.opts.Scope,
			Project:  s.project(),
			Launcher: launcherURL(),
			Scratch:  settings.IsScratch(ws.homeRoot),
			Roots:    s.currentRoots(),
			View:     ws.viewRoot,
			Files:    len(diff.Files),
		},
		Agent:         s.agentState(),
		Draft:         s.currentDraft(),
		Quiz:          s.currentQuiz(),
		PR:            ws.pr,
		Worktree:      s.currentWorktree(),
		Created:       s.currentCreatedPR(),
		Design:        s.currentDesign(),
		BranchPR:      s.currentBranchPR(),
		OpenPRs:       s.currentOpenPRs(),
		Reviews:       s.board.Reviews(),
		Notifications: s.board.Notifications(),
		Hub:           s.currentMCPHub(),
		Limits:        s.currentLimits(),
		Verify:        s.verifyState(),
		Linear:        s.board.Tickets(),
		Project:       s.projectDebt(),
		Chat:          s.chatInfo(),
		Attachment:    s.currentAttachment(),
		Bus:           s.currentBus(),
	})
}

// chatInfo is where the conversation is and how long, for the state.
func (s *Server) chatInfo() ChatInfo {
	session := s.agent.Current()
	return ChatInfo{Session: session, Count: s.chatStore().Len(session)}
}

// currentLimits is the plan windows as last read, or nothing yet.
func (s *Server) currentLimits() *LimitsState {
	state := s.limits.Snapshot(s.ctx)
	if state.Claude == nil && state.Codex == nil {
		return nil
	}
	return &state
}

// handleDiff serves the diff on its own, tagged with the version /api/state
// reports, so the client can tell whether the copy it holds is current.
func (s *Server) handleDiff(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	diff, version := s.diff, s.diffHash
	s.mu.RUnlock()

	if diff == nil {
		diff = &gitdiff.Diff{}
	}

	writeJSON(w, http.StatusOK, diffResponse{Version: version, Diff: diff})
}

type diffResponse struct {
	Version string        `json:"version"`
	Diff    *gitdiff.Diff `json:"diff"`
}

func (s *Server) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
		File  string `json:"file"`
		Side  string `json:"side"`
		Line  int    `json:"line"`
		Body  string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}

	record := comments.Thread{
		Scope:    req.Scope,
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: req.Body}},
	}

	// A repo comment is about the change as a whole, so it needs no anchor.
	if req.Scope != comments.ScopeRepo {
		s.mu.RLock()
		diff := s.diff
		s.mu.RUnlock()

		anchor, ok := comments.Locate(diff, req.File, req.Side, req.Line)
		if !ok {
			writeError(w, http.StatusBadRequest, "that line is not in the current diff")
			return
		}
		record.File, record.Side, record.Line, record.Anchor = req.File, req.Side, req.Line, anchor
	}

	thread, err := s.space().store.Add(record)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.hub.Broadcast("threads", nil)
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) handleReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	thread, err := s.space().store.Reply(r.PathValue("id"), comments.AuthorYou, req.Body)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.hub.Broadcast("threads", nil)
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) handleThreadResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resolved bool `json:"resolved"`
	}
	if !decode(w, r, &req) {
		return
	}
	thread, err := s.space().store.SetResolved(r.PathValue("id"), req.Resolved)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	s.hub.Broadcast("threads", nil)
	writeJSON(w, http.StatusOK, thread)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.space().store.Delete(r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	s.hub.Broadcast("threads", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleResetThreads clears the comments on this branch so a re-review starts
// from nothing. What came from a pull request, or has already been posted to
// one, stays: this store is not where either of those lives.
//
// Refused while anything is running, so a review part way through filing
// cannot half survive its own deletion.
func (s *Server) handleResetThreads(w http.ResponseWriter, _ *http.Request) {
	if s.agent.State().Running {
		writeError(w, http.StatusConflict, "the agent is running; wait for it or stop it first")
		return
	}
	if runs := s.sideRuns(); len(runs) > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf("@%s is still reviewing; wait for it or stop it first", runs[0].Name))
		return
	}

	removed, err := s.space().store.Reset()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

// handleExit shuts this server down, and can take the session out of the
// launcher's list on the way. It is the door: `ade stop` without a terminal.
//
// The reply goes first and the shutdown a beat later, so the browser hears
// "yes" rather than a dropped connection it would have to guess about.
func (s *Server) handleExit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// Session is what to do with the conversation: keep it listed, archive
		// it, or take it out of every list for good. Empty means keep.
		Session string `json:"session"`
	}
	if !decode(w, r, &req) {
		return
	}
	if code, err := s.partWith(req.Session); err != nil {
		writeError(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stopping": true})

	// The door is the last chance to have this session's prose distilled, and
	// the launcher is what will still be here to do it.
	s.quietly()

	// Nil before Run has started serving, which is every test and no real
	// browser: answering is still the honest thing to do.
	if s.stop == nil {
		return
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.stop()
	}()
}

// partWith puts the conversation away on the way out. Archiving and deleting
// are both reversible from the launcher; deleting only stops it being offered.
func (s *Server) partWith(what string) (int, error) {
	if what == "" || what == "keep" {
		return 0, nil
	}
	id := s.agent.Current()
	if id == "" {
		return http.StatusBadRequest, fmt.Errorf("this checkout has no session to %s yet", what)
	}
	switch what {
	case "archive":
		if err := settings.SetArchived(id, true); err != nil {
			return http.StatusInternalServerError, err
		}
	case "delete":
		if _, err := settings.Forget(id); err != nil {
			return http.StatusInternalServerError, err
		}
	default:
		return http.StatusBadRequest, fmt.Errorf("unknown session disposal %q", what)
	}
	return 0, nil
}

// poll recomputes the diff on a timer and tells the browser when it changed.
func (s *Server) poll(ctx context.Context) {
	if s.space().snapshot {
		return
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// watchMCPHub keeps track of whether mcp-hub is up, so the page can offer a way
// into it.
//
// On its own timer rather than the main tick: that runs every 1.5s and the hub
// does not come and go at anything like that rate, so probing a loopback port
// with it would be noise for no news. Costs a failed stat when no hub has ever
// run, which is the usual case.
func (s *Server) watchMCPHub(ctx context.Context) {
	const step = 10 * time.Second

	check := func() {
		found := hub.Detect(ctx)
		s.mu.Lock()
		changed := found != s.mcpHub
		s.mcpHub = found
		s.mu.Unlock()
		// The button appears and disappears on this, so the page has to be told
		// rather than left until something else refetches.
		if changed {
			s.hub.Broadcast("state", nil)
		}
	}

	check()
	ticker := time.NewTicker(step)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

// currentMCPHub is the last look, or nothing when there is no hub to offer.
func (s *Server) currentMCPHub() *hub.Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.mcpHub.Running {
		return nil
	}
	found := s.mcpHub
	return &found
}

// going records why the server is shutting down, for the farewell the pages
// get. Left unset for a shutdown nothing asked for — a signal, or `ade stop`
// — which is what "stopped" already says.
func (s *Server) going(why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goingReason = why
}

func (s *Server) goingWhy() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.goingReason == "" {
		return "stopped"
	}
	return s.goingReason
}

// watchIdle shuts a detached server down once nothing is watching it, so a
// forgotten one stops polling git and gh rather than running until reboot.
func (s *Server) watchIdle(ctx context.Context, stop context.CancelFunc) {
	if s.idle <= 0 {
		return
	}
	const step = time.Minute

	var quiet time.Duration
	ticker := time.NewTicker(step)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// An agent run can outlast a closed tab, and killing the server out from
		// under one would strand it mid-build.
		if s.hub.Clients() > 0 || s.agent.State().Running {
			quiet = 0
			continue
		}
		if quiet += step; quiet >= s.idle {
			slog.Info("no browser attached, shutting down", "idle", s.idle)
			s.going(fmt.Sprintf("nothing was watching it for %s", s.idle))
			stop()
			return
		}
	}
}

// tick is one poll. It recovers so that a single bad diff — an input that makes
// a syntax lexer panic, say — costs one refresh rather than the whole process:
// an unrecovered panic in this goroutine would take the server down with it.
func (s *Server) tick(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("refresh panicked", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	if err := s.refresh(ctx); err != nil {
		slog.Debug("refresh failed", "err", err)
	}
	s.followBranch(ctx)
	s.holdViewRoot(ctx)
	s.watchDoc()
	s.drainCommits(ctx)
}

// watchDoc notices the agent rewriting the open design document. It writes the
// file directly, so without this the browser sits showing whatever it fetched
// when the run started and you have to reload by hand at the right moment.
func (s *Server) watchDoc() {
	if s.docsDir == "" {
		return
	}
	file := s.currentDoc()
	if file == "" {
		return
	}
	version := docs.Version(s.docsDir, file)

	s.mu.Lock()
	changed := version != s.docVersion
	s.docVersion = version
	s.mu.Unlock()

	if changed {
		s.hub.Broadcast("state", nil)
	}
}

// followBranch picks up a checkout under a long-lived server. Without it a
// detached ade keeps describing the branch it started on, silently, which is
// worse than either following or refusing.
func (s *Server) followBranch(ctx context.Context) {
	if s.space().snapshot || s.space().pr != nil {
		return
	}
	ws := s.space()
	// Mid-run the branch is the agent's business, not ours: a build can create
	// one, and rebinding under it would swap the store it is being reviewed in.
	if s.agent.State().Running {
		return
	}
	// A bind would snap the Diff tab home silently; going home rereads it.
	if ws.viewRoot != "" {
		return
	}
	if branch := gitdiff.Branch(ctx, ws.homeRoot); branch != "" && branch != ws.branch {
		slog.Info("branch changed under us", "from", ws.branch, "to", branch)
		if err := s.rebind(ctx, ws.homeRoot); err != nil {
			slog.Warn("could not follow the branch", "err", err)
		}
	}
}

// refresh reloads the diff, re-anchors threads and broadcasts real changes only.
func (s *Server) refresh(ctx context.Context) error {
	w := s.space()
	diff, err := w.source(ctx)
	if err != nil {
		return err
	}
	// Hashed before highlighting, since the classes derive from the content and
	// cannot change the answer.
	hash := hashDiff(diff)

	// An unchanged diff keeps the copy already held, which is the highlighted
	// one. Replacing it would drop the tokens and re-lex on every poll.
	if hash == s.currentHash() {
		if s.refreshWorktree(ctx) {
			s.hub.Broadcast("state", nil)
		}
		return nil
	}

	highlight.Apply(diff)
	words.Apply(diff)

	s.mu.Lock()
	// The diff belongs to the workspace it was read from. A root switch that
	// landed while git ran wins; the next poll reads the new one.
	if s.ws != w {
		s.mu.Unlock()
		return nil
	}
	s.diff, s.diffHash = diff, hash
	s.mu.Unlock()

	s.refreshWorktree(ctx)
	sweptComments(w.store.Reanchor(diff))
	slog.Debug("diff changed", "root", w.viewRoot, "files", len(diff.Files), "mode", diff.Mode)
	s.hub.Broadcast("state", nil)
	return nil
}

func (s *Server) currentHash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.diffHash
}

// sweptComments records what a reanchor pass deleted. Nothing else says it
// happened, and a review that quietly lost a finding is worth a line.
func sweptComments(gone []comments.Thread) {
	for _, thread := range gone {
		slog.Info("swept a comment whose code has gone",
			"thread", thread.ID, "at", fmt.Sprintf("%s:%d", thread.File, thread.Line))
	}
}

func hashDiff(diff *gitdiff.Diff) string {
	data, err := json.Marshal(diff)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	return decodeUpTo(w, r, into, maxBody)
}

func decodeUpTo(w http.ResponseWriter, r *http.Request, into any, limit int64) bool {
	body := http.MaxBytesReader(w, r.Body, limit)
	data, err := io.ReadAll(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "request too large")
		return false
	}
	if err := json.Unmarshal(data, into); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Debug("write response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, comments.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.Repeat("0", 32)
	}
	return hex.EncodeToString(b[:])
}

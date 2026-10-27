package launcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/archive"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/myday"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
	"github.com/owenrumney/ade/internal/wiki"
)

// jobsInterval is how often the clock looks. A minute is the finest a cron
// expression can ask for, so looking oftener would find nothing new.
const jobsInterval = time.Minute

// maxJobBody caps a saved definition. The clock re-reads every job file each
// tick, so an unbounded one is a cost that never goes away.
const maxJobBody = 64 << 10

// maxFiring is how many jobs may be in flight at once. Each is a process on
// the subscription, and a morning where six windows passed should not be six.
const maxFiring = 2

// limitsWarmup is how long the start tick waits for a plan reading before it
// fires without one, since an unreadable window must not stop the clock.
const limitsWarmup = 30 * time.Second

// jobsClock is the launcher's scheduler: which jobs are firing right now, and
// the runner that fires them.
type jobsClock struct {
	mu     sync.Mutex
	firing map[string]bool

	// run performs one fire. nil is a cold `claude -p`; a test stands one in.
	run jobs.Runner
	// start opens a writes job's session. nil is `ade create`; a test stands one in.
	start jobs.Starter
	// distill is the wiki distiller's model. nil is the real one-shot.
	distill wiki.DistillModel
	// archive is the design archiver's model. nil is the real one-shot.
	archive archive.Runner
	// myday is the morning report's model. nil is the real one-shot.
	myday myday.Model

	// inflight is the fires a tick started, so a test can wait one out.
	inflight sync.WaitGroup
}

// jobsList is the jobs this machine has: the user's, and a checkout's own when
// a request names one, since the launcher itself is in no checkout.
func (s *Server) jobsList() ([]jobs.Job, []jobs.Skipped) { return s.jobsListFor("") }

func (s *Server) jobsListFor(dir string) ([]jobs.Job, []jobs.Skipped) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	list, skipped := jobs.LoadAll(dir, home)
	if config, err := settings.Load(); err == nil {
		list = jobs.WithBuiltins(list, config)
		// Off means off: the marks go, so turning it on again starts at the
		// current end of every transcript rather than the week it was off.
		if !config.WikiDistill && s.wiki != nil {
			if err := s.wiki.StopDistilling(); err != nil {
				slog.Debug("forget the distillation watermarks", "err", err)
			}
		}
	}
	return list, skipped
}

// distillStore is the store this fire uses. The launcher outlives an `ade wiki
// init`, so the remote is read again rather than kept from startup.
func (s *Server) distillStore() *wiki.Store {
	config, err := settings.Load()
	if err != nil {
		return s.wiki
	}
	if s.wiki != nil && s.wiki.CachedStatus().Remote == strings.TrimSpace(config.WikiRemote) {
		return s.wiki
	}
	return wiki.New(s.home, config.WikiRemote)
}

// distillWiki is the built-in wiki-distill job. The first fire after turning
// it on only marks where every transcript ends, so nothing old is published.
func (s *Server) distillWiki(ctx context.Context, manual bool) jobs.Outcome {
	started := time.Now()
	store := s.distillStore()
	if store == nil || !store.Enabled() {
		return jobs.Outcome{FiredAt: started, Error: "wiki is disabled; run ade wiki init --remote <url>"}
	}
	// Before the spend gate: marking where the transcripts end costs nothing and
	// calls no model, and a held first fire would leave the feature never on.
	all := transcripts.Scan()
	if !store.DistillStarted() {
		if err := store.StartDistilling(transcriptPaths(all)); err != nil {
			return jobs.Outcome{FiredAt: started, Error: err.Error()}
		}
		return jobs.Outcome{FiredAt: started, Report: "started: sessions are read from here on"}
	}
	if ok, why := jobs.WithinSpend(s.limits.Snapshot(ctx)); !ok {
		return jobs.Outcome{FiredAt: started, Error: "held: " + why}
	}
	repos, err := s.repos()
	if err != nil {
		return jobs.Outcome{FiredAt: started, Error: err.Error()}
	}
	model := s.jobs.distill
	if model == nil {
		dir, err := settings.Dir()
		if err != nil {
			return jobs.Outcome{FiredAt: started, Error: err.Error()}
		}
		model = jobs.DistillModel("", dir)
	}
	out := jobs.FireDistill(ctx, store, jobs.DistillTargets(ctx, repos, all), model, manual)
	s.refreshWiki()
	return out
}

// archiveDesigns is the built-in design-archive job: see jobs.FireArchive for
// what it sweeps.
func (s *Server) archiveDesigns(ctx context.Context) jobs.Outcome {
	started := time.Now()
	if ok, why := jobs.WithinSpend(s.limits.Snapshot(ctx)); !ok {
		return jobs.Outcome{FiredAt: started, Error: "held: " + why}
	}
	repos, err := s.repos()
	if err != nil {
		return jobs.Outcome{FiredAt: started, Error: err.Error()}
	}
	model := s.jobs.archive
	if model == nil {
		dir, err := settings.Dir()
		if err != nil {
			return jobs.Outcome{FiredAt: started, Error: err.Error()}
		}
		model = jobs.ArchiveModel("", dir)
	}
	// The same gate the prune panel takes: the two must not reach for the
	// same document at once.
	release, err := s.holdArchive(ctx)
	if err != nil {
		return jobs.Outcome{FiredAt: started, Error: err.Error()}
	}
	defer release()
	out := jobs.FireArchive(ctx, s.distillStore(), jobs.ArchiveTargets(ctx, repos), model)
	s.refreshWiki()
	return out
}

// myDay is the built-in my-day job: the three lists read fresh, and the
// tickets sized by a model only where the cache has gone stale.
func (s *Server) myDay(ctx context.Context) jobs.Outcome {
	in := myday.Input{Now: time.Now()}

	var mine []ghpr.Request
	var mineErr error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); s.board.RefreshReviews(ctx) }()
	go func() { defer wg.Done(); s.board.RefreshTickets(ctx) }()
	go func() { defer wg.Done(); mine, mineErr = ghpr.MyOpenPRs(ctx, "") }()
	wg.Wait()

	for _, review := range s.board.Reviews() {
		if !review.Muted {
			in.Asked = append(in.Asked, review.Request)
		}
	}
	in.AskedError = s.board.ReviewTroubleNow().Error
	in.Mine = mine
	if mineErr != nil {
		in.MineError = mineErr.Error()
	}
	if tickets := s.board.Tickets(); tickets == nil {
		in.TicketError = "no Linear API key in ade's settings"
	} else {
		in.Tickets, in.TicketError = tickets.Issues, tickets.Error
	}

	// Only the sizing is gated: the two searches and every cached estimate
	// cost nothing, and a held window must not be a silent morning.
	held := ""
	if ok, why := jobs.WithinSpend(s.limits.Snapshot(ctx)); !ok {
		held = why
	}
	model := s.jobs.myday
	if model == nil {
		dir, err := settings.Dir()
		if err != nil {
			return jobs.Outcome{FiredAt: time.Now(), Error: err.Error()}
		}
		model = jobs.MyDayModel("", dir)
	}
	return jobs.FireMyDay(ctx, in, model, held)
}

func transcriptPaths(all []*transcripts.Session) []string {
	paths := make([]string, 0, len(all))
	for _, session := range all {
		paths = append(paths, session.Path)
	}
	return paths
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	list, skipped := s.jobsListFor(strings.TrimSpace(r.URL.Query().Get("dir")))
	if skipped == nil {
		skipped = []jobs.Skipped{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs": s.jobs.mark(jobs.Views(list)), "skipped": skipped,
	})
}

// mark flags the jobs a fire is in flight for.
func (c *jobsClock) mark(views []jobs.View) []jobs.View {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range views {
		views[i].Running = c.firing[views[i].Name]
	}
	return views
}

// handleJobRun fires a job now, whatever its schedule says. The answer waits
// for the run: a click on "Run now" wants the report, not an acknowledgement.
func (s *Server) handleJobRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		// Dir is the checkout to run in, which a writes job started by hand
		// takes from you: the worktree you are in, not the project's main one.
		Dir string `json:"dir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	job, ok := jobs.Find(mustList(s.jobsListFor(strings.TrimSpace(req.Dir))), req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the jobs on this machine")
		return
	}
	// Not the request's context: a closed tab would cancel the run, and the
	// cancellation would be recorded as a failure and move the clock on.
	out, err := s.fireJob(context.WithoutCancel(r.Context()), job, true, strings.TrimSpace(req.Dir))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.hub.Broadcast("jobs", nil)
	writeJSON(w, http.StatusOK, map[string]any{"outcome": out})
}

func (s *Server) handleJobPause(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Paused bool   `json:"paused"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	job, ok := jobs.Find(mustList(s.jobsList()), req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the jobs on this machine")
		return
	}
	state, err := jobs.SetPaused(job, req.Paused, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("jobs", nil)
	writeJSON(w, http.StatusOK, jobs.Viewed(job, state))
}

// handleJobSave writes a user job. The file is the definition, so this is the
// same edit as opening it in an editor, validated the way the loader does.
func (s *Server) handleJobSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		jobs.Draft
		// Mode is what the form meant: "create" refuses a name already taken,
		// "replace" requires one. A default would let one become the other.
		Mode string `json:"mode"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJobBody)
	if !decodeJSON(w, r, &req) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	job, err := jobs.Save(home, req.Draft, req.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("jobs", nil)
	writeJSON(w, http.StatusOK, jobs.Viewed(job, jobs.ReadState(job)))
}

func (s *Server) handleJobDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A fire in flight writes its state back on finishing, which would recreate
	// what Remove deleted and hand it to the next job of that name.
	if s.jobs.isFiring(req.Name) {
		writeError(w, http.StatusConflict, "that job is running; wait for it to finish")
		return
	}
	if err := jobs.Remove(home, req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("jobs", nil)
	writeJSON(w, http.StatusOK, map[string]any{"removed": req.Name})
}

// handleJobSeen puts a report away. It is the whole of reviewing one.
func (s *Server) handleJobSeen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string     `json:"name"`
		FiredAt *time.Time `json:"firedAt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	job, ok := jobs.Find(mustList(s.jobsList()), req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the jobs on this machine")
		return
	}
	at := time.Time{}
	if req.FiredAt != nil {
		at = *req.FiredAt
	}
	state, err := jobs.MarkSeen(job, at)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("jobs", nil)
	writeJSON(w, http.StatusOK, jobs.Viewed(job, state))
}

func mustList(list []jobs.Job, _ []jobs.Skipped) []jobs.Job { return list }

// watchJobs is the clock. The launcher is not a daemon: it looks on start, so
// a job whose window passed while nothing was running fires once.
func (s *Server) watchJobs(ctx context.Context) {
	s.warmLimits(ctx)
	s.tickJobs(ctx)
	ticker := time.NewTicker(jobsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tickJobs(ctx)
		}
	}
}

// warmLimits waits for the spend gate to have a reading. The start tick is the
// one that fires every window that passed, and a cold cache reads as room.
func (s *Server) warmLimits(ctx context.Context) {
	for range int(limitsWarmup / time.Second) {
		state := s.limits.Snapshot(ctx)
		if state.Claude != nil || state.Codex != nil || state.Error != "" {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// tickJobs fires everything due, in the background: one slow job must not
// hold up the next tick, and the clock never blocks on a run.
func (s *Server) tickJobs(ctx context.Context) {
	list, _ := s.jobsList()
	if len(list) == 0 {
		return
	}
	now := time.Now()
	for _, job := range jobs.Yours(due(list, now)) {
		if ok, why := jobs.WithinSpend(s.limits.Snapshot(ctx)); !ok {
			slog.Info("holding scheduled jobs", "job", job.Name, "why", why)
			return
		}
		// Claimed here rather than inside the goroutine: the cap has to fall on
		// the jobs this tick put last, not on whichever goroutine starts last.
		if err := s.jobs.claim(job.Name); err != nil {
			slog.Debug("scheduled job did not fire", "job", job.Name, "err", err)
			continue
		}
		s.jobs.inflight.Go(func() {
			defer s.jobs.release(job.Name)
			s.fireClaimed(ctx, job, false, "")
			s.hub.Broadcast("jobs", nil)
		})
	}
}

// due is the jobs whose window has come.
func due(list []jobs.Job, now time.Time) []jobs.Job {
	var out []jobs.Job
	for _, job := range list {
		if jobs.Due(job, jobs.ReadState(job), now) {
			out = append(out, job)
		}
	}
	return out
}

// fire runs one job and records what came of it. A project that is not on this
// machine is an outcome saying so, not a silence.
func (s *Server) fire(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
	return s.fireJob(ctx, job, false, "")
}

// fireJob is fire, told whether someone pressed the button. Only a built-in
// kind cares: the button skips its quiet period and its brake.
func (s *Server) fireJob(ctx context.Context, job jobs.Job, manual bool, dir string) (jobs.Outcome, error) {
	if err := s.jobs.claim(job.Name); err != nil {
		return jobs.Outcome{}, err
	}
	defer s.jobs.release(job.Name)
	return s.fireClaimed(ctx, job, manual, dir), nil
}

// fireClaimed is fireJob with the slot already taken, for a caller that had to
// know whether it got one before starting the goroutine.
func (s *Server) fireClaimed(ctx context.Context, job jobs.Job, manual bool, dir string) jobs.Outcome {
	state := jobs.ReadState(job)
	now := time.Now()
	if job.Kind == jobs.KindDesignArchive {
		out := s.archiveDesigns(ctx)
		if _, err := jobs.Record(job, out, "", now); err != nil {
			slog.Warn("record a design archive", "err", err)
		}
		return out
	}
	if job.Kind == jobs.KindWikiDistill {
		out := s.distillWiki(ctx, manual)
		if _, err := jobs.Record(job, out, "", now); err != nil {
			slog.Warn("record a wiki distillation", "err", err)
		}
		return out
	}
	if job.Kind == jobs.KindMyDay {
		out := s.myDay(ctx)
		if _, err := jobs.Record(job, out, "", now); err != nil {
			slog.Warn("record a my-day report", "err", err)
		}
		return out
	}
	if job.Allow == jobs.Writes {
		where, err := s.writesCheckout(ctx, job, dir)
		if err != nil {
			out := jobs.Outcome{FiredAt: now, Error: err.Error()}
			if _, err := jobs.Record(job, out, "", now); err != nil {
				slog.Warn("record a job that could not start", "job", job.Name, "err", err)
			}
			return out
		}
		return jobs.Start(ctx, job, where, s.jobs.starter())
	}
	checkout, err := s.jobCheckout(ctx, job)
	if err != nil {
		out := jobs.Outcome{FiredAt: now, Error: err.Error()}
		if _, err := jobs.Record(job, out, state.Handover, now); err != nil {
			slog.Warn("record a skipped job", "job", job.Name, "err", err)
		}
		return out
	}

	return jobs.Fire(ctx, job, checkout, s.jobs.runner())
}

// writesCheckout is where a writes job makes its worktree: the checkout named
// by hand, which must be one, or else the project's main one.
func (s *Server) writesCheckout(ctx context.Context, job jobs.Job, dir string) (string, error) {
	if dir == "" {
		return s.jobCheckout(ctx, job)
	}
	root, err := gitdiff.RepoRoot(ctx, dir)
	if err != nil {
		return "", fmt.Errorf("%s is not a checkout a job can work beside", dir)
	}
	return root, nil
}

// jobCheckout resolves a job's project to a live checkout, or the launcher's
// own directory for a job that belongs to no project.
func (s *Server) jobCheckout(ctx context.Context, job jobs.Job) (string, error) {
	var repos []sessions.Repo
	if job.Project != "" {
		found, err := s.repos()
		if err != nil {
			return "", err
		}
		repos = found
	}
	here, err := os.Getwd()
	if err != nil {
		here = s.home
	}
	return jobs.Checkout(ctx, job, repos, here)
}

func (c *jobsClock) runner() jobs.Runner {
	if c.run != nil {
		return c.run
	}
	return jobs.ColdRunner("")
}

func (c *jobsClock) starter() jobs.Starter {
	if c.start != nil {
		return c.start
	}
	return jobs.CreateStarter()
}

// claim takes the one slot a job has, and a slot under the cap on how many
// fires may be in flight.
func (c *jobsClock) claim(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.firing == nil {
		c.firing = map[string]bool{}
	}
	if c.firing[name] {
		return errors.New("that job is already running")
	}
	if len(c.firing) >= maxFiring {
		return fmt.Errorf("%d jobs are already running, %d at most", len(c.firing), maxFiring)
	}
	c.firing[name] = true
	return nil
}

// busy is whether a fire is in flight, which is work the launcher is doing
// with nobody attached: the idle shutdown has to wait for it.
func (c *jobsClock) busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.firing) > 0
}

// isFiring is whether a fire for name is in flight right now.
func (c *jobsClock) isFiring(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.firing[name]
}

func (c *jobsClock) release(name string) {
	c.mu.Lock()
	delete(c.firing, name)
	c.mu.Unlock()
}

package launcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/myday"
	"github.com/owenrumney/ade/internal/settings"
)

// maxFiring is how many jobs may be in flight at once. Each is a process on
// the subscription, and a morning where six windows passed should not be six.
const maxFiring = 2

// jobsClock is the launcher's scheduler: which jobs are firing right now, and
// the runner that fires them.
type jobsClock struct {
	mu     sync.Mutex
	firing map[string]bool

	// myday is the morning report's model. nil is the real one-shot.
	myday myday.Model
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

	model := s.jobs.myday
	if model == nil {
		dir, err := settings.Dir()
		if err != nil {
			return jobs.Outcome{FiredAt: time.Now(), Error: err.Error()}
		}
		model = jobs.MyDayModel("", dir)
	}
	return jobs.FireMyDay(ctx, in, model, "")
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"jobs": s.jobs.mark(jobs.Views(s.jobsList())), "skipped": []jobs.Skipped{},
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
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	job, ok := jobs.Find(s.jobsList(), req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the jobs on this machine")
		return
	}
	// Not the request's context: a closed tab would cancel the run, and the
	// cancellation would be recorded as a failure and move the clock on.
	out, err := s.fireJob(context.WithoutCancel(r.Context()), job)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.hub.Broadcast("jobs", nil)
	writeJSON(w, http.StatusOK, map[string]any{"outcome": out})
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
	job, ok := jobs.Find(s.jobsList(), req.Name)
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

func (s *Server) fireJob(ctx context.Context, job jobs.Job) (jobs.Outcome, error) {
	if err := s.jobs.claim(job.Name); err != nil {
		return jobs.Outcome{}, err
	}
	defer s.jobs.release(job.Name)
	out := s.myDay(ctx)
	if _, err := jobs.Record(job, out); err != nil {
		slog.Warn("record a my-day report", "err", err)
	}
	return out, nil
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

func (c *jobsClock) release(name string) {
	c.mu.Lock()
	delete(c.firing, name)
	c.mu.Unlock()
}

// jobsList is the one job left: the report on the day.
func (s *Server) jobsList() []jobs.Job { return []jobs.Job{jobs.MyDayJob()} }

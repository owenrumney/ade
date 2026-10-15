package launcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/owenrumney/ade/internal/dashboard"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
)

// dashboardInterval is how often the kiosk is told, and usageEvery how many
// of those ticks carry the transcript totals as well as the windows.
const (
	dashboardInterval = 2 * time.Minute
	usageEvery        = 5
)

// watchDashboard reports on a tick of its own, not watchLimits': that one
// skips while no browser is attached, which is when a wall display matters.
// Read every tick: a worker is told where to report after its launcher starts.
func (s *Server) watchDashboard(ctx context.Context) {
	ticker := time.NewTicker(dashboardInterval)
	defer ticker.Stop()

	var (
		config settings.Dashboard
		to     *dashboard.Reporter
		stop   context.CancelFunc
		who    dashboard.Accounts
		tick   int
	)
	defer func() {
		if stop != nil {
			stop()
		}
	}()

	for {
		if cfg, err := settings.Load(); err == nil && cfg.Dashboard != config {
			config = cfg.Dashboard
			if stop != nil {
				stop()
			}
			to, stop = nil, nil
			if config.On() {
				to, stop = reporterFor(ctx, config)
				who = dashboard.Accounts{Claude: config.Claude, Codex: config.Codex}
				if who.Claude == "" && who.Codex == "" {
					slog.Warn("the dashboard gets no subscription windows from this machine",
						"why", "neither claude_account nor codex_account is set")
				}
			}
			tick = 0
		}
		if to != nil {
			s.reportDashboard(ctx, to, who, tick%usageEvery == 0)
			tick++
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reporterFor is a reporter and the way to stop its goroutine when the kiosk
// changes. Separate so the cancel is returned rather than held in a loop.
func reporterFor(ctx context.Context, config settings.Dashboard) (*dashboard.Reporter, context.CancelFunc) {
	inner, stop := context.WithCancel(ctx)
	return dashboard.New(inner, config.URL, config.Token, config.Machine()), stop
}

// reportDashboard sends the windows, and the totals only when asked: reading
// those stats every transcript on the machine. See the design.
func (s *Server) reportDashboard(ctx context.Context, to *dashboard.Reporter, who dashboard.Accounts, usage bool) {
	host := to.Host()
	now := time.Now()
	to.Send(dashboard.Subscriptions(s.limits.Snapshot(ctx), who, host)...)
	operations := s.bus.operations(now)
	additions, deletions := changedLines(ctx, operations.Dirs)
	to.Send(dashboard.Operations(operations.Live, operations.Queued, operations.QueueWait, additions, deletions, host, now)...)
	if usage {
		to.Send(dashboard.UsageByAgent(transcripts.StatisticsByAgent(transcripts.Scan()), who, host, now, dashboard.SparkDays)...)
	}
}

func changedLines(ctx context.Context, dirs []string) (int, int) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var additions, deletions int
	for _, dir := range dirs {
		diff, err := gitdiff.Load(ctx, gitdiff.Options{Repo: dir, Scope: gitdiff.ScopeBranch})
		if err != nil {
			continue
		}
		for _, file := range diff.Files {
			additions += file.Additions
			deletions += file.Deletions
		}
	}
	return additions, deletions
}

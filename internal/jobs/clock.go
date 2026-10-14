package jobs

import (
	"fmt"
	"time"

	"github.com/owenrumney/ade/internal/limits"
)

// SpendCeiling is how full the worst plan window may be before jobs stop
// firing. See the design doc.
const SpendCeiling = 85.0

// Due is whether a job should fire now: on the first tick for one that has
// never fired, and once for one whose window has passed, however many passed.
func Due(job Job, state State, now time.Time) bool {
	if state.Paused || job.Every.Manual() {
		return false
	}
	if state.NextDue.IsZero() {
		// A fired job with no next due matched nothing in the search: 31 February
		// is a job that never fires again, not one due every minute.
		return state.LastFired.IsZero()
	}
	return !now.Before(state.NextDue)
}

// WithinSpend is whether there is enough of the plan left to fire a job. Over
// the ceiling a job defers: the window has stopped, not the schedule.
func WithinSpend(state limits.State) (bool, string) {
	for _, provider := range []*limits.Limits{state.Claude, state.Codex} {
		if provider == nil {
			continue
		}
		worst, found := provider.Worst()
		if found && worst.Percent >= SpendCeiling {
			return false, fmt.Sprintf("%s %s window is %.0f%% used", provider.Provider, worst.Name, worst.Percent)
		}
	}
	return true, ""
}

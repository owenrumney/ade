package launcher

import (
	"net/http"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/transcripts"
)

// windowShares is how many directories are named. Past a few it stops
// answering "what do I stop" and becomes a list to read.
const windowShares = 4

// spendTTL is how long a breakdown is good for. It costs a parse of every
// transcript that ran inside the window, so it is not done per request.
const spendTTL = time.Minute

// windowResponse is what the usage panel shows under the 5h bar: where the
// window went, and the window it is talking about.
type windowResponse struct {
	Since  *time.Time          `json:"since,omitempty"`
	Shares []transcripts.Share `json:"shares,omitempty"`
}

// spendCache keeps the last breakdown, since scanning is the expensive part.
type spendCache struct {
	mu       sync.Mutex
	at       time.Time
	since    time.Time
	out      []transcripts.Share
	scanning bool
}

// handleWindowSpend is which checkouts spent the current 5h window. With no
// plan reading there is no window to slice, so it answers empty.
func (s *Server) handleWindowSpend(w http.ResponseWriter, r *http.Request) {
	// The launcher's context, not the request's: a Snapshot may start the
	// reading, and the response returning would then cancel it.
	since, ok := shortWindowStart(s.limits.Snapshot(s.ctx))
	if !ok {
		writeJSON(w, http.StatusOK, windowResponse{})
		return
	}
	writeJSON(w, http.StatusOK, windowResponse{Since: &since, Shares: s.spend.shares(since)})
}

// shares is the breakdown for a window, reusing the last one while it is warm.
// A caller takes what is held rather than queueing behind a running scan.
func (c *spendCache) shares(since time.Time) []transcripts.Share {
	c.mu.Lock()
	same := c.since.Equal(since)
	if (same && time.Since(c.at) < spendTTL) || c.scanning {
		var out []transcripts.Share
		if same {
			out = c.out
		}
		c.mu.Unlock()
		return out
	}
	c.scanning = true
	c.mu.Unlock()

	got := transcripts.InWindow(transcripts.Scan(), since)
	if len(got) > windowShares {
		got = got[:windowShares]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.since, c.out, c.scanning = time.Now(), since, got, false
	return got
}

// shortWindowStart is when the 5h window opened, read back from its reset.
func shortWindowStart(state limits.State) (time.Time, bool) {
	for _, l := range []*limits.Limits{state.Claude, state.Codex} {
		if l == nil {
			continue
		}
		for _, win := range l.Windows {
			if win.Name == "5h" && !win.ResetsAt.IsZero() {
				return win.ResetsAt.Add(-5 * time.Hour), true
			}
		}
	}
	return time.Time{}, false
}

package launcher

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/transcripts"
)

// statsTTL is how long a summary is good for. Reading one is incremental, but
// it still stats every transcript on the machine.
const statsTTL = time.Minute

// statsCache is the last machine-wide summary. reading holds one scan at a
// time; mu is only ever held around the fields.
type statsCache struct {
	mu      sync.Mutex
	at      time.Time
	out     transcripts.Stats
	reading sync.Mutex
}

// handleStats reads local coding-agent transcripts; it never contacts a
// provider.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.stats.read())
}

// read is the last summary while it is warm, and a fresh one otherwise.
func (c *statsCache) read() transcripts.Stats {
	if out, ok := c.warm(); ok {
		return out
	}
	c.reading.Lock()
	defer c.reading.Unlock()
	// Another scan may have finished while this one waited for it.
	if out, ok := c.warm(); ok {
		return out
	}
	out := transcripts.Statistics(transcripts.Scan())

	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.out = time.Now(), out
	return out
}

func (c *statsCache) warm() (transcripts.Stats, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < statsTTL {
		return c.out, true
	}
	return transcripts.Stats{}, false
}

// warmStats parses the transcripts once at startup so the first look at the
// page is not the one that pays for it. Nothing waits on this.
func (s *Server) warmStats(ctx context.Context) {
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		s.stats.read()
	}
}

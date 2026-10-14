package launcher

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/remote"
	"github.com/owenrumney/ade/internal/settings"
)

// loadFresh is how long a reading stands. Short enough to watch a build land
// on a machine, long enough that a page open is not an ssh round trip each.
const loadFresh = 10 * time.Second

// loadRow is one machine's pressure, as the page draws it.
type loadRow struct {
	remote.Load
	Label string  `json:"label,omitempty"`
	CPU   float64 `json:"cpu"`
}

type loadCache struct {
	mu   sync.Mutex
	at   time.Time
	rows []loadRow
}

// hostLabels is what the settings call each machine, for rows that would
// otherwise show only an ssh alias. Read once, not once per machine.
func hostLabels() map[string]string {
	cfg, err := settings.Load()
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, host := range cfg.Hosts {
		out[strings.TrimSpace(host.Name)] = host.Label
	}
	return out
}

func (s *Server) handleHostLoad(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hosts": s.load.read(s.sweepLoad)})
}

// read is the last sweep while it is warm, and a fresh one otherwise. An empty
// sweep is not kept: nothing to show is a failure, not a reading.
func (c *loadCache) read(sweep func() []loadRow) []loadRow {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < loadFresh {
		return c.rows
	}
	rows := sweep()
	if len(rows) == 0 {
		return rows
	}
	c.at, c.rows = time.Now(), rows
	return rows
}

// sweepLoad asks every connected machine at once, on its own deadline: a reader
// that goes away must not cancel the sweep. A machine that fails is left out.
func (s *Server) sweepLoad() []loadRow {
	live := map[string]*remote.Host{}
	s.mu.Lock()
	for name, conn := range s.hosts {
		live[name] = conn.host
	}
	s.mu.Unlock()
	if len(live) == 0 {
		return []loadRow{}
	}

	labels := hostLabels()
	ctx, cancel := context.WithTimeout(context.Background(), hostAsk)
	defer cancel()
	var mu sync.Mutex
	rows := []loadRow{}
	var wg sync.WaitGroup
	for name, host := range live {
		wg.Add(1)
		go func(name string, host *remote.Host) {
			defer wg.Done()
			load, err := host.Load(ctx)
			if err != nil {
				slog.Debug("a machine did not report its load", "host", name, "err", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			rows = append(rows, loadRow{Load: load, Label: labels[name], CPU: load.CPU()})
		}(name, host)
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return rows
}

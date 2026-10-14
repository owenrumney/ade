package launcher

import (
	"net/http"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/repostats"
	"github.com/owenrumney/ade/internal/transcripts"
)

// repoTTL is how long a reading stands. Counting a million lines is a read of
// every tracked file, and the page is opened and closed, not watched.
const repoTTL = 2 * time.Minute

// repoCache holds the last reading per directory.
type repoCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	out  map[string]*repostats.Stats
	busy sync.Mutex
}

// handleRepo answers what one checkout is and what ade has done in it. dir is
// the folder the editor has open; nothing else names a repository here.
func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeError(w, http.StatusBadRequest, "dir is required")
		return
	}
	if out, ok := s.repoPages.warm(dir); ok {
		writeJSON(w, http.StatusOK, out)
		return
	}

	s.repoPages.busy.Lock()
	defer s.repoPages.busy.Unlock()
	if out, ok := s.repoPages.warm(dir); ok {
		writeJSON(w, http.StatusOK, out)
		return
	}

	out, err := repostats.Collect(r.Context(), dir, transcripts.Scan(), time.Now())
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.repoPages.put(dir, out)
	writeJSON(w, http.StatusOK, out)
}

func (c *repoCache) warm(dir string) (*repostats.Stats, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.at[dir]
	if !ok || time.Since(at) > repoTTL {
		return nil, false
	}
	return c.out[dir], true
}

func (c *repoCache) put(dir string, out *repostats.Stats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at == nil {
		c.at, c.out = map[string]time.Time{}, map[string]*repostats.Stats{}
	}
	c.at[dir], c.out[dir] = time.Now(), out
}

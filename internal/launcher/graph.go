package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
)

// handleGraphStop shuts a ade down, from the graph or the rail. A session on a
// worker is stopped by asking that worker, which is the only one that has it.
func (s *Server) handleGraphStop(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.graphTarget(w, r)
	if !ok {
		return
	}
	if err := sessions.StopServer(dir); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// graphTarget reads the checkout a graph action names, and refuses one the
// scan did not offer: these act on processes, so only somewhere ade knows.
//
// The boundary is "same user, same machine": the allow-list of found
// checkouts bounds which directories, and the launcher's own token bounds
// who, since the target ade's token is read from its record on disk. A leak
// of this token reaches every ade on the box.
func (s *Server) graphTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return "", false
	}
	all, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return "", false
	}
	if dir, ok := sessions.FindCheckout(all, req.Dir); ok {
		return dir.Path, true
	}
	// A worktree made a moment ago is not in the scan yet, and its row is
	// the one you most want to stop.
	if dir, ok := s.announcedIn(req.Dir); ok {
		return dir, true
	}
	writeError(w, http.StatusBadRequest, "that is not one of the checkouts on this machine")
	return "", false
}

func (s *Server) announcedIn(dir string) (string, bool) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", false
	}
	s.mu.Lock()
	rows := make([]Serving, 0, len(s.served))
	for _, row := range s.served {
		rows = append(rows, row)
	}
	s.mu.Unlock()
	for _, row := range rows {
		if row.Dir == dir && serves(row.Dir, row.Port) {
			return row.Dir, true
		}
	}
	return "", false
}

// liveInterval is how often the live set is probed: nothing announces a ade
// starting or dying elsewhere on the machine.
const liveInterval = 5 * time.Second

// watchLive tells open pages when what is running changed, so the page needs no
// timer. It sweeps only while somebody is watching: a probe per checkout costs.
func (s *Server) watchLive(ctx context.Context) {
	ticker := time.NewTicker(s.liveEvery)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.hub.Clients() == 0 {
				continue
			}
			if now := s.liveKey(ctx); now != last {
				last = now
				s.hub.Broadcast("live", nil)
			}
		}
	}
}

// liveKey is every live checkout and what its agent is doing, as one string.
// Compared rather than diffed: the page re-reads the whole graph either way.
func (s *Server) liveKey(ctx context.Context) string {
	repos, err := s.repos()
	if err != nil {
		return ""
	}
	live := []sessions.GraphCheckout{}
	for _, repo := range sessions.Graph(ctx, repos, s.probe) {
		for _, dir := range repo.Checkouts {
			if dir.Server != nil {
				live = append(live, dir)
			}
		}
	}
	key, err := json.Marshal(live)
	if err != nil {
		return ""
	}
	return string(key)
}

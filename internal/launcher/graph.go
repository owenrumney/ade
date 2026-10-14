package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/machine"
	"github.com/owenrumney/ade/internal/sessions"
)

// handleGraph is the machine right now: every checkout, and what is running
// on each. Drawn by the page rather than listed, so a glance says where the
// work is.
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if machine := strings.TrimSpace(r.URL.Query().Get("machine")); machine != "" {
		s.graphOnHost(w, r, machine)
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": sessions.Graph(r.Context(), repos, s.probe)})
}

// handleGraphCancel stops what a ade is running, from the graph. One on a
// worker is cancelled by asking that worker, as a stop is.
func (s *Server) handleGraphCancel(w http.ResponseWriter, r *http.Request) {
	if machine, dir, ok := stopElsewhere(r); ok {
		s.tellHost(w, r, machine, dir, "/api/graph/cancel", "cancel")
		return
	}
	dir, ok := s.graphTarget(w, r)
	if !ok {
		return
	}
	if err := sessions.CancelAgent(r.Context(), dir); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGraphStop shuts a ade down, from the graph or the rail. A session on a
// worker is stopped by asking that worker, which is the only one that has it.
func (s *Server) handleGraphStop(w http.ResponseWriter, r *http.Request) {
	if machine, dir, ok := stopElsewhere(r); ok {
		s.tellHost(w, r, machine, dir, "/api/graph/stop", "stop")
		return
	}
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

// stopElsewhere reads the machine a stop names, rewinding the body for the
// local path.
func stopElsewhere(r *http.Request) (string, string, bool) {
	body, err := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return "", "", false
	}
	var req struct {
		Machine string `json:"machine"`
		Dir     string `json:"dir"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", "", false
	}
	machine := strings.TrimSpace(req.Machine)
	return machine, req.Dir, machine != ""
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
	dir, ok := sessions.FindCheckout(all, req.Dir)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the checkouts on this machine")
		return "", false
	}
	return dir.Path, true
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

// watchMachine leaves the graph in ~/.ade/machine, for a reader with no port
// to ask. Its own loop: watchLive sweeps only while a page is open.
func (s *Server) watchMachine(ctx context.Context) {
	dir, err := machine.Dir()
	if err != nil {
		slog.Debug("machine snapshot: no state directory", "err", err)
		return
	}
	src := machine.Source{
		Dir:    dir,
		Writer: machine.Writer{PID: os.Getpid(), Kind: "launcher"},
		Repos:  s.repos,
		Probe:  s.probe,
	}
	src.Loop(ctx)
}

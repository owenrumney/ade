package launcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// ActiveSession is one ade that announced itself, as the rail draws it. No
// scan: an announced checkout need not be under a scan root at all.
type ActiveSession struct {
	ID     string               `json:"id"`
	Dir    string               `json:"dir"`
	Name   string               `json:"name"`
	Repo   string               `json:"repo,omitempty"`
	Branch string               `json:"branch,omitempty"`
	Agent  *session.AgentHealth `json:"agent,omitempty"`

	// Machine is the host it is on, empty for this one. No address rides
	// along: a list any loopback page can GET is no place for a token.
	Machine string `json:"machine,omitempty"`
}

// activeFresh is how long an answer stands. Every open page asks on each
// launcher event, and one ask probes every announcement.
const activeFresh = time.Second

// servingFile keeps the announced set across a launcher that idled out: the
// ades it was told about are still running, and none of them announces twice.
const servingFile = "serving.json"

// handleActive is every announced session, re-checked and asked what it is
// doing. The scan answers a different question: where a session could start.
func (s *Server) handleActive(w http.ResponseWriter, r *http.Request) {
	// local=1 is another launcher asking, and it wants this machine's own rows.
	if r.URL.Query().Get("local") == "1" {
		writeJSON(w, http.StatusOK, s.localSessions(r.Context()))
		return
	}
	writeJSON(w, http.StatusOK, s.activeSessions(r.Context()))
}

func (s *Server) activeSessions(ctx context.Context) []ActiveSession {
	rows := s.localSessions(ctx)
	hosts := s.hostSessions(ctx)
	if len(hosts) == 0 {
		return rows
	}
	out := make([]ActiveSession, 0, len(rows)+len(hosts))
	return append(append(out, rows...), hosts...)
}

func (s *Server) localSessions(ctx context.Context) []ActiveSession {
	if rows, ok := s.freshActive(); ok {
		return rows
	}
	// One sweep at a time: three tabs asking at once is still one round of
	// probes, and the second caller takes what the first just worked out.
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	if rows, ok := s.freshActive(); ok {
		return rows
	}

	s.mu.Lock()
	rows := make([]Serving, 0, len(s.served))
	for _, row := range s.served {
		rows = append(rows, row)
	}
	s.mu.Unlock()

	found := make([]ActiveSession, len(rows))
	gone := make([]bool, len(rows))
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(slot *ActiveSession, dead *bool, row Serving) {
			defer wg.Done()
			rec, ok := session.Read(session.Path(sessions.GitDir(row.Dir)))
			// The record decides whether the announcement survives. A probe that
			// did not answer is a busy ade, or a cancelled request, not a gone one.
			if !ok || !session.Running(rec.PID) || portOf(rec.URL) != row.Port {
				*dead = true
				return
			}
			*slot = ActiveSession{
				ID: row.ID, Dir: row.Dir,
				Name: filepath.Base(row.Dir), Repo: repoOf(row.Dir),
			}
			health, live := session.Probe(ctx, rec)
			if !live {
				return
			}
			// The ade announced a fresh id when the agent named a new session;
			// this row is the one it left behind.
			if health.Agent != nil && health.Agent.Session != "" && health.Agent.Session != row.ID {
				*slot, *dead = ActiveSession{}, true
				return
			}
			slot.Branch, slot.Agent = health.Branch, health.Agent
		}(&found[i], &gone[i], rows[i])
	}
	wg.Wait()

	out := make([]ActiveSession, 0, len(found))
	dead := make([]Serving, 0, len(found))
	for i, row := range found {
		if gone[i] {
			dead = append(dead, rows[i])
			continue
		}
		out = append(out, row)
	}
	s.forget(dead)
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	if ctx.Err() == nil {
		s.mu.Lock()
		s.active, s.activeAt = out, time.Now()
		s.mu.Unlock()
	}
	return out
}

// freshActive is the last answer while it still stands.
func (s *Server) freshActive() ([]ActiveSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || time.Since(s.activeAt) >= activeFresh {
		return nil, false
	}
	return s.active, true
}

// forget drops what no longer answers. The whole row, not the id: an ade that
// re-announced during the sweep is a new row under the same id, and stays.
func (s *Server) forget(rows []Serving) {
	if len(rows) == 0 {
		return
	}
	s.mu.Lock()
	dropped := false
	for _, row := range rows {
		if s.served[row.ID] == row {
			delete(s.served, row.ID)
			dropped = true
		}
	}
	s.mu.Unlock()
	if dropped {
		s.remember()
	}
}

// remember writes the announced set down. recall reads it back at startup:
// each ade announces once, so a launcher that came back knows none of them.
func (s *Server) remember() {
	s.mu.Lock()
	rows := make([]Serving, 0, len(s.served))
	for _, row := range s.served {
		rows = append(rows, row)
	}
	s.mu.Unlock()
	data, err := json.Marshal(rows)
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.home, 0o750); err != nil {
		slog.Debug("keep the announced set", "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join(s.home, servingFile), data, 0o600); err != nil {
		slog.Debug("keep the announced set", "err", err)
	}
}

func (s *Server) recall() {
	data, err := os.ReadFile(filepath.Join(s.home, servingFile)) // #nosec G304 -- ade's own state directory
	if err != nil {
		return
	}
	var rows []Serving
	if json.Unmarshal(data, &rows) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range rows {
		// Checked as an announcement is: what the file names may be long dead.
		if row.ID != "" && serves(row.Dir, row.Port) {
			s.onlyServer(row.Dir, row.ID)
			s.served[row.ID] = row
		}
	}
}

// repoOf names the repository a checkout belongs to, so two worktrees of one
// repository are not two unrelated rows on the rail.
func repoOf(dir string) string {
	if main, ok := sessions.MainCheckout(dir); ok {
		return filepath.Base(main)
	}
	return filepath.Base(dir)
}

package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/homeassistant"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// Waiting is the same row the walk produces; an alias, not a second one.
type Waiting = sessions.Row

// waitingTTL is how long a reading is good for; the page and sweep share one.
const waitingTTL = 15 * time.Second

// waitingCache is the last reading of what is waiting on you.
type waitingCache struct {
	mu   sync.Mutex
	at   time.Time
	rows []Waiting
}

func (c *waitingCache) read(walk func() []Waiting) []Waiting {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < waitingTTL {
		return c.rows
	}
	c.rows, c.at = walk(), time.Now()
	return c.rows
}

// forget drops the reading, so a dismissal shows up at once.
func (c *waitingCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
}

func (s *Server) handleWaiting(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"waiting": s.waiting(r.Context())})
}

// handleSay answers a session from wherever you are. The message queues
// behind whatever that session is doing; nothing is interrupted.
func (s *Server) handleSay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "say something")
		return
	}
	if !s.knownSession(req.ID) {
		writeError(w, http.StatusBadRequest, "that is not one of this machine's sessions")
		return
	}
	url, err := s.sessionURL(r.Context(), req.ID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	body, err := json.Marshal(map[string]string{"message": message})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sendFirst(r.Context(), url, string(body)); err != nil {
		writeError(w, http.StatusBadGateway, "carry that to the session: "+err.Error())
		return
	}
	// Answered is dealt with: the bell clears now rather than when the agent
	// next speaks.
	if err := settings.MarkSeen(req.ID, time.Now()); err != nil {
		slog.Debug("mark a session seen after answering it", "err", err)
	}
	s.wait.forget()
	s.hub.Broadcast("waiting", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleSeen marks a session looked at without opening it.
func (s *Server) handleSeen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Off the list of sessions on this machine, not off the request: an id
	// nothing names would sit in the file forever.
	if !s.knownSession(req.ID) {
		writeError(w, http.StatusBadRequest, "that is not one of this machine's sessions")
		return
	}
	if err := settings.MarkSeen(req.ID, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.wait.forget()
	s.hub.Broadcast("waiting", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) knownSession(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	repos, err := s.repos()
	if err != nil {
		return false
	}
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			if _, ok := sessions.Find(dir.Sessions, dir.Path, id); ok {
				return true
			}
		}
	}
	return false
}

// waiting is every session that has spoken since you last looked.
func (s *Server) waiting(ctx context.Context) []Waiting {
	return s.wait.read(func() []Waiting { return s.walkWaiting(ctx) })
}

func (s *Server) walkWaiting(ctx context.Context) []Waiting {
	repos, err := s.repos()
	if err != nil {
		return nil
	}
	// Nothing found is a scan that failed, not a machine with no sessions:
	// pruning against it would forget every look you have ever taken.
	if known := sessionIDs(repos); len(known) > 0 {
		if err := settings.KeepSeen(known); err != nil {
			slog.Debug("prune the seen sessions", "err", err)
		}
	}
	return sessions.WaitingIn(repos, s.runningSessions(ctx, repos), settings.Seen(), settings.Archived())
}

func sessionIDs(repos []sessions.Repo) map[string]bool {
	out := map[string]bool{}
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			for _, session := range dir.Sessions {
				out[session.ID] = true
			}
		}
	}
	return out
}

// runningSessions is what an agent is working on, by id — not by checkout.
func (s *Server) runningSessions(ctx context.Context, repos []sessions.Repo) map[string]bool {
	out := map[string]bool{}
	for _, repo := range sessions.Graph(ctx, repos, s.probe) {
		for _, dir := range repo.Checkouts {
			if dir.Server == nil || dir.Server.Agent == nil {
				continue
			}
			if agent := dir.Server.Agent; agent.Running && agent.Session != "" {
				out[agent.Session] = true
			}
		}
	}
	return out
}

// notifyWait bounds one notification; notifyBurst bounds one sweep.
const (
	notifyWait  = 10 * time.Second
	notifyBurst = 4
)

// announce is for when no page is open; a page announces its own.
func (s *Server) announce(ctx context.Context, now []Waiting) {
	fresh := s.freshlyWaiting(now)
	if len(fresh) == 0 || s.hub.Clients() > 0 {
		return
	}
	cfg, err := settings.Load()
	if err != nil || strings.TrimSpace(cfg.HomeAssistant.NotifyTarget) == "" {
		return
	}
	client := homeassistant.Client{URL: cfg.HomeAssistant.URL, Token: cfg.HomeAssistant.Token}
	if len(fresh) > notifyBurst {
		fresh = fresh[:notifyBurst]
	}
	for _, row := range fresh {
		name := row.Branch
		if name == "" {
			name = row.Name
		}
		body := row.Title
		if body == "" {
			body = "is waiting for you"
		}
		// Its own deadline: a host that never answers would stop the sweep.
		send, cancel := context.WithTimeout(ctx, notifyWait)
		err := client.Notify(send, cfg.HomeAssistant.NotifyTarget, "ade · "+name, body)
		cancel()
		if err != nil {
			// One unreachable host is every unreachable host this sweep.
			slog.Debug("notify about a waiting session", "err", err)
			return
		}
	}
}

// freshlyWaiting is what was not waiting last time; the first sweep is backlog.
func (s *Server) freshlyWaiting(now []Waiting) []Waiting {
	s.announcedMu.Lock()
	defer s.announcedMu.Unlock()
	was := s.announced
	s.announced = make(map[string]bool, len(now))
	for _, row := range now {
		s.announced[row.ID] = true
	}
	if was == nil {
		return nil
	}
	var fresh []Waiting
	for _, row := range now {
		if !was[row.ID] {
			fresh = append(fresh, row)
		}
	}
	return fresh
}

// waitingEvery is slower than the live sweep: it walks every checkout.
const waitingEvery = 30 * time.Second

// watchWaiting runs with no page attached; that is the point of notifying.
func (s *Server) watchWaiting(ctx context.Context) {
	ticker := time.NewTicker(waitingEvery)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.wait.forget()
			now := s.waiting(ctx)
			s.announce(ctx, now)
			if key := waitingKey(now); key != last {
				last = key
				s.hub.Broadcast("waiting", nil)
			}
		}
	}
}

// waitingKey is the list as one string, so an unchanged one is not broadcast.
func waitingKey(rows []Waiting) string {
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%s@%d,", row.ID, row.LastUsed.Unix())
	}
	return b.String()
}

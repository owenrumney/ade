package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/busstate"
	"github.com/owenrumney/ade/internal/chat"
)

// The launcher holds this machine's one agentbus connection, so a person is
// one row and a claim and its verdict share a socket. See the design doc.

// busReport is how often a checkout tells the launcher where it stands. The
// launcher ages a report out, so this has to be the shorter of the two.
const busReport = 10 * time.Second

// busCall sends one request to the launcher's bus API. Out may be nil where
// only the status matters.
func busCall(ctx context.Context, method, path string, body, out any) error {
	base := launcherURL()
	if base == "" {
		return errNoLauncher
	}
	at, err := url.Parse(base)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	ask, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ask, method, at.Scheme+"://"+at.Host+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ade-Token", at.Query().Get("t"))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &busRefused{status: res.StatusCode, body: raw}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// busRefused carries the launcher's status and sentence through, so a checkout
// does not invent a second wording for the same refusal.
type busRefused struct {
	status int
	body   []byte
}

func (e *busRefused) Error() string {
	var said struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(e.body, &said) == nil && said.Error != "" {
		return said.Error
	}
	return string(e.body)
}

var errNoLauncher = &busRefused{
	status: http.StatusConflict,
	body:   []byte(`{"error":"the launcher is not running, so this machine is not on agentbus"}`),
}

// pulseBus tells the launcher where this checkout stands, until ctx ends.
func (s *Server) pulseBus(ctx context.Context) {
	ticker := time.NewTicker(busReport)
	defer ticker.Stop()
	for {
		s.reportBus(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// reportBus is this checkout's account of itself: what the launcher cannot
// see from outside, which is focus and what the agent is doing.
func (s *Server) reportBus(ctx context.Context) {
	run := s.agentState()
	body := map[string]any{
		"dir":         s.startRoot,
		"label":       s.busLabel(),
		"focused":     s.here(),
		"asking":      len(run.Asking),
		"running":     run.Running,
		"queued":      run.Queued,
		"queue_since": s.queueSince(),
	}
	if err := busCall(ctx, http.MethodPost, "/api/bus/checkout", body, nil); err != nil {
		slog.Debug("report to the launcher's bus", "err", err)
	}
	s.sessionOnBus(ctx)
}

// currentBus is the machine's standing, read from the launcher. Nothing is
// cached here: the launcher is the one that knows.
func (s *Server) currentBus() *BusState {
	s.busMu.Lock()
	held := s.busHeld
	s.busMu.Unlock()
	return held
}

// refreshBus reads the launcher's bus state so the page has it without every
// draw making an HTTP call, and says anything that came back for this checkout.
func (s *Server) refreshBus(ctx context.Context) {
	var state busstate.State
	if err := busCall(ctx, http.MethodGet, "/api/bus", nil, &state); err != nil {
		s.busMu.Lock()
		s.busHeld = nil
		s.busMu.Unlock()
		return
	}
	held := &state
	if !state.Online && len(state.Offers) == 0 && len(state.Replies) == 0 &&
		len(state.Sent) == 0 && len(state.Team) == 0 {
		held = nil
	}
	s.busMu.Lock()
	moved := !reflect.DeepEqual(s.busHeld, held)
	s.busHeld = held
	fresh := unsaid(state.Replies, s.startRoot, s.busSaid)
	for _, reply := range fresh {
		s.busSaid[reply.ID] = true
	}
	s.busMu.Unlock()
	// The page reads the roster off the state, and nothing else here pushes
	// one: without this it fills on the next unrelated event, or never.
	if moved {
		s.hub.Broadcast("state", nil)
	}
	for _, reply := range fresh {
		s.say(ChatMessage{Kind: chat.KindNote, Body: replyLine(reply)})
	}
}

// handleBusRefresh re-reads the roster from the bus now, rather than waiting
// for the next thing somebody else changes.
func (s *Server) handleBusRefresh(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/refresh", nil)
}

// unsaid is the answers a run here asked for. What you asked by hand is a
// notification instead: the agent never asked it.
func unsaid(replies []busstate.Reply, dir string, said map[string]bool) []busstate.Reply {
	var out []busstate.Reply
	for _, reply := range replies {
		if !reply.ForAgent || reply.Dir != dir || said[reply.ID] {
			continue
		}
		out = append(out, reply)
	}
	return out
}

// replyLine is how an answer reads in the conversation. The agent asked and
// walked away, so it has to say what was asked as well as what came back.
func replyLine(reply busstate.Reply) string {
	who := reply.From
	if who == "" {
		who = "somebody"
	}
	if reply.ByAgent {
		who += " (their agent, they were away)"
	}
	said := strings.TrimSpace(reply.Option + " " + reply.Text)
	if reply.Kind == "review" {
		return fmt.Sprintf("%s reviewed %s: %s. %s", who, reply.PR, reply.Verdict, reply.Text)
	}
	if reply.Question != "" {
		return fmt.Sprintf("%s answered %q: %s", who, reply.Question, said)
	}
	return fmt.Sprintf("%s answered: %s", who, said)
}

// watchBus keeps this checkout's report and its copy of the roster current.
func (s *Server) watchBus(ctx context.Context) {
	go s.pulseBus(ctx)
	ticker := time.NewTicker(busReport)
	defer ticker.Stop()
	for {
		s.refreshBus(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

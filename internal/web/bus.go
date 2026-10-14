package web

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/busstate"
)

// busPulse is how often presence and status are pushed. A person moving
// between tabs should show up on someone else's `who` within a few seconds.
const busPulse = 10 * time.Second

// busTTL is how long ade's own requests stay open.
const busTTL = 4 * time.Hour

// focusGrace is how long an unfocused editor still counts as you being here.
// Reading a page in a browser is not being away.
const focusGrace = 5 * time.Minute

// The rows a list draws, now shared with the launcher, which owns the
// connection. Aliases so every handler and test keeps its spelling.
type (
	BusOffer  = busstate.Offer
	BusReply  = busstate.Reply
	BusMember = busstate.Member
	BusState  = busstate.State
)

// bus is what this checkout reports and what it last read back. The
// connection itself belongs to the launcher.
type bus struct {
	// focused is what the editor last said about its window, focusAt when it
	// said it, and focusTold whether anything ever did.
	focused   bool
	focusAt   time.Time
	focusTold bool
}

func newBus() *bus { return &bus{} }

// here is whether you are at this checkout. The editor says so when it has
// told us; otherwise an attached page is the only evidence there is.
func (s *Server) here() bool {
	s.mu.RLock()
	focused, at, told := s.bus.focused, s.bus.focusAt, s.bus.focusTold
	s.mu.RUnlock()
	if !told {
		return s.hub.Clients() > 0
	}
	return focused || time.Since(at) < focusGrace
}

// busLabel is how this checkout shows up in `agentbus who`.
func (s *Server) busLabel() string {
	if where := s.project(); where != "" {
		return where + " on " + s.space().branch
	}
	return s.space().branch
}

// forwardBus hands a request to the launcher and returns its answer verbatim,
// status and sentence both: a second wording would be a second answer.
func (s *Server) forwardBus(w http.ResponseWriter, r *http.Request, path string, extra map[string]any) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body == nil {
		body = map[string]any{}
	}
	for k, v := range extra {
		body[k] = v
	}
	var out json.RawMessage
	err := busCall(r.Context(), http.MethodPost, path, body, &out)
	if err != nil {
		refused := &busRefused{}
		if errors.As(err, &refused) {
			writeError(w, refused.status, refused.Error())
			return
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	go s.refreshBus(context.WithoutCancel(r.Context()))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func (s *Server) handleBusAnswer(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/answer", nil)
}

func (s *Server) handleBusClaim(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/claim", nil)
}

func (s *Server) handleBusResult(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/result", nil)
}

// The checkout is named so an answer can be handed back to the session here
// that is waiting on it.
func (s *Server) handleBusRequestReview(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/review", map[string]any{"dir": s.startRoot})
}

func (s *Server) handleBusAsk(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/ask", map[string]any{"dir": s.startRoot})
}

func (s *Server) handleBusDismiss(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/dismiss", nil)
}

func (s *Server) handleBusPresence(w http.ResponseWriter, r *http.Request) {
	s.forwardBus(w, r, "/api/bus/presence", nil)
}

// handleBusHere takes the editor's word for whether its window has focus,
// which is the nearest thing to knowing you are at the keyboard.
func (s *Server) handleBusHere(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Focused bool `json:"focused"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	changed := !s.bus.focusTold || s.bus.focused != req.Focused
	s.bus.focused, s.bus.focusTold = req.Focused, true
	if changed {
		s.bus.focusAt = time.Now()
	}
	s.mu.Unlock()

	// Straight away rather than on the next tick: picking the window back up
	// should not take ten seconds to show.
	s.reportBus(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"here": s.here()})
}

// handleBusWho lists who is on the bus, for choosing a reviewer.
func (s *Server) handleBusWho(w http.ResponseWriter, r *http.Request) {
	var out json.RawMessage
	if err := busCall(r.Context(), http.MethodGet, "/api/bus/who", nil, &out); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"online": false})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// busReviewed hands a verdict posted here to the launcher, which holds the
// claim it answers.
func (s *Server) busReviewed(ctx context.Context, repo string, number int, state, summary, url string) {
	body := map[string]any{
		"repo": repo, "number": number, "state": state, "summary": summary, "url": url,
	}
	if err := busCall(ctx, http.MethodPost, "/api/bus/reviewed", body, nil); err != nil {
		slog.Debug("report the review back", "err", err)
	}
}

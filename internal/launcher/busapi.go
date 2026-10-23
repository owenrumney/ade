package launcher

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/bushistory"
	"github.com/owenrumney/ade/internal/busstate"
	"github.com/owenrumney/ade/internal/dash"
)

const notConnected = "this machine is not connected to agentbus"

// take removes an offer and answers it, or nothing when it has gone.
func (b *busnode) take(id string) *busstate.Offer {
	b.mu.Lock()
	defer b.mu.Unlock()
	offer, ok := b.offers[id]
	if !ok {
		return nil
	}
	delete(b.offers, id)
	return offer
}

func (b *busnode) offer(id string) *busstate.Offer {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.offers[id]
}

// handleBusState is the machine's standing, for a checkout's page to draw.
func (s *Server) handleBusState(w http.ResponseWriter, r *http.Request) {
	state := s.currentBus()
	if state == nil {
		writeJSON(w, http.StatusOK, busstate.State{})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// handleBusCheckout takes one ade's account of itself. Presence is the sum of
// these, so a checkout that stops reporting ages out rather than lying.
func (s *Server) handleBusCheckout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir        string    `json:"dir"`
		Label      string    `json:"label"`
		Focused    bool      `json:"focused"`
		Asking     int       `json:"asking"`
		Running    bool      `json:"running"`
		Queued     int       `json:"queued"`
		QueueSince time.Time `json:"queue_since"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Dir) == "" {
		writeError(w, http.StatusBadRequest, "a report names its checkout")
		return
	}
	s.bus.report(req.Dir, checkoutState{
		Label: req.Label, Focused: req.Focused, Asking: req.Asking, Running: req.Running,
		Queued: req.Queued, QueueSince: req.QueueSince,
	})
	// Straight away rather than on the next tick: picking the window back up
	// should not take ten seconds to show.
	s.sendBusStatus(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"presence": s.busPresence()})
}

// handleBusPresence fixes how you look to the team, or clears it. The window
// cannot know you have stepped out, so this is the one thing it cannot infer.
func (s *Server) handleBusPresence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Presence string `json:"presence"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Presence {
	case "", agentbus.PresenceActive, agentbus.PresenceIdle, agentbus.PresenceDND:
	default:
		writeError(w, http.StatusBadRequest, "presence is active, idle, dnd, or empty to follow your window")
		return
	}
	s.bus.setOverride(req.Presence)
	s.sendBusStatus(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{"presence": s.busPresence()})
}

// handleBusAsk puts a question to somebody, for a session that does not want
// to wait. The id comes straight back and the answer arrives on the socket.
func (s *Server) handleBusAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir      string   `json:"dir"`
		From     string   `json:"from"`
		Question string   `json:"question"`
		Options  []string `json:"options"`
		To       string   `json:"to"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		writeError(w, http.StatusBadRequest, "a question needs some words")
		return
	}
	to, err := agentbus.ParseTarget(req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, ok := s.bus.live()
	if !ok {
		writeError(w, http.StatusConflict, notConnected)
		return
	}
	id, err := client.Ask(r.Context(), to, busTTL,
		agentbus.AskBody{Question: req.Question, Options: req.Options})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.bus.mu.Lock()
	s.bus.asked[id] = askedBy{Dir: req.Dir, Agent: req.From == "agent",
		Kind: "ask", To: to.String(), Question: req.Question, At: time.Now()}
	s.bus.mu.Unlock()
	s.rememberAsked()
	s.said(to.String(), bushistory.Entry{
		ID: id, Kind: bushistory.KindAsk, Mine: true, Agent: req.From == "agent",
		Question: req.Question, Options: req.Options,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "asked", "id": id, "to": to.String()})
}

// handleBusAnswer answers a question somebody's agent put to you.
func (s *Server) handleBusAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Option string `json:"option"`
		Text   string `json:"text"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Option) == "" && strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "an answer needs an option or some words")
		return
	}
	client, ok := s.bus.live()
	if !ok {
		writeError(w, http.StatusConflict, notConnected)
		return
	}
	asked := s.bus.offer(req.ID)
	if asked == nil {
		writeError(w, http.StatusNotFound, "that question is no longer waiting")
		return
	}
	err := client.Answer(r.Context(), req.ID, agentbus.AnswerBody{
		Option: req.Option, Text: req.Text, By: agentbus.AnsweredByHuman,
	})
	// A conflict is somebody else having answered first, which is the point of
	// offering it to everyone: drop it and say so plainly.
	if agentbus.IsConflict(err) {
		s.bus.take(req.ID)
		s.busChanged()
		writeError(w, http.StatusConflict, "somebody answered that first")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.bus.take(req.ID)
	s.said(asked.From, bushistory.Entry{
		ID: req.ID, Kind: bushistory.KindAnswer, Mine: true, Text: req.Text, Option: req.Option,
	})
	s.busChanged()
	writeJSON(w, http.StatusOK, map[string]string{"status": "answered"})
}

// handleBusClaim takes a review request, and checks the pull request out when
// asked to. Claim and verdict share this connection, whatever checkout reports.
func (s *Server) handleBusClaim(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Open bool   `json:"open"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	client, ok := s.bus.live()
	if !ok {
		writeError(w, http.StatusConflict, notConnected)
		return
	}
	offer := s.bus.offer(req.ID)
	if offer == nil || offer.Kind != "review" {
		writeError(w, http.StatusNotFound, "that review request is no longer waiting")
		return
	}
	if !offer.Claimed {
		err := client.ClaimReview(r.Context(), req.ID)
		if agentbus.IsConflict(err) {
			s.bus.take(req.ID)
			s.busChanged()
			writeError(w, http.StatusConflict, "somebody else took that review")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.bus.mu.Lock()
		offer.Claimed = true
		s.bus.claimed[busstate.ReviewKey(offer.Repo, offer.Number)] = req.ID
		s.bus.mu.Unlock()
		s.busChanged()
	}
	if !req.Open {
		writeJSON(w, http.StatusOK, map[string]string{"status": "claimed"})
		return
	}
	// Its own context: checking out and starting an ade outlives the fetch that
	// asked for it, and a browser that gave up should not undo the worktree.
	open, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
	defer cancel()
	url, path, err := s.board.OpenPullRequest(open, offer.Repo, offer.Number)
	var gone *dash.GoneError
	if errors.As(err, &gone) {
		s.bus.take(req.ID)
		s.busChanged()
		writeError(w, http.StatusConflict, gone.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "claimed", "url": url, "path": path})
}

// handleBusResult reports a verdict on a review this machine claimed.
func (s *Server) handleBusResult(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		State   string `json:"state"`
		Summary string `json:"summary"`
		URL     string `json:"url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.State {
	case agentbus.ReviewApproved, agentbus.ReviewChangesRequested, agentbus.ReviewCommented:
	default:
		writeError(w, http.StatusBadRequest, "a verdict is approved, changes_requested or commented")
		return
	}
	client, ok := s.bus.live()
	if !ok {
		writeError(w, http.StatusConflict, notConnected)
		return
	}
	offer := s.bus.offer(req.ID)
	if offer == nil {
		writeError(w, http.StatusNotFound, "that review request is no longer waiting")
		return
	}
	if req.URL == "" {
		req.URL = offer.PR
	}
	if err := client.ReviewResult(r.Context(), req.ID, agentbus.ReviewResultBody{
		State: req.State, Summary: req.Summary, URL: req.URL,
	}); err != nil && !agentbus.IsConflict(err) {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.bus.take(req.ID)
	s.busChanged()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reported"})
}

// handleBusRequestReview asks the team to review a pull request.
func (s *Server) handleBusRequestReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir  string `json:"dir"`
		From string `json:"from"`
		PR   string `json:"pr"`
		To   string `json:"to"`
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	url, repo, err := agentbus.ParsePR(req.PR)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	to, err := agentbus.ParseTarget(req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, ok := s.bus.live()
	if !ok {
		writeError(w, http.StatusConflict, notConnected)
		return
	}
	id, err := client.RequestReview(r.Context(), to, busTTL,
		agentbus.ReviewRequestBody{PR: url, Note: req.Note})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.bus.mu.Lock()
	s.bus.asked[id] = askedBy{Dir: req.Dir, Agent: req.From == "agent",
		Kind: "review", To: to.String(), PR: url, At: time.Now()}
	s.bus.mu.Unlock()
	s.rememberAsked()

	// The ask has gone out, so from here nothing may fail the request.
	said := s.reviewer.Set(r.Context(), req.Dir, repo, url, to)
	out := map[string]any{"status": "asked", "id": id, "to": to.String(), "reviewerSet": said.Set}
	if said.Login != "" {
		out["reviewer"] = said.Login
	}
	if said.Why != "" {
		out["reviewerNote"] = said.Why
		slog.Debug("no reviewer on the pull request", "pr", url, "to", to.String(), "why", said.Why)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBusRefresh reads the roster again. The pushes carry only changes, so
// a page that missed one has no other way back to the truth.
func (s *Server) handleBusRefresh(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.bus.live(); !ok {
		writeError(w, http.StatusConflict, notConnected)
		return
	}
	s.seedTeam(r.Context())
	s.busChanged()
	writeJSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

// handleBusDismiss clears an answer once it has been read.
func (s *Server) handleBusDismiss(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	s.bus.mu.Lock()
	delete(s.bus.replies, req.ID)
	delete(s.bus.asked, req.ID)
	if offer, ok := s.bus.offers[req.ID]; ok {
		delete(s.bus.offers, req.ID)
		if key := busstate.ReviewKey(offer.Repo, offer.Number); s.bus.claimed[key] == req.ID {
			delete(s.bus.claimed, key)
		}
		s.bus.dismissed[req.ID] = true
	}
	s.bus.mu.Unlock()
	s.rememberAsked()
	s.busChanged()
	writeJSON(w, http.StatusOK, map[string]string{"status": "dismissed"})
}

// handleBusWho lists who is on the bus, for choosing a reviewer.
func (s *Server) handleBusWho(w http.ResponseWriter, r *http.Request) {
	client, ok := s.bus.live()
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"online": false})
		return
	}
	rows, err := client.Who(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"online": true, "instances": rows})
}

// busReviewed reports a verdict posted in a checkout back to whoever asked,
// when the pull request is one this machine claimed.
func (s *Server) busReviewed(ctx context.Context, repo string, number int, state, summary, url string) {
	s.bus.mu.Lock()
	id := s.bus.claimed[busstate.ReviewKey(repo, number)]
	s.bus.mu.Unlock()
	if id == "" {
		return
	}
	client, ok := s.bus.live()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := client.ReviewResult(ctx, id, agentbus.ReviewResultBody{
		State: state, Summary: summary, URL: url,
	}); err != nil {
		slog.Warn("could not report the review back", "pr", busstate.ReviewKey(repo, number), "err", err)
		return
	}
	s.bus.take(id)
	s.busChanged()
}

// handleBusReviewed is a checkout telling the launcher a verdict was posted,
// so the claim made here can be answered from wherever the work happened.
func (s *Server) handleBusReviewed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo    string `json:"repo"`
		Number  int    `json:"number"`
		State   string `json:"state"`
		Summary string `json:"summary"`
		URL     string `json:"url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	go s.busReviewed(context.WithoutCancel(r.Context()), req.Repo, req.Number, req.State, req.Summary, req.URL)
	writeJSON(w, http.StatusOK, map[string]string{"status": "reporting"})
}

// busPulseSeen keeps a checkout's report fresh while it is serving, so a quiet
// ade does not read as gone.
func (s *Server) busPulseSeen(dir string) time.Time {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	return s.bus.checkouts[dir].At
}

// handleBusHistory is everything said with one person, oldest first. It is
// this machine's record: the bus itself keeps none.
func (s *Server) handleBusHistory(w http.ResponseWriter, r *http.Request) {
	who := strings.TrimSpace(r.URL.Query().Get("with"))
	said, err := s.history.With(who)
	if errors.Is(err, bushistory.ErrNobody) {
		writeError(w, http.StatusBadRequest, "say who: ?with=an address or a team")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if said == nil {
		said = []bushistory.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"with": who, "said": said})
}

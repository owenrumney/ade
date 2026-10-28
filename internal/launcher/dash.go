package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/spaces"
)

// decodeJSON reads a request body, or says so.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		writeError(w, http.StatusBadRequest, "could not read that request")
		return false
	}
	return true
}

// dashInterval is how often the three lists are read again: they change
// because of other people, not because of anything this machine did.
const dashInterval = 5 * time.Minute

// dashResponse is the whole dashboard in one reply: what the page asks for on
// load, and again whenever the stream says it changed.
type dashResponse struct {
	Reviews       []dash.Review       `json:"reviews"`
	Notifications []dash.Notification `json:"notifications"`
	Linear        *dash.Tickets       `json:"linear,omitempty"`

	// Why Reviews may be short. An empty list that cannot say reads as a bug.
	ReviewTrouble dash.ReviewTrouble `json:"reviewTrouble,omitzero"`

	// Started is the checkout each ticket is already being worked on in, by
	// identifier, so the page offers to go there rather than to start again.
	Started map[string]dash.Started `json:"started,omitempty"`
}

func (s *Server) handleDash(w http.ResponseWriter, _ *http.Request) {
	tickets := s.board.Tickets()
	out := dashResponse{
		Reviews:       s.board.Reviews(),
		Notifications: s.board.Notifications(),
		Linear:        tickets,
		ReviewTrouble: s.board.ReviewTroubleNow(),
	}
	if tickets != nil {
		out.Started = s.board.Started(tickets.Issues)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDashRefresh reads all three again now, for the page's refresh button.
func (s *Server) handleDashRefresh(w http.ResponseWriter, r *http.Request) {
	s.board.RefreshOnly(r.Context(), r.URL.Query().Get("only"))
	s.handleDash(w, r)
}

func (s *Server) handleMuteReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		Muted  bool   `json:"muted"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch err := s.board.Mute(req.Repo, req.Number, req.Muted); {
	case errors.Is(err, dash.ErrNotOffered):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleDismissNotification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Dismissed bool   `json:"dismissed"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch err := s.board.Dismiss(req.ID, req.Dismissed); {
	case errors.Is(err, dash.ErrNotInInbox):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleOpenReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
		// Link is a pasted pull request, which need not be one on the list.
		Link string `json:"link"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Link) != "" {
		ref, ok := ghpr.ParseRef(req.Link)
		if !ok {
			writeError(w, http.StatusBadRequest,
				"that is not a pull request link: paste one like https://github.com/owner/repo/pull/123")
			return
		}
		req.Repo, req.Number = ref.Repo, ref.Number
		url, path, err := s.board.OpenPR(r.Context(), ref)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.openSpaceFor(req.Repo)
		writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path})
		return
	}
	url, path, err := s.board.OpenReview(r.Context(), req.Repo, req.Number)
	switch {
	case errors.Is(err, dash.ErrGone):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		s.openSpaceFor(req.Repo)
		writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path})
	}
}

// openSpaceFor opens the review's repository as a space, so the checkout it
// was put in is listed. An empty list hides nothing, so it is left empty.
func (s *Server) openSpaceFor(repo string) {
	cfg, err := settings.Load()
	if err != nil {
		slog.Debug("open the space for a review", "repo", repo, "err", err)
		return
	}
	open := spaces.Clean(cfg.Spaces)
	if len(open) == 0 {
		return
	}
	// Against the cleaned list, not the raw one: a settings file with a
	// blank or a repeat in it counted as unchanged and the space never opened.
	want := spaces.Open(cfg.Spaces, repo)
	if len(want) == len(open) {
		return
	}
	cfg.Spaces = want
	if err := settings.Save(cfg); err != nil {
		slog.Debug("save the space for a review", "repo", repo, "err", err)
	}
}

// handleRunReview checks the pull request out and sets a cold review going
// on it, with no window opened. The row is what reports on it.
func (s *Server) handleRunReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo   string `json:"repo"`
		Number int    `json:"number"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	path, err := s.board.StartReview(r.Context(), req.Repo, req.Number)
	switch {
	case errors.Is(err, dash.ErrGone):
		writeError(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		s.openSpaceFor(req.Repo)
		writeJSON(w, http.StatusAccepted, map[string]any{"path": path})
	}
}

// handleStartTicket makes a ticket's worktree and document in a repository you
// have checked out, and answers the link to the ade now serving it.
func (s *Server) handleStartTicket(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Identifier string `json:"identifier"`
		Dir        string `json:"dir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	url, path, err := s.board.StartTicket(r.Context(), strings.TrimSpace(req.Dir), req.Identifier)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path})
}

// watchDash keeps the three lists current. The first read is immediate, since
// the page usually opens straight after.
func (s *Server) watchDash(ctx context.Context) {
	s.board.Refresh(ctx)
	ticker := time.NewTicker(dashInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Debug("refreshing the launcher dashboard")
			s.board.Refresh(ctx)
		}
	}
}

// handleLimits is the plan usage, as a checkout's server reports it.
func (s *Server) handleLimits(w http.ResponseWriter, _ *http.Request) {
	// The launcher's context: the reading this may start outlives the response.
	writeJSON(w, http.StatusOK, s.limits.Snapshot(s.ctx))
}

// limitsInterval is how often usage is read again, for an unwatched screen.
const limitsInterval = 2 * time.Minute

// watchLimits keeps usage current, so the page needs no timer of its own.
func (s *Server) watchLimits(ctx context.Context) {
	ticker := time.NewTicker(limitsInterval)
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
			if now := limitsKey(s.limits.Snapshot(ctx)); now != last {
				last = now
				s.hub.Broadcast("limits", nil)
			}
		}
	}
}

// limitsKey is a reading as one string, so an unchanged one is not broadcast.
func limitsKey(state limits.State) string {
	var b strings.Builder
	for _, l := range []*limits.Limits{state.Claude, state.Codex} {
		if l == nil {
			continue
		}
		fmt.Fprintf(&b, "%s:", l.Provider)
		for _, w := range l.Windows {
			fmt.Fprintf(&b, "%s=%.1f@%d,", w.Name, w.Percent, w.ResetsAt.Unix())
		}
	}
	return b.String()
}

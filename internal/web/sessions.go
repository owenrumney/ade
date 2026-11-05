package web

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// handleRepos lists the repositories on this machine, each with its worktrees
// and the conversations that happened in them, so you can get back to another
// piece of work without going through a terminal.
func (s *Server) handleRepos(w http.ResponseWriter, _ *http.Request) {
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// repos is the whole of what the switcher offers, and the whole of what it
// will act on.
func (s *Server) repos() ([]sessions.Repo, error) {
	cfg, err := settings.Load()
	if err != nil {
		return nil, err
	}
	return sessions.MarkRepoHere(sessions.Repos(cfg.Roots(), cfg.Ignored), s.space().homeRoot), nil
}

// handleOpenSession starts or reuses a ade in another checkout and hands back
// its link.
//
// Nothing becomes machine-wide by doing this: every server is still one
// checkout's, recorded in its own git dir. This only saves you a terminal.
func (s *Server) handleOpenSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
		ID  string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}

	// Only somewhere the scan actually found. The request names a directory to
	// start a process in, so it has to be one that was offered.
	all, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, ok := sessions.FindCheckout(all, req.Dir)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of the checkouts on this machine")
		return
	}

	// An empty id opens the checkout without a conversation, which is how you
	// start something new somewhere you have never run an agent.
	id, provider := "", ""
	if req.ID != "" {
		match, ok := sessions.Find(dir.Sessions, dir.Path, req.ID)
		if !ok {
			writeError(w, http.StatusBadRequest, "that is not one of that checkout's sessions")
			return
		}
		if match.Resumable {
			id, provider = match.ID, match.Agent
		}
	}

	url, err := sessions.OpenAgentIn(s.ctx, dir.Path, provider, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url})
}

// handleRecent lists the checkouts you have had open, newest first. The
// switcher is for finding anything; this is for going back to what you were
// just doing.
func (s *Server) handleRecent(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"recent": settings.Recents(settings.Recent)})
}

// rememberWorkspace records this checkout as the most recently opened, so the
// next server can offer it. Failing to write history is not worth an error
// anywhere: it costs a dropdown entry.
func (s *Server) rememberWorkspace() {
	ws := s.space()
	if ws.snapshot || ws.homeRoot == "" {
		return
	}
	if err := settings.Touch(ws.homeRoot, ws.branch, time.Now()); err != nil {
		slog.Debug("record recent workspace", "err", err)
	}
}

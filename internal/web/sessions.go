package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/owenrumney/ade/internal/machine"
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

// handleGraph is the machine right now — every checkout and what is running
// on each — the same picture the launcher draws, from inside a checkout.
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": sessions.Graph(r.Context(), repos, nil)})
}

// handleGraphCancel stops what another ade is running, from the graph. This
// server's own agent has its own Stop button; a request naming this checkout
// goes there too, so the picture has one way to stop a thing.
//
// The boundary here is "same user, same machine". Which directories can be
// acted on is bounded by the allow-list of checkouts the scan found; who can
// act is bounded by this server's own token, since the other ade's token is
// read from its record on disk. A leak of this token therefore reaches every
// ade on the box, not just this one.
func (s *Server) handleGraphCancel(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.graphTarget(w, r)
	if !ok {
		return
	}
	if dir == s.space().homeRoot {
		s.handleCancel(w, r)
		return
	}
	if err := sessions.CancelAgent(r.Context(), dir); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGraphStop shuts another ade down, from the graph. Not this one: the
// page asking would go with it, and `ade stop` or closing the tab is the
// honest way to end the server you are looking at. The same "same user, same
// machine" boundary as handleGraphCancel: a checkout the scan offered, asked
// for with this server's token.
func (s *Server) handleGraphStop(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.graphTarget(w, r)
	if !ok {
		return
	}
	if dir == s.space().homeRoot {
		writeError(w, http.StatusBadRequest, "that is this ade; close the tab or run `ade stop` in the checkout")
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
func (s *Server) graphTarget(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		Dir string `json:"dir"`
	}
	if !decode(w, r, &req) {
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

// handleDeleteSessions takes sessions out of the lists for good. The
// launcher fills up with conversations you will never open again, and
// archiving only moves them behind a "show archived"; this is how they go.
func (s *Server) handleDeleteSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	gone, err := settings.Forget(req.IDs...)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": gone})
}

// handleRecent lists the checkouts you have had open, newest first. The
// switcher is for finding anything; this is for going back to what you were
// just doing.
func (s *Server) handleCombineSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string   `json:"dir"`
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	group, err := sessions.Combine(repos, req.Dir, req.IDs)
	if err != nil {
		writeError(w, groupStatus(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group})
}

// groupStatus tells a bad selection from a store that would not take it: an
// unwritable ~/.ade is not something the user can fix in the picker.
func groupStatus(err error) int {
	if errors.Is(err, settings.ErrStore) {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

func (s *Server) handleUncombineSessions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sessions.Uncombine(repos, req.ID); err != nil {
		writeError(w, groupStatus(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDismissSessionSuggestion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir         string `json:"dir"`
		Fingerprint string `json:"fingerprint"`
	}
	if !decode(w, r, &req) {
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, ok := sessions.FindCheckout(repos, req.Dir)
	if !ok || dir.Suggestion == nil || dir.Suggestion.Fingerprint != req.Fingerprint {
		writeError(w, http.StatusBadRequest, "that is not a current session suggestion")
		return
	}
	if err := settings.SetSessionGroupSuggestionDismissed(dir.Path, req.Fingerprint); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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

// watchMachine leaves the graph in ~/.ade/machine, so a box with no launcher
// still has one. Where one is up, the lease makes this a read and a sleep.
func (s *Server) watchMachine(ctx context.Context) {
	dir, err := machine.Dir()
	if err != nil {
		slog.Debug("machine snapshot: no state directory", "err", err)
		return
	}
	src := machine.Source{
		Dir:    dir,
		Writer: machine.Writer{PID: os.Getpid(), Kind: "server", Root: s.startRoot},
		Repos:  s.repos,
	}
	src.Loop(ctx)
}

package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/browse"
	"github.com/owenrumney/ade/internal/claudelog"
	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// Importable is a conversation claude holds, and how it stands with ade.
type Importable struct {
	claudelog.Conversation
	// Here is one a session in ade is already on.
	Here bool `json:"here"`
	// Checkout is its folder being the top of a git checkout, which a session needs.
	Checkout bool `json:"checkout"`
}

// moveAgent tells the ade serving a checkout which conversation its agent is in.
var moveAgent = func(ctx context.Context, sessionURL, id string) error {
	body, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		return err
	}
	_, err = sessions.AskFor(ctx, sessionURL, http.MethodPost, "/api/agent/conversation", string(body), 10*time.Second)
	return err
}

// handleConversations lists the conversations claude holds on this machine, newest first.
func (s *Server) handleConversations(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	here := make(map[string]bool, len(s.served))
	for id := range s.served {
		here[id] = true
	}
	s.mu.Unlock()
	list := []Importable{}
	for _, one := range claudelog.Recent(r.URL.Query().Get("first")) {
		list = append(list, Importable{Conversation: one, Here: here[one.ID], Checkout: browse.IsCheckout(one.Dir)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": list})
}

// handleImport starts a session on a conversation claude holds, or moves the
// one its checkout already has onto it.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// The folder comes off claude's own transcript, never off the request.
	found, ok := claudelog.Find(strings.TrimSpace(req.ID))
	if !ok {
		writeError(w, http.StatusBadRequest, "claude holds no conversation by that id")
		return
	}
	dir, ok := browsed(found.Dir)
	if !ok {
		writeError(w, http.StatusBadRequest,
			"that conversation was started in "+found.Dir+", which is not the top of a git checkout: a session needs one")
		return
	}
	// With no ade on the checkout, a claude that has it open is one in a terminal.
	if found.Running && !s.live(r.Context(), dir.Path) {
		writeError(w, http.StatusConflict, "that conversation is open in another terminal; quit claude there first")
		return
	}
	url, err := s.openWith(s.ctx, dir.Path, sessions.Start{Agent: "claude", Session: found.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// An ade already serving the checkout keeps its own conversation until told.
	if err := moveAgent(s.ctx, url, found.ID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.awaitServing(found.ID, dir.Path)
	s.hub.Broadcast("repos", nil)
	writeJSON(w, http.StatusOK, map[string]any{"session": found.ID, "dir": dir.Path})
}

// live is whether an ade is serving the checkout now.
func (s *Server) live(ctx context.Context, dir string) bool {
	rec, ok := session.Read(session.Path(sessions.GitDir(dir)))
	if !ok {
		return false
	}
	probe := s.probe
	if probe == nil {
		probe = session.Probe
	}
	_, up := probe(ctx, rec)
	return up
}

// awaitServing waits, briefly, for the checkout's ade to announce itself under id.
func (s *Server) awaitServing(id, dir string) {
	wait := announcedWait
	if httpx.Wide() {
		wait *= 10
	}
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		s.mu.Lock()
		row, ok := s.served[id]
		s.mu.Unlock()
		if ok && row.Dir == dir {
			return
		}
	}
}

package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/sessions"
)

// forwardSession passes a session verb to the machine the conversation is on.
// Those rows are read from that machine, so only it can hide one.
func (s *Server) forwardSession(ctx context.Context, machine, path string, body any) ([]byte, error) {
	live := s.connection(machine)
	if live == nil {
		return nil, fmt.Errorf("not connected to %s", machine)
	}
	sent, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	answer, err := sessions.AskFor(ctx, live.url, http.MethodPost, path, string(sent), openWait)
	if err != nil {
		return nil, fmt.Errorf("%s on %s: %w", strings.TrimPrefix(path, "/api/"), machine, err)
	}
	// The cached sweep still has the row that was just removed.
	s.mu.Lock()
	s.hostCheckouts = nil
	s.mu.Unlock()
	return answer, nil
}

// onMachine replies with what that machine said, and reports whether it
// handled the request at all.
func (s *Server) onMachine(w http.ResponseWriter, r *http.Request, machine, path string, body any) bool {
	machine = strings.TrimSpace(machine)
	if machine == "" {
		return false
	}
	answer, err := s.forwardSession(r.Context(), machine, path, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(answer)
	return true
}

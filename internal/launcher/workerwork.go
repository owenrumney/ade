package launcher

import (
	"context"
	"net/http"
	"time"
)

// workerAction is one thing a connected machine's worker is told to do, and the
// word the row shows while it happens.
type workerAction struct {
	// Verb is the state the row draws: syncing, updating or stopping.
	Verb string
	// Wait bounds the work. An update is a registry pull over someone else's
	// network, so it is minutes rather than seconds.
	Wait time.Duration
	// Do is the work itself, outside the request.
	Do func(context.Context, *conn) error
}

const (
	workSyncing  = "syncing"
	workUpdating = "updating"
	workStopping = "stopping"
)

// workingOn is the action in flight on a machine, or empty.
func (s *Server) workingOn(name string) string {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	return s.hostWork[name]
}

// start runs a worker action in the background and answers 202 at once. The
// row learns the outcome from the hosts list, not from this request.
func (s *Server) start(w http.ResponseWriter, r *http.Request, name string, action workerAction) {
	live := s.connection(name)
	if live == nil {
		writeError(w, http.StatusBadRequest, name+" is not connected")
		return
	}
	if !s.claimWork(name, action.Verb) {
		writeError(w, http.StatusConflict, name+" is already "+s.workingOn(name))
		return
	}
	// Not the request's context. A closed window must not leave a machine with
	// its worker stopped and nothing put back.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), action.Wait)
	go func() {
		defer cancel()
		err := action.Do(ctx, live)
		s.releaseWork(name)
		s.noteWork(name, err)
		s.hub.Broadcast("hosts", nil)
	}()
	s.hub.Broadcast("hosts", nil)
	w.WriteHeader(http.StatusAccepted)
}

// claimWork takes a machine's one work slot. Two actions at once would have
// each one's cleanup undo the other's.
func (s *Server) claimWork(name, verb string) bool {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if s.hostWork[name] != "" {
		return false
	}
	// A new attempt starts from nothing said: the row's message is about the
	// attempt that just failed, not about this one.
	delete(s.workFailed, name)
	s.hostWork[name] = verb
	return true
}

func (s *Server) releaseWork(name string) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	delete(s.hostWork, name)
}

// noteWork keeps what the last action on a machine said, so the row can show
// it once the request that started the work has gone.
func (s *Server) noteWork(name string, err error) {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if err == nil {
		delete(s.workFailed, name)
		return
	}
	s.workFailed[name] = err.Error()
}

func (s *Server) workFailure(name string) string {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	return s.workFailed[name]
}

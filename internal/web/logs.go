package web

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/logs"
)

// logNudge coalesces a burst into one wake-up: a run logs far faster than a
// page can usefully refetch.
const logNudge = 500 * time.Millisecond

// logsResponse is the records the viewer has not seen, and where to ask from.
type logsResponse struct {
	Records []logs.Record `json:"records"`
	Latest  uint64        `json:"latest"`
}

// handleLogs serves this process's log from `since`, so following it costs the
// new lines rather than the whole ring.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	ring := logs.Installed
	if ring == nil {
		writeJSON(w, http.StatusOK, logsResponse{Records: []logs.Record{}})
		return
	}
	since, _ := strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	found := ring.Since(since)
	if found == nil {
		found = []logs.Record{}
	}
	writeJSON(w, http.StatusOK, logsResponse{Records: found, Latest: ring.Latest()})
}

// logWaker turns records into one broadcast per window. The timer fires off
// the logging goroutine, so a broadcast that logged cannot recurse.
type logWaker struct {
	mu    sync.Mutex
	timer *time.Timer
	send  func()
}

func (l *logWaker) note() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.timer != nil {
		return
	}
	l.timer = time.AfterFunc(logNudge, func() {
		l.mu.Lock()
		l.timer = nil
		l.mu.Unlock()
		l.send()
	})
}

// watchLogs wakes the viewer when lines land.
func (s *Server) watchLogs() {
	if logs.Installed == nil {
		return
	}
	waker := &logWaker{send: func() { s.hub.Broadcast("logs", nil) }}
	logs.Installed.OnChange(func(rec logs.Record) {
		if !wakeFor(rec) {
			return
		}
		waker.note()
	})
}

// A viewer fetching writes an access line, so waking on one never settles.
func wakeFor(rec logs.Record) bool {
	return rec.Msg != "request"
}

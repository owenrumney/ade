package launcher

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/owenrumney/ade/internal/concierge"
	"github.com/owenrumney/ade/internal/httpx"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// deskFile remembers the day the desk last opened, which is all the state it
// keeps: the todos are the desk's memory, and the transcript is not.
const deskFile = "concierge.json"

// brief is what a new day's desk is asked for first. It is one turn over data
// the launcher already holds, and it is why the desk never opens empty.
const brief = "Brief me: what is waiting on me, what is due, and what is running. " +
	"Answer as three bullet points, one each for waiting, due and running, nothing else."

// handleConcierge is the front desk, started if it is not already up. The
// checkout is the key, not a session id; each day gets its own conversation.
func (s *Server) handleConcierge(w http.ResponseWriter, r *http.Request) {
	dir, err := concierge.Ensure(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fresh := deskIsNewToday(time.Now())
	session := ""
	if fresh {
		session = sessions.None
	}
	url, err := s.openOn(s.ctx, dir, concierge.Agent, concierge.Model, session)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fresh {
		markDeskOpened(time.Now())
		sendFirstMessage(s.ctx, url, brief, nil)
	}
	if httpx.Wide() {
		via, ok := s.proxyURL(r, s.announcedFor(dir))
		if !ok {
			writeError(w, http.StatusBadGateway, "the desk started but never announced itself")
			return
		}
		url = via
	}
	s.notifyPorts()
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": dir, "fresh": fresh})
}

// deskIsNewToday reports whether the desk has yet opened today.
func deskIsNewToday(now time.Time) bool {
	last, err := deskLastOpened()
	if err != nil {
		return true
	}
	return !sameDay(last, now)
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func deskPath() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, deskFile), nil
}

func deskLastOpened() (time.Time, error) {
	path, err := deskPath()
	if err != nil {
		return time.Time{}, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return time.Time{}, err
	}
	var held struct {
		OpenedAt time.Time `json:"openedAt"`
	}
	if err := json.Unmarshal(data, &held); err != nil {
		return time.Time{}, err
	}
	if held.OpenedAt.IsZero() {
		return time.Time{}, errors.New("the desk has never opened")
	}
	return held.OpenedAt, nil
}

func markDeskOpened(at time.Time) {
	path, err := deskPath()
	if err != nil {
		return
	}
	data, err := json.Marshal(map[string]any{"openedAt": at})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, append(data, '\n'), 0o600)
}

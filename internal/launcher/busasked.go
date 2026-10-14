package launcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/owenrumney/ade/internal/busstate"
)

// askedFile keeps what this machine put to the team. The bus can fetch one
// request by id but cannot list ours, so a restart loses them unless we write.
const askedFile = "bus-asked.json"

// askedRow is one remembered request. askedBy on disk, with its id.
type askedRow struct {
	ID       string    `json:"id"`
	Dir      string    `json:"dir,omitempty"`
	Agent    bool      `json:"agent,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	To       string    `json:"to,omitempty"`
	Question string    `json:"question,omitempty"`
	PR       string    `json:"pr,omitempty"`
	At       time.Time `json:"at"`
}

// rememberAsked writes the open requests down. Called with the lock free.
func (s *Server) rememberAsked() {
	s.bus.mu.Lock()
	rows := make([]askedRow, 0, len(s.bus.asked))
	for id, one := range s.bus.asked {
		rows = append(rows, askedRow{
			ID: id, Dir: one.Dir, Agent: one.Agent, Kind: one.Kind,
			To: one.To, Question: one.Question, PR: one.PR, At: one.At,
		})
	}
	s.bus.mu.Unlock()

	data, err := json.Marshal(rows)
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.home, 0o750); err != nil {
		slog.Debug("keep what was asked", "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join(s.home, askedFile), data, 0o600); err != nil {
		slog.Debug("keep what was asked", "err", err)
	}
}

// recallAsked reads them back, without the ones the bus has already dropped.
func (s *Server) recallAsked() {
	data, err := os.ReadFile(filepath.Join(s.home, askedFile)) // #nosec G304 -- ade's own state directory
	if err != nil {
		return
	}
	var rows []askedRow
	if json.Unmarshal(data, &rows) != nil {
		return
	}
	s.bus.mu.Lock()
	for _, row := range rows {
		if row.ID == "" || (!row.At.IsZero() && time.Since(row.At) > busTTL) {
			continue
		}
		s.bus.asked[row.ID] = askedBy{
			Dir: row.Dir, Agent: row.Agent, Kind: row.Kind,
			To: row.To, Question: row.Question, PR: row.PR, At: row.At,
		}
	}
	s.bus.mu.Unlock()
}

// reconcileAsked drops the ones answered while this machine was away. The
// list is of what is still out, so one already dealt with does not belong.
func (s *Server) reconcileAsked(ctx context.Context) {
	client, ok := s.bus.live()
	if !ok {
		return
	}
	s.bus.mu.Lock()
	ids := make([]string, 0, len(s.bus.asked))
	for id := range s.bus.asked {
		ids = append(ids, id)
	}
	s.bus.mu.Unlock()
	if len(ids) == 0 {
		return
	}

	var settled int
	for _, id := range ids {
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		frame, err := client.Get(call, id)
		cancel()
		// Unreadable is not answered: leave it alone rather than forget it.
		if err != nil || busstate.ReplyOf(frame) == nil {
			continue
		}
		s.bus.mu.Lock()
		delete(s.bus.asked, id)
		s.bus.mu.Unlock()
		settled++
	}
	if settled == 0 {
		return
	}
	slog.Info("agentbus requests already answered", "count", settled)
	s.rememberAsked()
	s.busChanged()
}

package web

import (
	"log/slog"
	"net/http"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/ledger"
)

// maxSeen bounds one report. A viewport holds tens of ranges, so anything past
// this is not a window drawing a diff.
const maxSeen = 500

// maxLines bounds the lines one report may cover, counted across its ranges: a
// viewport holds tens, so this is already far past a window drawing a diff.
const maxLines = 20_000

func (s *Server) handleLedger(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ledgerReport())
}

func (s *Server) handleLedgerSeen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seen []ledger.Seen `json:"seen"`
		Tick *ledger.Span  `json:"tick,omitempty"`
		// On is whether Tick is being claimed or taken back.
		On bool `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Seen) > maxSeen {
		writeError(w, http.StatusBadRequest, "too many ranges in one report")
		return
	}
	lines := 0
	for _, one := range req.Seen {
		if one.Lines > maxLines {
			lines = maxLines + 1
			break
		}
		if one.Lines > 0 {
			lines += one.Lines
		}
	}
	if lines > maxLines {
		writeError(w, http.StatusBadRequest, "too many lines in one report")
		return
	}

	store := s.ledgerStore()
	if store == nil {
		writeJSON(w, http.StatusOK, ledger.Report{Files: []ledger.FileCount{}})
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	if err := store.Note(diff, req.Seen); err != nil {
		slog.Warn("could not record what was read", "err", err)
	}
	if req.Tick != nil {
		if err := store.Tick(diff, *req.Tick, req.On); err != nil {
			slog.Warn("could not record a review tick", "err", err)
		}
		s.hub.Broadcast("ledger", nil)
	}
	writeJSON(w, http.StatusOK, s.ledgerReport())
}

func (s *Server) ledgerReport() ledger.Report {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	return s.ledgerStore().Count(diff, s.lineageStore().Marks(diff))
}

// ledgerUnread counts only the files given, which for a commit is the index:
// the trailer has to be about the lines the commit carries and no others.
func (s *Server) ledgerUnread(files []string) (unread, total int) {
	in := map[string]bool{}
	for _, f := range files {
		in[f] = true
	}
	report := s.ledgerReport()
	for _, file := range report.Files {
		if !in[file.Path] {
			continue
		}
		unread += file.Unread
		total += file.Agent
	}
	return unread, total
}

// dropLedger throws the ledger away, which a rebase does: the read marks were
// placed against a diff that no longer exists.
func (s *Server) dropLedger() {
	if err := s.ledgerStore().Drop(); err != nil {
		slog.Warn("could not drop the read ledger", "err", err)
	}
}

// pruneLedger drops marks the diff can no longer place, which is how a line the
// agent rewrote loses its read mark.
func (s *Server) pruneLedger(diff *gitdiff.Diff) {
	if err := s.ledgerStore().Prune(diff); err != nil {
		slog.Debug("prune ledger", "err", err)
	}
}

// ledgerStore is the current workspace's ledger, opened once and kept, the way
// lineageStore keeps the lineage it counts against.
func (s *Server) ledgerStore() *ledger.Store {
	w := s.space()
	if w == nil || w.gitDir == "" || w.snapshot {
		return nil
	}
	path := ledger.Path(w.gitDir, w.branch)

	s.mu.RLock()
	store := s.ledgers[path]
	s.mu.RUnlock()
	if store != nil {
		return store
	}

	store, err := ledger.Open(w.gitDir, w.branch)
	if err != nil {
		slog.Warn("could not open the read ledger", "err", err)
		return nil
	}
	s.mu.Lock()
	if have := s.ledgers[path]; have != nil {
		store = have
	} else {
		if s.ledgers == nil {
			s.ledgers = map[string]*ledger.Store{}
		}
		s.ledgers[path] = store
	}
	s.mu.Unlock()
	return store
}

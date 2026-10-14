package web

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/weld"
)

// weldedTurn is the prompts the running turn was made of, to run separately.
type weldedTurn struct {
	first  queued
	folded []queued
}

// weldable is the prompts behind next that would go with it as one turn: the
// gate first, then the model. The queue is read and left alone.
func (s *Server) weldable(next queued, since time.Time) []queued {
	if cfg, err := settings.Load(); err != nil || cfg.MergeQueuedOff {
		return nil
	}
	s.mu.RLock()
	var behind []queued
	if i := slices.IndexFunc(s.queue, func(q queued) bool { return q.id == next.id }); i >= 0 {
		behind = append([]queued(nil), s.queue[i+1:]...)
	}
	s.mu.RUnlock()

	entries := make([]weld.Entry, len(behind))
	for i, q := range behind {
		entries[i] = entry(q)
	}
	asked := weld.Candidates(entry(next), entries, since)
	if len(asked) == 0 {
		return nil
	}
	texts := make([]string, len(asked))
	for i, at := range asked {
		texts[i] = behind[at].message
	}
	chosen := weld.Fit(next.message, texts, weld.Linked(s.ctx, s.weldRunner, next.message, texts))
	out := make([]queued, 0, len(chosen))
	for _, i := range chosen {
		out = append(out, behind[asked[i]])
	}
	return out
}

func entry(q queued) weld.Entry {
	file := ""
	if q.context != nil {
		file = q.context.From
	}
	return weld.Entry{Chat: q.kind == kindChat, At: q.at, Message: q.message, File: file}
}

// weld takes the folded prompts off the queue, wherever they sit, and joins
// them to first. One the queue lost while the model read is left out.
func (s *Server) weld(first queued, folded []queued) queued {
	s.mu.Lock()
	var taken []queued
	for _, q := range folded {
		if i := slices.IndexFunc(s.queue, func(held queued) bool { return held.id == q.id }); i >= 0 {
			s.queue = slices.Delete(s.queue, i, i+1)
			taken = append(taken, q)
		}
	}
	if len(taken) == 0 {
		s.mu.Unlock()
		return first
	}
	s.saveQueue()
	s.welded = &weldedTurn{first: first, folded: taken}
	s.mu.Unlock()

	for _, q := range taken {
		s.markFolded(q.chatID, first.chatID)
	}
	return joined(first, taken)
}

// joined is one turn from several, in the order they were typed.
func joined(first queued, folded []queued) queued {
	out := first
	texts := []string{first.message}
	files := append([]chat.File(nil), first.files...)
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Path] = true
	}
	var attached *chat.Context
	if first.context != nil {
		c := *first.context
		attached = &c
	}
	for _, q := range folded {
		texts = append(texts, q.message)
		for _, f := range q.files {
			if !seen[f.Path] && len(files) < chat.MaxFiles {
				seen[f.Path] = true
				files = append(files, f)
			}
		}
		if q.context == nil {
			continue
		}
		if attached == nil {
			c := *q.context
			attached = &c
		} else if q.context.From == attached.From {
			attached.Text += "\n\n" + q.context.Text
		}
	}
	out.message = strings.Join(texts, "\n\n")
	out.files = files
	out.context = attached
	return out
}

// markFolded writes which prompt a message was sent with, or clears it.
func (s *Server) markFolded(id, into string) {
	if id == "" {
		return
	}
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.ID == id {
			msg.FoldedInto = into
			s.amend(msg)
			return
		}
	}
}

func (s *Server) forgetWeld() {
	s.mu.Lock()
	s.welded = nil
	s.mu.Unlock()
}

func (s *Server) unweld() {
	s.mu.Lock()
	welded := s.welded
	s.welded = nil
	s.mu.Unlock()
	if welded == nil {
		return
	}
	for _, q := range welded.folded {
		s.markFolded(q.chatID, "")
	}
}

// weldedID is the first prompt of the running welded turn, for the page to
// offer taking it apart under.
func (s *Server) weldedID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.welded == nil {
		return ""
	}
	return s.welded.first.chatID
}

func (s *Server) weldRunner(ctx context.Context, system, prompt string) (string, error) {
	turn, err := s.coldClaude(ctx, bridge.OneShotOpts{
		Bin:                  s.agent.bin,
		Dir:                  s.space().homeRoot,
		Model:                weld.Model,
		SystemPrompt:         system,
		Prompt:               prompt,
		Tools:                []string{},
		NoMCP:                true,
		NoSessionPersistence: true,
	})
	if err != nil {
		return "", err
	}
	return turn.Text, nil
}

// handleChatSeparate undoes a weld while its turn is going: the run stops,
// the prompts go back on the queue in order, and the first runs alone.
func (s *Server) handleChatSeparate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.weldedID() != id {
		writeError(w, http.StatusNotFound, "those prompts are no longer running as one turn")
		return
	}
	// Restacked under the runner's lock, before the stop: the drain the stop
	// causes finds them, and a turn already over is not put back to run twice.
	var welded *weldedTurn
	stopped := s.agent.stopWhile("the prompts are being run separately", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		welded = s.welded
		if welded == nil || welded.first.chatID != id {
			return false
		}
		s.welded = nil
		s.queue = append(append([]queued{welded.first}, welded.folded...), s.queue...)
		s.saveQueue()
		return true
	})
	if !stopped {
		s.forgetWeld()
		writeError(w, http.StatusConflict, "that turn has already finished")
		return
	}

	for _, q := range welded.folded {
		s.markFolded(q.chatID, "")
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

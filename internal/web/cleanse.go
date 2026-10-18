package web

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/cleanse"
	"github.com/owenrumney/ade/internal/settings"
)

// cleanse rewrites a reply short off the run's goroutine; every failure is no change.
func (s *Server) cleanse(msg ChatMessage) {
	if !cleansed(msg) || !cleanse.Wanted(msg.Body) {
		return
	}
	if cfg, err := settings.Load(); err != nil || cfg.CleanseOff {
		return
	}
	session := s.agent.Current()
	s.cleansing.Add(1)
	go func() {
		defer s.cleansing.Done()
		started := time.Now()
		short, err := cleanse.Rewrite(context.Background(), s.cleanseRunner, msg.Body)
		if err != nil {
			// Warn: a drop that logs nothing looks like a feature switched off.
			slog.Warn("cleanse dropped", append([]any{"id", msg.ID}, dropped(err, time.Since(started))...)...)
			return
		}
		s.amendShort(session, msg.ID, short)
	}()
}

// dropped is what a drop logs: the gate, the elapsed time and what it lost.
func dropped(err error, took time.Duration) []any {
	drop := &cleanse.Drop{Why: cleanse.WhyFailed, Err: err}
	errors.As(err, &drop)
	out := []any{"why", drop.Why, "ms", took.Milliseconds()}
	if drop.Lost != "" {
		out = append(out, "lost", drop.Lost)
	}
	if drop.Err != nil {
		out = append(out, "err", drop.Err)
	}
	return out
}

// cleansed is prose you read: never a review's, never a run's own output such as a commit message.
func cleansed(msg ChatMessage) bool {
	return msg.Kind == chat.KindAgent && !msg.Review && (msg.Run == kindChat || msg.Run == runSubagent)
}

// amendShort reads the message back first: pictures may have landed on it since.
func (s *Server) amendShort(session, id, short string) {
	s.attaching.Lock()
	defer s.attaching.Unlock()
	for _, msg := range s.chatStore().List(session) {
		if msg.ID != id {
			continue
		}
		msg.Short, msg.ShortHTML = short, s.replyHTML(short)
		if err := s.chatStore().Update(session, msg); err != nil {
			slog.Debug("could not update the conversation", "err", err)
			return
		}
		s.hub.Broadcast("chat", chatEvent{Message: &msg})
		return
	}
}

// cleanseRunner is sharpenRunner on cleanse's model: one run, no tools.
func (s *Server) cleanseRunner(ctx context.Context, system, prompt string) (string, error) {
	return s.cheapRunner(ctx, cleanse.Model, system, prompt)
}

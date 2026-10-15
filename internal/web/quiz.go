package web

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/quiz"
	"github.com/owenrumney/ade/internal/review"
)

// Quiz is questions about the diff the Diff tab is showing, answers and all:
// withholding them would cost a round trip each and stop nobody.
type Quiz struct {
	Questions []quiz.Question `json:"questions"`

	// Files, Base and Scope are what the panel header names, so a quiz on a
	// third of the change is visible rather than silent.
	Files int    `json:"files"`
	Base  string `json:"base"`
	Scope string `json:"scope"`

	// Stale is set on the way out, by comparing hash with the diff now.
	Stale bool `json:"stale"`

	// hash is the diff version this was written for. The panel is told
	// Stale instead.
	hash string
}

// handleQuiz asks for questions about the diff. One already written for it is
// handed back; Regenerate asks again anyway, which is the retake worth having.
func (s *Server) handleQuiz(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Regenerate bool `json:"regenerate"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(diff.Files) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to quiz: the diff is empty")
		return
	}
	if !req.Regenerate {
		if held := s.currentQuiz(); held != nil && !held.Stale {
			writeJSON(w, http.StatusOK, map[string]any{"quiz": held})
			return
		}
	}

	runCtx, ok := s.agent.begin(s.ctx, kindQuiz)
	if !ok {
		// You quiz while the agent is still working, the same as you stage
		// and describe while it works.
		waiting := s.stack(queued{kind: kindQuiz})
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "queued": waiting})
		return
	}

	s.setQuiz(nil)
	s.spawn(func() { s.runQuiz(runCtx, diff) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "files": len(diff.Files)})
}

func (s *Server) runQuiz(ctx context.Context, diff *gitdiff.Diff) {
	defer s.recoverRun(kindQuiz)

	err := s.quizOnce(ctx, diff)
	s.agent.finish(err)
	if err != nil {
		slog.Warn("quiz failed", "err", err)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) quizOnce(ctx context.Context, diff *gitdiff.Diff) error {
	s.agent.step(fmt.Sprintf("reading %d file(s)", len(diff.Files)))

	paths := map[string]bool{}
	for _, file := range diff.Files {
		paths[file.Path] = true
	}
	rendered := review.RenderDiff(diff)

	ask := func() ([]quiz.Question, error) {
		turn, err := s.oneShot(ctx, bridge.OneShotOpts{
			Bin: s.agent.bin,
			// Writing a question about what a change breaks is not the size
			// of job the commit message is, but it starts here on purpose.
			Model:  outgoing.FastModel,
			Dir:    s.space().diffRoot(),
			Prompt: quiz.Prompt(rendered),
			// The diff is handed over rendered; the reading is for the
			// context a hunk does not carry.
			AllowedTools: quiz.Tools(),
			NoMCP:        true,
			OnEvent:      s.onRunEvent(kindQuiz),
		})
		if err != nil {
			return nil, err
		}
		return quiz.Parse(turn.Text, func(path string) bool { return paths[path] }), nil
	}

	questions, err := ask()
	if err != nil {
		return err
	}
	// Dropping can leave too few. Ask once more and keep the larger set.
	if len(questions) < quiz.MinQuestions {
		s.agent.step("too few questions came back; asking once more")
		again, againErr := ask()
		if againErr != nil {
			return againErr
		}
		if len(again) > len(questions) {
			questions = again
		}
	}
	if len(questions) < quiz.MinQuestions {
		return fmt.Errorf("only %d question(s) came back readable; a quiz of fewer than %d is not a confidence check",
			len(questions), quiz.MinQuestions)
	}

	shuffle := rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404 -- a shuffle, not a secret
	s.setQuiz(&Quiz{
		Questions: quiz.Shuffle(questions, shuffle),
		Files:     len(diff.Files),
		Base:      diff.Base,
		Scope:     s.space().opts.Scope,
		hash:      s.currentHash(),
	})
	s.agent.step(fmt.Sprintf("%d question(s) ready", len(questions)))
	return nil
}

func (s *Server) setQuiz(q *Quiz) {
	s.mu.Lock()
	s.quiz = q
	s.mu.Unlock()
}

// currentQuiz is the held quiz, marked stale when the diff has moved under
// it. In memory only: a quiz is worth nothing tomorrow.
func (s *Server) currentQuiz() *Quiz {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.quiz == nil {
		return nil
	}
	held := *s.quiz
	held.Stale = held.hash != s.diffHash
	return &held
}

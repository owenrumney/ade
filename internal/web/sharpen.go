package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/sharpen"
)

// Sharpened is what the composer draws: no questions is the usual answer.
type Sharpened struct {
	Questions []sharpen.Question `json:"questions"`
}

// handleSharpen asks a cheap model which reading of a prompt was meant. Every
// failure is no questions: it must never be in the way of sending.
func (s *Server) handleSharpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	if !decode(w, r, &req) {
		return
	}
	out := Sharpened{Questions: []sharpen.Question{}}
	if cfg, err := settings.Load(); err != nil || cfg.SharpenOff || !sharpen.Wanted(req.Prompt) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// One at a time: the prompt typed since makes the last look stale.
	s.sharpening.Lock()
	if s.sharpenStop != nil {
		s.sharpenStop()
	}
	s.sharpenStop = cancel
	s.sharpening.Unlock()

	questions, err := sharpen.Ask(ctx, s.sharpenRunner, req.Prompt, s.openQuestions())
	if err != nil {
		slog.Debug("sharpen dropped", "err", err)
	}
	if len(questions) > 0 {
		out.Questions = questions
	}
	writeJSON(w, http.StatusOK, out)
}

// sharpenRunner is one claude run with no tools, whatever the session runs on.
func (s *Server) sharpenRunner(ctx context.Context, system, prompt string) (string, error) {
	turn, err := s.coldClaude(ctx, bridge.OneShotOpts{
		Bin:                  s.agent.bin,
		Dir:                  s.space().homeRoot,
		Model:                sharpen.Model,
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

// openQuestions is the open design document's own list of what is undecided.
func (s *Server) openQuestions() string {
	file := s.currentDoc()
	if file == "" {
		return ""
	}
	text, err := docs.Read(s.docsDir, file)
	if err != nil {
		return ""
	}
	return sharpen.OpenQuestions(text)
}

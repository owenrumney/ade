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

func (s *Server) sharpenRunner(ctx context.Context, system, prompt string) (string, error) {
	return s.cheapRunner(ctx, sharpen.Model, system, prompt)
}

// cheapRunner is one run of a cheap model with no tools, on the host even
// under a sandbox: it reads no checkout, and a VM boot outlasts the call.
func (s *Server) cheapRunner(ctx context.Context, model, system, prompt string) (string, error) {
	turn, err := runOneShot(ctx, bridge.OneShotOpts{
		Bin:                  s.agent.bin,
		Dir:                  s.space().homeRoot,
		Model:                model,
		SystemPrompt:         system,
		Prompt:               prompt,
		Tools:                []string{},
		NoMCP:                true,
		NoSessionPersistence: true,
	})
	hideOneShot(turn)
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

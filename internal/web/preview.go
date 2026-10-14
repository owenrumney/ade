package web

import (
	"context"
	"encoding/base64"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/preview"
	"github.com/owenrumney/ade/internal/settings"
)

var capturePreview = preview.Capture

var previewReachable = preview.Reachable

type previewView struct {
	// URL is this repository's own override; Effective is what will actually
	// be captured, which is the global default when the override is empty.
	URL       string `json:"url"`
	Effective string `json:"effective"`
	Off       bool   `json:"off"`
	Path      string `json:"path"`
}

func (s *Server) handlePreview(w http.ResponseWriter, _ *http.Request) {
	config, err := preview.Load(s.previewPath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.previewViewOf(config))
}

func (s *Server) previewViewOf(config preview.Config) previewView {
	cfg, err := settings.Load()
	if err != nil {
		slog.Debug("read settings for preview", "err", err)
	}
	return previewView{
		URL: config.URL, Effective: cfg.Preview.PreviewURL(config.URL),
		Off: cfg.Preview.Off, Path: s.previewPath(),
	}
}

func (s *Server) handleSavePreview(w http.ResponseWriter, r *http.Request) {
	var config preview.Config
	if !decode(w, r, &config) {
		return
	}
	config, err := preview.Save(s.previewPath(), config)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.previewViewOf(config))
}

func (s *Server) previewPath() string {
	return preview.Path(s.space().gitDir)
}

func (s *Server) capturePreview() {
	session := s.agent.Current()
	// Left pending under a sandbox: the shot is deferred to the next turn that
	// can take one, not thrown away.
	if s.agent.State().Sandbox {
		return
	}
	s.mu.Lock()
	pending := s.previewPending
	s.previewPending = false
	s.mu.Unlock()
	if !pending {
		return
	}
	config, err := preview.Load(s.previewPath())
	if err != nil {
		return
	}
	cfg, err := settings.Load()
	if err != nil {
		slog.Debug("read settings for preview", "err", err)
	}
	target := cfg.Preview.PreviewURL(config.URL)
	if target == "" || !previewReachable(target) {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	data, err := capturePreview(ctx, target)
	if err != nil {
		slog.Debug("capture preview", "err", err)
		return
	}
	images, err := pictures([]chat.Image{{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(data)}})
	if err != nil || len(images) == 0 {
		slog.Debug("keep preview", "err", err)
		return
	}
	if s.agent.Current() != session {
		return
	}
	s.attachImages(session, images[:1])
}

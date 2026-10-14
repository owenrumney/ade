package web

import (
	"context"
	"log/slog"
	"time"

	"github.com/owenrumney/ade/internal/homeassistant"
	"github.com/owenrumney/ade/internal/settings"
)

// deskFlashBudget is room for homeassistant.Hold plus the two calls around
// it; too tight and the light is left red rather than restored.
const deskFlashBudget = 10 * time.Second

var flashDesk = func(ctx context.Context, config settings.HomeAssistant, hue float64) error {
	return (homeassistant.Client{URL: config.URL, Token: config.Token}).Flash(ctx, config.DeskEntity, hue)
}

// deskHue is what the light says about the turn that just ended: green for
// done, red for a turn that wants you.
func (s *Server) deskHue() float64 {
	if f := s.agent.State().Finished; f != nil && f.Asking {
		return homeassistant.HueAsking
	}
	return homeassistant.HueDone
}

func (s *Server) flashDesk() {
	config, err := settings.Load()
	if err != nil {
		slog.Debug("read Home Assistant settings", "err", err)
		return
	}
	if config.HomeAssistant.URL == "" || config.HomeAssistant.DeskEntity == "" || config.HomeAssistant.Token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, deskFlashBudget)
	defer cancel()
	if err := flashDesk(ctx, config.HomeAssistant, s.deskHue()); err != nil {
		slog.Debug("flash desk", "err", err)
	}
}

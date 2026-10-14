package web

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/homeassistant"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/settingsapi"
)

// The context flashDesk hands over has to outlast the hold and the two calls
// around it, or Flash returns with the light still red.
func TestTheDeskFlashBudgetOutlastsTheHold(t *testing.T) {
	assert.Greater(t, deskFlashBudget, homeassistant.Hold()*2)
}

func TestFlashDeskUsesTheSavedIntegration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{HomeAssistant: settings.HomeAssistant{
		URL: "https://assistant.example", DeskEntity: "light.desk", Token: "not-a-real-token",
	}}))
	s := newTestServer(t)
	previous := flashDesk
	var got settings.HomeAssistant
	flashDesk = func(_ context.Context, config settings.HomeAssistant, _ float64) error {
		got = config
		return nil
	}
	t.Cleanup(func() { flashDesk = previous })

	s.flashDesk()
	assert.Equal(t, "https://assistant.example", got.URL)
	assert.Equal(t, "light.desk", got.DeskEntity)
	assert.Equal(t, "not-a-real-token", got.Token)
}

func TestSettingsViewNeverReturnsTheHomeAssistantToken(t *testing.T) {
	view := settingsapi.ViewOf(settings.Settings{HomeAssistant: settings.HomeAssistant{
		URL: "https://assistant.example", DeskEntity: "light.desk", Token: "not-a-real-token",
	}}, "settings.json")
	assert.True(t, view.HomeAssistantTokenSet)
	assert.Equal(t, "oken", view.HomeAssistantTokenEnd)
}

// The lamp is a lamp, so the colour is the whole message: a turn that wants
// you is red, one that is simply done is green.
func TestTheDeskSaysWhetherTheTurnWantsYou(t *testing.T) {
	s := newTestServer(t)
	assert.Equal(t, homeassistant.HueDone, s.deskHue(), "nothing has finished, so nothing is asking")

	s.agent.finished(&Finished{Files: 3})
	assert.Equal(t, homeassistant.HueDone, s.deskHue())

	s.agent.finished(&Finished{Files: 3, Asking: true})
	assert.Equal(t, homeassistant.HueAsking, s.deskHue())
}

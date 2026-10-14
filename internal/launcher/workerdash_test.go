package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestAWorkerReportsToTheKioskUnderItsOwnName(t *testing.T) {
	cfg := settings.Settings{
		Dashboard: settings.Dashboard{URL: "http://pi.local:8080", Token: "secret", Host: "laptop", Claude: "owen"},
		Hosts:     []settings.Host{{Name: "owen@mac-mini", Label: "the mini"}, {Name: "owen@linux"}},
	}

	assert.Equal(t, "the mini", workerDashboard(cfg, "owen@mac-mini").Host)
	assert.Equal(t, "linux", workerDashboard(cfg, "owen@linux").Host, "the login is not part of the name")
	assert.Equal(t, "nothing", workerDashboard(cfg, "nothing").Host, "an unbookmarked host is still itself")

	sent := workerDashboard(cfg, "owen@linux")
	assert.Equal(t, "http://pi.local:8080", sent.URL)
	assert.Equal(t, "owen", sent.Claude)
}

func TestAnUnconfiguredKioskSendsTheWorkerNothing(t *testing.T) {
	cfg := settings.Settings{Dashboard: settings.Dashboard{URL: "http://pi.local:8080"}}

	require.False(t, cfg.Dashboard.On())
	assert.Equal(t, settings.Dashboard{}, workerDashboard(cfg, "linux"))
}

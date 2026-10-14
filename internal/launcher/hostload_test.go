package launcher

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/remote"
)

func loadOf(name string, cores int, load float64) loadRow {
	one := remote.Load{Name: name, Cores: cores, Load1: load, MemTotal: 1 << 30, MemUsed: 1 << 29, Agents: 2}
	return loadRow{Load: one, CPU: one.CPU()}
}

func TestAReadingIsNotAnSSHPerRequest(t *testing.T) {
	sweeps := 0
	sweep := func() []loadRow {
		sweeps++
		return []loadRow{loadOf("pc", 8, 4)}
	}
	c := &loadCache{}
	for range 4 {
		require.Len(t, c.read(sweep), 1)
	}
	assert.Equal(t, 1, sweeps, "one reading stands for loadFresh")

	c.at = time.Now().Add(-loadFresh - time.Second)
	c.read(sweep)
	assert.Equal(t, 2, sweeps, "a cold reading is taken again")
}

func TestASweepThatFoundNothingIsNotCached(t *testing.T) {
	// A cancelled request or a machine that timed out must not hide the panel
	// for loadFresh afterwards.
	c := &loadCache{}
	assert.Empty(t, c.read(func() []loadRow { return nil }))
	assert.True(t, c.at.IsZero(), "nothing to show is a failure, not a reading")
	assert.Len(t, c.read(func() []loadRow { return []loadRow{loadOf("pc", 8, 4)} }), 1)
}

func TestOnlyWhatTheSweepFoundIsHandedBack(t *testing.T) {
	// The sweep drops a machine that did not answer, and nothing downstream
	// puts it back at zero, which would draw as an idle machine.
	c := &loadCache{}
	rows := c.read(func() []loadRow { return []loadRow{loadOf("pi", 4, 1)} })
	require.Len(t, rows, 1)
	assert.Equal(t, "pi", rows[0].Name)
}

func TestTheLoadPanelGetsTheKeysItTypesAgainst(t *testing.T) {
	s := newTestLauncher(t)
	s.load = &loadCache{at: time.Now(), rows: []loadRow{loadOf("pi", 4, 2), loadOf("beast", 16, 8)}}

	rec := do(t, s, "GET", "/api/hosts/load", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Hosts []map[string]any `json:"hosts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Hosts, 2)
	// Stats.tsx types against these names; a renamed tag breaks the panel silently.
	for _, key := range []string{"name", "cores", "load1", "cpu", "memTotal", "memUsed", "agents"} {
		assert.Containsf(t, got.Hosts[0], key, "the page reads %s off every row", key)
	}
	assert.InDelta(t, 50.0, got.Hosts[0]["cpu"], 0.01)
}

func TestASweepWithNoMachinesIsAnEmptyList(t *testing.T) {
	s := newTestLauncher(t)
	s.load = &loadCache{}
	assert.Empty(t, s.sweepLoad(), "an empty list, not a null the page has to guard")
}

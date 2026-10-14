package launcher

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/remote"
	"github.com/owenrumney/ade/internal/settings"
)

func TestAddingAMachineTouchesOnlyTheHostList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	// Something else in the file first: a whole-object save is what this
	// endpoint exists to avoid, so the rest has to still be there after.
	require.Equal(t, http.StatusOK,
		do(t, s, "PUT", "/api/settings", `{"scanDirs":["~/code"],"ignoredDirs":["node_modules"]}`).Code)

	rec := do(t, s, "POST", "/api/hosts", `{"name":"pi5.local","label":"the pi"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Hosts, 1)
	assert.Equal(t, "pi5.local", cfg.Hosts[0].Name)
	assert.Equal(t, "the pi", cfg.Hosts[0].Label)
	assert.Equal(t, []string{"~/code"}, cfg.ScanDirs, "adding a machine kept the rest of the file")
	assert.Equal(t, []string{"node_modules"}, cfg.IgnoredDirs)
}

func TestAMachineIsAddedOnceAndSaysSoTheSecondTime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"pi5.local"}`).Code)
	rec := do(t, s, "POST", "/api/hosts", `{"name":"PI5.local"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, "the name is matched however it is cased")

	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Len(t, cfg.Hosts, 1)
}

// The name goes to ssh, so it is checked by the same rule saving the settings
// applies rather than a second one that disagrees.
func TestAMachineNameSshWouldMisreadIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	for _, bad := range []string{"", "  ", "-oProxyCommand=x", "pi 5", "../etc"} {
		rec := do(t, s, "POST", "/api/hosts", `{"name":"`+bad+`"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, bad)
	}
	cfg, err := settings.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.Hosts)
}

// This Mac is in the list every time it is drawn and never stored. Saving it
// would give it two rows, and the stored one would not connect.
func TestThisMachineCannotBeBookmarked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, "POST", "/api/hosts", `{"name":"local"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "this machine")
}

func TestForgettingAMachineLeavesTheOthers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"pi5.local"}`).Code)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"big.local"}`).Code)

	rec := do(t, s, "DELETE", "/api/hosts/pi5.local", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Hosts, 1)
	assert.Equal(t, "big.local", cfg.Hosts[0].Name)
}

func TestForgettingAMachineThatIsNotThereSaysSo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	assert.Equal(t, http.StatusNotFound, do(t, s, "DELETE", "/api/hosts/nothing.local", "").Code)
}

// storedName is only half of it: the handler has to hand that spelling to
// disconnect, or the master stays up with nothing naming it.
func TestForgettingAMachineDisconnectsUnderTheStoredSpelling(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"Pi5.local"}`).Code)

	stopped := false
	host := remote.NewLocal()
	s.mu.Lock()
	s.hosts["Pi5.local"] = &conn{host: host, fwd: remote.NewForwarder(host), stop: func() { stopped = true }}
	s.mu.Unlock()

	rec := do(t, s, "DELETE", "/api/hosts/pi5.local", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	assert.True(t, stopped, "the connection was dropped, not orphaned")
	assert.NotContains(t, s.hosts, "Pi5.local")
}

// s.hosts is keyed by the name the connection was made under, so forgetting
// by a differently-cased name has to disconnect under the stored one — or an
// ssh master is left with nothing naming it.
func TestTheStoredSpellingIsWhatGetsDisconnected(t *testing.T) {
	hosts := []settings.Host{{Name: "Pi5.local"}, {Name: " big.local "}}

	assert.Equal(t, "Pi5.local", storedName(hosts, "pi5.local"))
	assert.Equal(t, "big.local", storedName(hosts, "BIG.LOCAL"), "and trimmed, as the list is read")
	assert.Equal(t, "", storedName(hosts, "nothing.local"))
	assert.Equal(t, "", storedName(nil, "pi5.local"))
}

func TestWithoutDropsOnlyTheOneNamed(t *testing.T) {
	hosts := []settings.Host{{Name: "Pi5.local"}, {Name: "big.local"}}

	kept := without(hosts, "pi5.local")
	require.Len(t, kept, 1)
	assert.Equal(t, "big.local", kept[0].Name)

	assert.Len(t, without(hosts, "nothing.local"), 2)
	assert.Empty(t, without(nil, "x"))
}

func TestRelabellingAMachineKeepsItsNameAndTheOthers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"pi5.local","label":"the pi"}`).Code)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"bigboy"}`).Code)

	rec := do(t, s, "PATCH", "/api/hosts/PI5.local", `{"label":" attic "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Hosts, 2)
	assert.Equal(t, settings.Host{Name: "pi5.local", Label: "attic"}, cfg.Hosts[0])
	assert.Equal(t, settings.Host{Name: "bigboy"}, cfg.Hosts[1])
}

// Blank is the rename box's way of saying "use the host name", so an empty
// label clears the stored one rather than keeping it.
func TestAnEmptyLabelClearsTheOneStored(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"pi5.local","label":"the pi"}`).Code)

	rec := do(t, s, "PATCH", "/api/hosts/pi5.local", `{"label":"   "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Hosts, 1)
	assert.Equal(t, settings.Host{Name: "pi5.local"}, cfg.Hosts[0])
}

func TestRelabellingAMachineThatIsNotThereSaysSo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, "PATCH", "/api/hosts/nowhere", `{"label":"x"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Blank is a label cleared, not a label missing: the row falls back to the
// host name, which is what the empty box in the UI means.
func TestRelabellingAMachineBackToBlankUsesItsName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/hosts", `{"name":"pi5.local","label":"the pi"}`).Code)

	rec := do(t, s, "PATCH", "/api/hosts/pi5.local", `{"label":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cfg, err := settings.Load()
	require.NoError(t, err)
	require.Len(t, cfg.Hosts, 1)
	assert.Equal(t, settings.Host{Name: "pi5.local", Label: ""}, cfg.Hosts[0])
}

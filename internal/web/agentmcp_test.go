package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/sandbox"
)

// The tools reach other people, so the entry has to carry the token. Without
// one the endpoint would refuse every call the agent made.
func TestTheBusEntryPointsAtAdeAndCarriesTheToken(t *testing.T) {
	raw := busServer("http://127.0.0.1:8080", "tok123")
	require.NotNil(t, raw)

	var entry struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	require.NoError(t, json.Unmarshal(raw, &entry))
	assert.Equal(t, "http", entry.Type)
	assert.Equal(t, "http://127.0.0.1:8080"+BusMCPPath, entry.URL)
	assert.Equal(t, "tok123", entry.Headers["X-Ade-Delegate-Token"])
}

func TestNoAdeToReachMeansNoBusEntry(t *testing.T) {
	assert.Nil(t, busServer("", "tok123"), "nowhere to call")
	assert.Nil(t, busServer("http://127.0.0.1:8080", ""), "nothing to call with")
}

// Nothing to write means no --mcp-config at all: an empty one would hand the
// agent a file that declares no tools.
func TestNoServersMeansNoConfigFile(t *testing.T) {
	path, err := writeAgentMCP(filepath.Join(t.TempDir(), "x.json"), nil)
	require.NoError(t, err)
	assert.Empty(t, path)
}

func TestTheConfigHoldsEveryServerItWasGiven(t *testing.T) {
	at := filepath.Join(t.TempDir(), "nested", "run.json")
	path, err := writeAgentMCP(at, map[string]json.RawMessage{
		"ade_wiki": json.RawMessage(`{"type":"http"}`),
		"agentbus": json.RawMessage(`{"type":"stdio"}`),
	})
	require.NoError(t, err)
	require.Equal(t, at, path)

	var out struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Len(t, out.Servers, 2)
	assert.Contains(t, out.Servers, "agentbus")
	assert.Contains(t, out.Servers, "ade_wiki")
}

// A box reaches this server somewhere, and loopback is not it. Under docker
// that somewhere has a name; under Apple's runtime it is only an address, and
// returning nothing left every ade URL in a box pointing at the box itself.
func TestWhatABoxCallsThisServer(t *testing.T) {
	for _, test := range []struct {
		name string
		rt   sandbox.Runtime
		box  bool
		want string
	}{
		{name: "docker", rt: sandbox.Docker{}, box: true, want: "host.docker.internal"},
		{name: "apple", rt: sandbox.Apple{Host: "192.168.64.1"}, box: true, want: "192.168.64.1"},
		{name: "apple with no gateway", rt: sandbox.Apple{}, box: true},
		{name: "on a worker", rt: sandbox.Docker{Network: "container:ade-hub"}, box: true},
		{name: "no sandbox", rt: sandbox.Apple{Host: "192.168.64.1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := &agentRunner{runtime: test.rt, sandbox: test.box}
			assert.Equal(t, test.want, a.containerHost())
		})
	}
}

// Only a runtime whose boxes cannot reach loopback asks for another address.
func TestOnlyAppleAsksForASecondAddress(t *testing.T) {
	for _, test := range []struct {
		name string
		rt   sandbox.Runtime
		want string
	}{
		{name: "apple", rt: sandbox.Apple{Host: "192.168.64.1"}, want: "192.168.64.1"},
		{name: "apple with no gateway", rt: sandbox.Apple{}},
		{name: "docker", rt: sandbox.Docker{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			asked := ""
			a := &agentRunner{alsoOn: func(host string) { asked = host }}
			a.reachable(test.rt)
			assert.Equal(t, test.want, asked)
		})
	}
}

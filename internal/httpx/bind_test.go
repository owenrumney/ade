package httpx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheBindIsLoopbackUnlessTheEnvironmentSaysOtherwise(t *testing.T) {
	t.Setenv(envBind, "")
	assert.Equal(t, "127.0.0.1", BindHost())
	assert.False(t, Wide())

	t.Setenv(envBind, "0.0.0.0")
	assert.Equal(t, "0.0.0.0", BindHost())
	assert.True(t, Wide())
}

// A container's wildcard address means nothing to the laptop reading the url,
// which reaches the port through `-p 127.0.0.1:N:N`.
func TestTheReportedURLIsLoopbackEvenWithAWideBind(t *testing.T) {
	t.Setenv(envBind, "0.0.0.0")
	ln, url, err := listenOn(0)
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	assert.Contains(t, url, "http://127.0.0.1:")
	assert.NotContains(t, url, "0.0.0.0")
}

// A wide bind is not a loopback listener: a sibling container reaches that
// interface without going through the host's published port at all.
func TestAWideBindDoesNotRelaxTheGuards(t *testing.T) {
	for _, bind := range []string{"", "0.0.0.0"} {
		t.Setenv(envBind, bind)
		assert.True(t, HostAllowed("localhost"))
		assert.False(t, HostAllowed("evil.example.com"), "a rebound name must not reach the API")
		assert.False(t, LoopbackOrigin("http://evil.example.com"), "any origin could open the terminal websocket")
	}
}

func TestALoopbackOriginIsRecognisedHoweverItIsWritten(t *testing.T) {
	for _, origin := range []string{
		"", "http://127.0.0.1:41999", "http://localhost:41999",
		"http://LOCALHOST:41999", "http://[::1]:41999", "http://127.0.0.2:8080",
	} {
		assert.True(t, LoopbackOrigin(origin), origin)
	}
	for _, origin := range []string{
		"http://bigboy.local:41999", "https://example.com", "http://10.0.0.4:80", "::",
	} {
		assert.False(t, LoopbackOrigin(origin), origin)
	}
}

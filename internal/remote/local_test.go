package remote

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The local runner is handed the argv ssh would have sent, so it has to find
// the command inside the login-shell wrapper rather than run the wrapper.
func TestTheLocalRunnerRunsTheCommandInsideTheSshArgv(t *testing.T) {
	host := NewLocal()
	out, err := host.Run(context.Background(), "echo", "hello local")
	require.NoError(t, err)
	assert.Equal(t, "hello local\n", string(out))
}

func TestTheLocalRunnerPassesStdinWithoutPuttingItInArgv(t *testing.T) {
	host := NewLocal()
	out, err := host.RunInput(context.Background(), []byte("secret\n"), "cat")
	require.NoError(t, err)
	assert.Equal(t, "secret\n", string(out))
}

// ssh's own control-socket calls have nothing to do locally, and must not be
// run as a shell command.
func TestControlSocketArgvDoesNothingLocally(t *testing.T) {
	assert.Equal(t, "", localCommand([]string{"-S", "/x.sock", "-O", "check", "pc"}))
	assert.Equal(t, "", localCommand([]string{"-M", "-S", "/x.sock", "-N", "-f", "pc"}))
}

func TestALocalHostIsAlwaysUpAndCostsNothingToClose(t *testing.T) {
	host := NewLocal()
	assert.True(t, host.Local())
	assert.True(t, host.Alive(context.Background()))
	require.NoError(t, host.Dial(context.Background()))
	require.NoError(t, host.Close())
}

// A port on this machine is already where the browser looks, so forwarding it
// to itself would only take the port away from what is serving on it.
func TestForwardingIsANoOpLocally(t *testing.T) {
	host := NewLocal()
	fwd := NewForwarder(host)
	require.NoError(t, fwd.Sync(context.Background(), []Mapping{Same(3000)}))
	assert.Empty(t, fwd.Live())
}

func TestUnwrapLoginKeepsAQuotedArgumentWhole(t *testing.T) {
	argv := []string{"-S", "/x.sock", "pc", "--", login([]string{"echo", "a b; c"})}
	assert.Equal(t, `'echo' 'a b; c'`, localCommand(argv))
	assert.NotContains(t, localCommand(argv), sentinel)
}

func TestTheLocalRunnerReportsWhatFailed(t *testing.T) {
	host := NewLocal()
	_, err := host.Run(context.Background(), "sh", "-c", "echo nope >&2; exit 3")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "nope"), err.Error())
}

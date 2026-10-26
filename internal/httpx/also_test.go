package httpx

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlsoOnOpensTheSamePortAtOneMoreAddress(t *testing.T) {
	t.Cleanup(func() { Allow("") })
	first, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	_, port, err := net.SplitHostPort(first.Addr().String())
	require.NoError(t, err)

	// 127.0.0.2 stands in for the container gateway: another address of
	// this machine, not the wildcard.
	also, err := AlsoOn("127.0.0.2", port)
	if err != nil {
		t.Skip("this machine does not route 127.0.0.2")
	}
	t.Cleanup(func() { _ = also.Close() })
	assert.Equal(t, "127.0.0.2:"+port, also.Addr().String())
}

func TestAlsoOnOpensNothingForLoopbackOrNoAddress(t *testing.T) {
	for _, host := range []string{"", "127.0.0.1"} {
		ln, err := AlsoOn(host, "8080")
		require.NoError(t, err, host)
		assert.Nil(t, ln, "loopback is already served, so there is nothing to open")
	}
}

func TestAlsoOnRefusesSomethingThatIsNotAPort(t *testing.T) {
	ln, err := AlsoOn("127.0.0.2", "")
	require.Error(t, err)
	assert.Nil(t, ln)
}

// The Host header naming the address a box reached the server at is this
// machine, and every other name still has to be refused.
func TestAnAllowedAddressIsAcceptedAsAHostAndNothingElseIs(t *testing.T) {
	t.Cleanup(func() { Allow("") })
	assert.False(t, HostAllowed("192.168.64.1"))
	Allow("192.168.64.1")
	assert.True(t, HostAllowed("192.168.64.1"))
	assert.True(t, HostAllowed("127.0.0.1"), "loopback never stops being allowed")
	for _, name := range []string{"example.com", "192.168.64.2", "0.0.0.0"} {
		assert.False(t, HostAllowed(name), name)
	}
}

package agentbus

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func noTailnet(t *testing.T, until int32) *atomic.Int32 {
	t.Helper()
	was := resolveURL
	t.Cleanup(func() { resolveURL = was })
	wasWait := backoffFirst
	backoffFirst = time.Millisecond
	t.Cleanup(func() { backoffFirst = wasWait })

	var tries atomic.Int32
	resolveURL = func(context.Context) (string, error) {
		if tries.Add(1) >= until {
			return "ws://bus/v1/ws", nil
		}
		return "", errors.New("find the tailnet name: tailscale CLI not found")
	}
	return &tries
}

// The Mac app starts ade at login, where Tailscale is often not up for
// another few seconds. Giving up there left the launcher off the bus until
// somebody restarted it by hand.
func TestTheBusAddressIsWaitedForRatherThanGivenUpOn(t *testing.T) {
	tries := noTailnet(t, 4)
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()

	assert.Equal(t, "ws://bus/v1/ws", Resolve(ctx, nil))
	assert.EqualValues(t, 4, tries.Load())
}

func TestWaitingForTheBusEndsWithTheContext(t *testing.T) {
	noTailnet(t, 1<<30)
	ctx, stop := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer stop()

	assert.Equal(t, "", Resolve(ctx, nil))
}

func TestATailnetAddressIsTheOnlyBareNameTaken(t *testing.T) {
	for ip, want := range map[string]bool{
		"100.77.154.54":          true,
		"100.64.0.1":             true,
		"fd7a:115c:a1e0::2e37:3": true,
		"127.0.0.1":              false,
		"10.0.0.7":               false,
		"100.63.255.255":         false,
		"100.128.0.1":            false,
		"93.184.216.34":          false,
		"::1":                    false,
	} {
		assert.Equal(t, want, tailnetIP(netip.MustParseAddr(ip)), ip)
	}
}

// The Mac app's CLI prints this on stdout and exits 0 when it decides it
// was not run from a shell. Parsed as JSON it reads "invalid character
// 'T'", which tells the reader nothing about what happened.
func TestWhatTheCLISaidIsWhatIsReported(t *testing.T) {
	assert.Equal(t,
		"The Tailscale GUI failed to start: The operation couldn’t be completed.",
		said([]byte("The Tailscale GUI failed to start: The operation couldn’t be completed.\nmore\n")))
	assert.Equal(t, "nothing", said(nil))
	assert.Equal(t, "nothing", said([]byte("  \n")))

	long := said([]byte(strings.Repeat("x", 300)))
	assert.True(t, strings.HasSuffix(long, "…"))
	assert.Len(t, strings.TrimSuffix(long, "…"), 120)
}

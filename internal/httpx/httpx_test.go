package httpx

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page too slow to take every frame is told to resync rather than left
// showing a conversation with a message missing from it.
func TestASlowClientIsToldToResync(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	require.Eventually(t, func() bool { return hub.Clients() == 1 }, time.Second, 10*time.Millisecond)

	// Far more than the buffer holds, sent before the stream is read.
	for i := 0; i < 200; i++ {
		hub.Broadcast("chat", map[string]int{"n": i})
	}

	events := map[string]int{}
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			events[name]++
			if name == "resync" {
				break
			}
		}
	}
	assert.Equal(t, 1, events["resync"], "the drop is reported once the stream drains")
	assert.Less(t, events["chat"], 200, "not every frame fits, which is the point")
	assert.Greater(t, events["chat"], 0)
}

// The same checkout comes up on the same port, so the browser's per-origin
// decisions hold across restarts; a taken port means any free one.
func TestListenOffersTheSamePortForTheSameKey(t *testing.T) {
	key := "/tmp/some/checkout-" + t.Name()
	first, base, err := Listen(key)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("http://127.0.0.1:%d", StablePort(key)), base)
	require.NoError(t, first.Close())

	again, base2, err := Listen(key)
	require.NoError(t, err)
	defer again.Close()
	assert.Equal(t, base, base2, "released and taken back")

	other, base3, err := Listen(key)
	require.NoError(t, err)
	defer other.Close()
	assert.NotEqual(t, base, base3, "held, so any free port")

	assert.GreaterOrEqual(t, StablePort("x"), 20000)
	assert.Less(t, StablePort("x"), 40000)
	assert.NotEqual(t, StablePort("/a"), StablePort("/b"))
}

// Firefox grants notification permission per origin, and the port is part of
// one. The launcher moving to a free port would cost the user that grant every
// time, so it refuses the port it cannot have instead of taking another.
func TestTheFixedPortIsRefusedRatherThanSwappedFor(t *testing.T) {
	key := "fixed-" + t.Name()
	first, base, err := ListenFixed(key)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("http://127.0.0.1:%d", StablePort(key)), base)

	_, _, err = ListenFixed(key)
	require.Error(t, err, "a second one does not quietly land elsewhere")
	assert.Contains(t, err.Error(), fmt.Sprint(StablePort(key)), "the message names the port to free")

	// Freed, it is available again: the failure is about the port being held,
	// not about the key being spent.
	require.NoError(t, first.Close())
	again, base2, err := ListenFixed(key)
	require.NoError(t, err)
	defer again.Close()
	assert.Equal(t, base, base2)
}

func TestBroadcastCountsWhoGotIt(t *testing.T) {
	hub := NewHub()
	defer hub.Close()
	assert.Equal(t, 0, hub.Broadcast("state", nil), "nobody listening")
	c := hub.subscribe()
	defer hub.unsubscribe(c)
	assert.Equal(t, 1, hub.Broadcast("state", nil))
}

// A server that stops without a word leaves the tab retrying a port nothing
// is listening on, unable to tell a restart from a shutdown. The farewell has
// to reach the page even though the hub closes immediately behind it.
func TestByeReachesThePageBeforeTheStreamEnds(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	require.Eventually(t, func() bool { return hub.Clients() == 1 }, time.Second, 10*time.Millisecond)
	hub.Bye("nothing was watching it for 30m0s")

	var saw, reason string
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if name, ok := strings.CutPrefix(line, "event: "); ok {
			saw = name
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok && saw == "bye" {
			reason = data
			break
		}
	}
	assert.Equal(t, "bye", saw, "the last thing the page hears is why")
	assert.JSONEq(t, `{"reason":"nothing was watching it for 30m0s"}`, reason)

	// And the stream is over: closing twice is what a second shutdown path
	// would do, and must not panic on an already-closed channel.
	assert.NotPanics(t, func() { hub.Close() })
}

// TestStablePortIsFixedForever pins the mapping, because ade over ssh computes
// the port a *remote* checkout will bind from this machine and forwards it
// identity-mapped. Change the hash or the range and every remote session
// breaks as a connection refused on a port nobody is listening on, with
// nothing to point at. Break this test on purpose or not at all.
//
// The keys keep their pre-rename spelling: the pair is the guarantee.
func TestStablePortIsFixedForever(t *testing.T) {
	golden := map[string]int{
		"launcher":             29233,
		"/home/owen/code/sow":  37545,
		"/Users/owen/code/sow": 20794,
		"/home/owen/code/foo":  24418,
		"":                     36261,
	}
	for key, want := range golden {
		assert.Equal(t, want, StablePort(key), "StablePort(%q)", key)
	}

	for key := range golden {
		port := StablePort(key)
		assert.GreaterOrEqual(t, port, 20000)
		assert.Less(t, port, 40000)
	}
}

// A page cannot see an SSE comment, so a silent stream has to say something it
// can: without this a suspended connection reads as a live one for ever.
func TestASilentStreamPingsWhereThePageCanSeeIt(t *testing.T) {
	was := KeepAlive
	KeepAlive = 20 * time.Millisecond
	defer func() { KeepAlive = was }()

	hub := NewHub()
	defer hub.Close()
	srv := httptest.NewServer(http.HandlerFunc(hub.HandleSSE))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "event: ping" {
			return
		}
	}
	t.Fatal("nothing named came down a stream with nothing to say")
}

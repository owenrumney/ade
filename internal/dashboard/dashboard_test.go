package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seen is one request the fake dashboard took.
type seen struct {
	path  string
	auth  string
	body  map[string]any
	plain string
}

// took records every request, so a test can assert on the order as well as
// the shapes.
type took struct {
	mu   sync.Mutex
	hits []seen
}

func (t *took) add(one seen) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.hits = append(t.hits, one)
}

func (t *took) all() []seen {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]seen(nil), t.hits...)
}

// fake is a dashboard that records and answers. answer is per request.
func fake(t *testing.T, answer func() int) (string, *took) {
	t.Helper()
	log := &took{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		// EscapedPath, not Path: what went over the wire is the point of
		// escaping a label with a space in it.
		one := seen{path: r.URL.EscapedPath(), auth: r.Header.Get("Authorization"), plain: string(raw)}
		_ = json.Unmarshal(raw, &one.body)
		log.add(one)
		w.WriteHeader(answer())
	}))
	t.Cleanup(server.Close)
	return server.URL, log
}

func ok() int { return http.StatusNoContent }

func TestATileIsPostedToItsSourceWithTheToken(t *testing.T) {
	url, log := fake(t, ok)
	at := time.Date(2026, 10, 4, 15, 46, 12, 0, time.UTC)

	to := New(t.Context(), url, "secret", "mbp")
	require.NotNil(t, to)
	to.Send(Tile{
		Source: "claude personal", Type: TypeSubscription, Title: "Claude · personal",
		Group: "subscriptions", Order: 10, Reduce: Latest, Host: "mbp", At: at,
		Data: map[string]any{"provider": "claude"},
	})
	require.NoError(t, to.Drain(t.Context()))

	hits := log.all()
	require.Len(t, hits, 2, "the machine is declared, then the tile")

	// The host declaration comes first and carries no body.
	assert.Equal(t, "/ingest/claude%20personal/host/mbp", hits[0].path)
	assert.Empty(t, hits[0].plain)
	assert.Equal(t, "Bearer secret", hits[0].auth)

	assert.Equal(t, "/ingest/claude%20personal", hits[1].path)
	assert.Equal(t, "Bearer secret", hits[1].auth)
	assert.Equal(t, map[string]any{
		"type": "subscription", "title": "Claude · personal",
		"group": "subscriptions", "order": float64(10), "reduce": "latest",
		"host": "mbp", "observed_at": "2026-10-04T15:46:12Z",
		"data": map[string]any{"provider": "claude"},
	}, hits[1].body)
	assert.NotContains(t, hits[1].plain, "Source", "the source is the path, not the body")
}

func TestTheMachineIsDeclaredOncePerSource(t *testing.T) {
	url, log := fake(t, ok)
	to := New(t.Context(), url, "secret", "mbp")

	for range 3 {
		to.Send(Tile{Source: "a", Host: "mbp"}, Tile{Source: "b", Host: "mbp"})
	}
	require.NoError(t, to.Drain(t.Context()))

	declares := 0
	for _, hit := range log.all() {
		if hit.plain == "" {
			declares++
		}
	}
	assert.Equal(t, 2, declares, "one per source, not one per tick")
}

func TestAnUnsetConfigStartsNothingAndAsksNothing(t *testing.T) {
	url, log := fake(t, ok)
	for _, to := range []*Reporter{
		New(t.Context(), "", "secret", "mbp"),
		New(t.Context(), url, "", "mbp"),
	} {
		assert.Nil(t, to)
		// Every method takes a nil receiver, so no caller needs a check.
		to.Send(Tile{Source: "a"})
		assert.NoError(t, to.Drain(t.Context()))
		assert.Empty(t, to.Host())
	}
	assert.Empty(t, log.all())
}

func TestAServerThatFailsDoesNotFailTheCaller(t *testing.T) {
	for _, answer := range []int{
		http.StatusInternalServerError,
		http.StatusUnauthorized,
		http.StatusConflict,
		http.StatusAlreadyReported,
	} {
		url, log := fake(t, func() int { return answer })
		to := New(t.Context(), url, "secret", "mbp")
		to.Send(Tile{Source: "a", Host: "mbp"})
		require.NoError(t, to.Drain(t.Context()))
		assert.NotEmpty(t, log.all())
	}
}

// A pi that is off is a connection that hangs. Nothing may wait on it.
func TestASlowServerDoesNotHoldTheCallerUp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	to := New(t.Context(), server.URL, "secret", "mbp")
	started := time.Now()
	to.Send(Tile{Source: "a", Host: "mbp"})
	assert.Less(t, time.Since(started), 50*time.Millisecond, "Send waits for nothing")

	require.NoError(t, to.Drain(t.Context()))
	assert.Less(t, time.Since(started), 3*time.Second, "each request is capped")
}

func TestAFullQueueDropsTheOldestRatherThanBlocking(t *testing.T) {
	// Held until the test lets go, so the drain goroutine is busy and the
	// channel fills behind it.
	hold := make(chan struct{})
	var taken atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		taken.Add(1)
		<-hold
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	to := New(t.Context(), server.URL, "secret", "mbp")
	started := time.Now()
	for i := range queued * 4 {
		to.Send(Tile{Source: "a", Host: "mbp", Order: i})
	}
	assert.Less(t, time.Since(started), time.Second, "a full queue never blocks the sender")

	close(hold)
	require.NoError(t, to.Drain(t.Context()))
	assert.Less(t, int(taken.Load()), queued*4, "the oldest were dropped, not sent")
}

func TestADroppedBatchIsNotWaitedOnForever(t *testing.T) {
	url, _ := fake(t, ok)
	to := New(t.Context(), url, "secret", "mbp")

	// Many more than the queue holds, then a drain: a dropped batch whose
	// done was never closed would hang here.
	for i := range queued * 4 {
		to.Send(Tile{Source: "a", Host: "mbp", Order: i})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, to.Drain(ctx))
}

func TestATileWithNoSourceGoesNowhere(t *testing.T) {
	url, log := fake(t, ok)
	to := New(t.Context(), url, "secret", "mbp")
	to.Send(Tile{Type: TypeCounter, Host: "mbp"})
	require.NoError(t, to.Drain(t.Context()))
	assert.Empty(t, log.all(), "a tile with no source has nowhere to be written")
}

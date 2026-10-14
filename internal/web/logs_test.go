package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/logs"
)

// pinRing installs a ring for one test and puts the old one back.
func pinRing(t *testing.T) *logs.Ring {
	t.Helper()
	was := logs.Installed
	ring := logs.New(nil)
	logs.Installed = ring
	t.Cleanup(func() { logs.Installed = was })
	return ring
}

func TestTheLogIsReadableFromTheBrowser(t *testing.T) {
	s := newTestServer(t)
	ring := pinRing(t)
	slog.New(ring).Debug("list Linear issues", "err", "context deadline exceeded")

	rec := do(t, s, "GET", "/api/logs", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body logsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Records, 1)
	assert.Equal(t, "list Linear issues", body.Records[0].Msg)
	assert.Equal(t, "DEBUG", body.Records[0].Level, "the level the terminal drops")
	assert.Equal(t, "context deadline exceeded", body.Records[0].Attrs[0].Value)
	assert.Equal(t, body.Records[0].Seq, body.Latest)
}

func TestFollowingTheLogCostsOnlyTheNewLines(t *testing.T) {
	s := newTestServer(t)
	ring := pinRing(t)
	log := slog.New(ring)
	log.Info("first")

	var first logsResponse
	require.NoError(t, json.Unmarshal(do(t, s, "GET", "/api/logs", "").Body.Bytes(), &first))
	log.Info("second")

	var next logsResponse
	path := "/api/logs?since=" + strconv.FormatUint(first.Latest, 10)
	require.NoError(t, json.Unmarshal(do(t, s, "GET", path, "").Body.Bytes(), &next))
	require.Len(t, next.Records, 1)
	assert.Equal(t, "second", next.Records[0].Msg)
}

func TestNoRingIsAnEmptyLogRatherThanAFailure(t *testing.T) {
	s := newTestServer(t)
	was := logs.Installed
	logs.Installed = nil
	t.Cleanup(func() { logs.Installed = was })

	rec := do(t, s, "GET", "/api/logs", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"records":[]`)
}

// TestABurstOfLinesIsOneWakeUp is the one that keeps an agent run from making
// the log the busiest thing on the event stream.
func TestABurstOfLinesIsOneWakeUp(t *testing.T) {
	var sent atomic.Int32
	waker := &logWaker{send: func() { sent.Add(1) }}
	for i := 0; i < 500; i++ {
		waker.note()
	}
	assert.Zero(t, sent.Load(), "nothing goes out inside the window")

	require.Eventually(t, func() bool { return sent.Load() == 1 }, 3*time.Second, 20*time.Millisecond)

	waker.note()
	require.Eventually(t, func() bool { return sent.Load() == 2 }, 3*time.Second, 20*time.Millisecond,
		"and the window opens again after it fires")
}

func TestAnAccessLineDoesNotWakeTheViewer(t *testing.T) {
	assert.False(t, wakeFor(logs.Record{Msg: "request"}))
	assert.True(t, wakeFor(logs.Record{Msg: "turn started"}))
	assert.True(t, wakeFor(logs.Record{Msg: "git failed"}))
}

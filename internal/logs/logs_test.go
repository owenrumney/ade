package logs

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheRingKeepsWhatTheTerminalDropped(t *testing.T) {
	var out bytes.Buffer
	// The terminal's level, which is the one that loses the debug line.
	ring := New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	log := slog.New(ring)

	log.Debug("list Linear issues", "err", "context deadline exceeded")
	log.Warn("something louder")

	assert.NotContains(t, out.String(), "Linear", "the terminal still drops it")

	kept := ring.Since(0)
	require.Len(t, kept, 2)
	assert.Equal(t, "list Linear issues", kept[0].Msg)
	assert.Equal(t, "DEBUG", kept[0].Level)
	assert.Equal(t, []Attr{{Key: "err", Value: "context deadline exceeded"}}, kept[0].Attrs)
	assert.Contains(t, out.String(), "something louder")
}

func TestSinceOnlyGivesBackWhatIsNew(t *testing.T) {
	ring := New(nil)
	log := slog.New(ring)

	log.Info("one")
	log.Info("two")
	at := ring.Latest()
	log.Info("three")

	fresh := ring.Since(at)
	require.Len(t, fresh, 1)
	assert.Equal(t, "three", fresh[0].Msg)
	assert.Len(t, ring.Since(0), 3)
}

func TestTheRingDropsTheOldestAndStaysInOrder(t *testing.T) {
	ring := New(nil)
	log := slog.New(ring)
	for i := 0; i < Keep+50; i++ {
		log.Info("line", "n", i)
	}

	kept := ring.Since(0)
	require.Len(t, kept, Keep)
	assert.Equal(t, "50", kept[0].Attrs[0].Value, "the first 50 fell off the back")
	assert.Equal(t, uint64(Keep+50), kept[len(kept)-1].Seq)
	for i := 1; i < len(kept); i++ {
		require.Greater(t, kept[i].Seq, kept[i-1].Seq, "oldest first")
	}
}

func TestAttrsFromAWithLoggerReachTheRing(t *testing.T) {
	ring := New(nil)
	log := slog.New(ring).With("component", "linear")
	log.Error("ask Linear", "err", "timeout")

	kept := ring.Since(0)
	require.Len(t, kept, 1)
	assert.Contains(t, kept[0].Attrs, Attr{Key: "component", Value: "linear"})
	assert.Contains(t, kept[0].Attrs, Attr{Key: "err", Value: "timeout"})
}

func TestARecordWakesThePage(t *testing.T) {
	ring := New(nil)
	woken := 0
	ring.OnChange(func(Record) { woken++ })
	slog.New(ring).Info("hello")
	assert.Equal(t, 1, woken)
}

func TestEnabledIsAlwaysTrueSoNothingIsLostBeforeTheRing(t *testing.T) {
	ring := New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError}))
	assert.True(t, ring.Enabled(context.Background(), slog.LevelDebug))
}

package limits

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACodexReadingCrossesProcesses(t *testing.T) {
	// Codex reports its windows on a run, so the launcher — which runs
	// nothing — can only learn them from the checkout that did.
	t.Setenv("HOME", t.TempDir())
	ran := NewCache()
	ran.Ask = func(context.Context) (Limits, error) { return Limits{}, errors.New("no login") }
	ran.NoteCodex(Limits{
		Provider: "codex", CheckedAt: time.Now(),
		Windows: []Window{{Name: "5h", Percent: 9}},
	})

	launcher := NewCache()
	launcher.Ask = func(context.Context) (Limits, error) { return Limits{}, errors.New("no login") }
	got := launcher.Snapshot(context.Background())
	require.NotNil(t, got.Codex, "the reading did not reach a second process")
	require.Len(t, got.Codex.Windows, 1)
	assert.Equal(t, 9.0, got.Codex.Windows[0].Percent)
}

func TestNoCodexReadingIsNotAZeroOne(t *testing.T) {
	// Nothing there means nothing to say, not "you have used none of it".
	t.Setenv("HOME", t.TempDir())
	c := NewCache()
	c.Ask = func(context.Context) (Limits, error) { return Limits{}, errors.New("no login") }
	assert.Nil(t, c.Snapshot(context.Background()).Codex)
}

func TestAFailedClaudeReadingIsNotRetriedEveryPoll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	asked := make(chan struct{}, 10)
	c := NewCache()
	c.Ask = func(context.Context) (Limits, error) {
		asked <- struct{}{}
		return Limits{}, errors.New("no login")
	}
	for range 5 {
		c.Snapshot(context.Background())
	}
	// The first ask is in flight; give it room to land before counting.
	<-asked
	time.Sleep(20 * time.Millisecond)
	assert.Emptyf(t, asked, "a missing login was asked about %d more times", len(asked))
}

func TestAFailedReadingSaysWhy(t *testing.T) {
	// "No plan usage to show" blamed the login for what might be an HTTP
	// status or a network error, and the reason was only at debug level.
	t.Setenv("HOME", t.TempDir())
	c := NewCache()
	c.Ask = func(context.Context) (Limits, error) { return Limits{}, errors.New("usage answered HTTP 401") }

	require.Eventually(t, func() bool {
		return c.Snapshot(context.Background()).Error != ""
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, "usage answered HTTP 401", c.Snapshot(context.Background()).Error)
}

func TestAGoodReadingCarriesNoError(t *testing.T) {
	// A stale number beside an error reads as a wrong number, not an old one.
	t.Setenv("HOME", t.TempDir())
	c := NewCache()
	c.Ask = func(context.Context) (Limits, error) {
		return Limits{Provider: "claude", CheckedAt: time.Now(), Windows: []Window{{Name: "5h", Percent: 12}}}, nil
	}
	require.Eventually(t, func() bool {
		return c.Snapshot(context.Background()).Claude != nil
	}, time.Second, 5*time.Millisecond)
	assert.Empty(t, c.Snapshot(context.Background()).Error)
}

func TestOneProcessAsksAndTheRestRead(t *testing.T) {
	// The account rate-limits: every ade asking on its own clock is how a
	// working login comes back as HTTP 429 and the panel shows nothing.
	t.Setenv("HOME", t.TempDir())
	first := NewCache()
	asked := 0
	first.Ask = func(context.Context) (Limits, error) {
		asked++
		return Limits{Provider: "claude", CheckedAt: time.Now(),
			Windows: []Window{{Name: "5h", Percent: 68}}}, nil
	}
	require.Eventually(t, func() bool {
		return first.Snapshot(context.Background()).Claude != nil
	}, time.Second, 5*time.Millisecond)

	second := NewCache()
	second.Ask = func(context.Context) (Limits, error) {
		t.Error("the second process asked the account instead of reading the first's answer")
		return Limits{}, errors.New("should not be called")
	}
	got := second.Snapshot(context.Background())
	require.NotNil(t, got.Claude)
	assert.Equal(t, 68.0, got.Claude.Windows[0].Percent)
	assert.Equal(t, 1, asked)
}

func TestARateLimitFallsBackToTheLastReading(t *testing.T) {
	// A 429 is the account saying "not now", not "you have no plan". Showing
	// the last number is better than showing nothing.
	t.Setenv("HOME", t.TempDir())
	writeShared(Limits{Provider: "claude", CheckedAt: time.Now().Add(-30 * time.Minute),
		Windows: []Window{{Name: "5h", Percent: 41}}})

	c := NewCache()
	c.Ask = func(context.Context) (Limits, error) { return Limits{}, errors.New("usage answered HTTP 429") }
	got := c.Snapshot(context.Background())
	require.NotNil(t, got.Claude, "a rate limit lost the last good reading")
	assert.Equal(t, 41.0, got.Claude.Windows[0].Percent)
	assert.Empty(t, got.Error, "a number that is merely old is not an error")
}

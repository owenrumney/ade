package machine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/session"
)

func TestWhatAServerIsDoing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		agent *session.AgentHealth
		want  string
	}{
		{"an error outranks everything", &session.AgentHealth{Running: true, Queued: 2, Error: "boom"}, Failed},
		{"a queue with nothing running is wedged", &session.AgentHealth{Queued: 2}, Wedged},
		{"a run with a queue behind it is still working", &session.AgentHealth{Running: true, Queued: 2}, Working},
		{"a run on its own is working", &session.AgentHealth{Running: true}, Working},
		{"a paused turn reads as asking, not working", &session.AgentHealth{Running: true, Asking: 1}, Asking},
		{"a failure still outranks a question", &session.AgentHealth{Running: true, Asking: 1, Error: "boom"}, Failed},
		{"nothing running and nothing waiting is idle", &session.AgentHealth{}, Idle},
		{"a server that told us nothing is idle", nil, Idle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, State(tc.agent))
		})
	}
}

func TestWhichStatesAreYourTurn(t *testing.T) {
	for _, state := range []string{Failed, Wedged, Asking} {
		assert.True(t, Attention(state), state+" is waiting on the user")
	}
	for _, state := range []string{Working, Idle} {
		assert.False(t, Attention(state), state+" is the agent's turn")
	}
}

func TestHowLongItHasBeenQuiet(t *testing.T) {
	now := time.Now()

	silent, ok := Silence(&session.AgentHealth{LastEventAt: now.Add(-90 * time.Second)}, now)
	assert.True(t, ok)
	assert.Equal(t, 90*time.Second, silent.Round(time.Second))

	_, ok = Silence(&session.AgentHealth{}, now)
	assert.False(t, ok, "an agent that never spoke has no silence to report")

	_, ok = Silence(nil, now)
	assert.False(t, ok)

	// A snapshot written a moment ahead of this clock is not negative silence.
	ahead, ok := Silence(&session.AgentHealth{LastEventAt: now.Add(time.Second)}, now)
	assert.True(t, ok)
	assert.Zero(t, ahead)
}

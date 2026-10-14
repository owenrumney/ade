package machine

import (
	"time"

	"github.com/owenrumney/ade/internal/session"
)

// The states a live server can be in, each a fact it recorded itself.
const (
	Failed  = "failed"
	Wedged  = "wedged"
	Asking  = "asking"
	Working = "working"
	Idle    = "idle"
)

// State names what a server's agent is doing. First match wins.
func State(a *session.AgentHealth) string {
	switch {
	case a == nil:
		return Idle
	case a.Error != "":
		return Failed
	case !a.Running && a.Queued > 0:
		return Wedged
	case a.Asking > 0:
		return Asking
	case a.Running:
		return Working
	default:
		return Idle
	}
}

// Silence is how long the agent has said nothing, and whether it ever has.
// Never folded into State: no threshold separates a hang from a long command.
func Silence(a *session.AgentHealth, now time.Time) (time.Duration, bool) {
	if a == nil || a.LastEventAt.IsZero() {
		return 0, false
	}
	if d := now.Sub(a.LastEventAt); d > 0 {
		return d, true
	}
	return 0, true
}

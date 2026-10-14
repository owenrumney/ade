// Package said reads back what a named agent put in the conversation. The main
// agent hears a subagent's reply once, and never its filed findings at all.
package said

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/delegate"
)

// Env names the capability the server hands its agent. Absent means whatever
// is asking was not started by ade.
const (
	EnvURL   = delegate.EnvURL
	EnvToken = delegate.EnvToken
)

// ErrNoSession is asking from outside a running ade.
var ErrNoSession = delegate.ErrNoSession

// Entry is one thing a named agent said: a reply, or a note it filed.
type Entry struct {
	Who  string    `json:"who"`
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Answer is what the server holds for one agent. Known false is a name nobody
// is defined as, which is a different thing from an agent that never spoke.
type Answer struct {
	Agent string  `json:"agent"`
	Said  []Entry `json:"said"`
	Known bool    `json:"known"`
}

// Ask reads what agent said in the session this process belongs to.
func Ask(ctx context.Context, agent string) (Answer, error) {
	agent = strings.TrimPrefix(strings.TrimSpace(agent), "@")
	if agent == "" {
		return Answer{}, errors.New("which agent?")
	}
	var out Answer
	err := delegate.Get(ctx, "what @"+agent+" said", "/api/internal/delegate/said",
		url.Values{"agent": {agent}}, &out)
	return out, err
}

// Format is the answer as the agent reads it: oldest first, each under who
// said it and when.
func Format(answer Answer) string {
	if len(answer.Said) == 0 {
		if !answer.Known {
			return fmt.Sprintf("There is no agent called @%s. `ade agents list` has the ones there are.\n", answer.Agent)
		}
		return fmt.Sprintf("@%s has not said anything in this conversation.\n", answer.Agent)
	}
	var out strings.Builder
	for _, entry := range answer.Said {
		fmt.Fprintf(&out, "--- @%s, %s\n%s\n\n", entry.Who, entry.At.Format(time.RFC3339), entry.Text)
	}
	return out.String()
}

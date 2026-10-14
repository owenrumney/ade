package busstate

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agentbus"
)

func TestAPullRequestNumberIsReadOffItsURL(t *testing.T) {
	assert.Equal(t, 123, PRNumberOf("https://github.com/infracost/infra/pull/123"))
	assert.Equal(t, 0, PRNumberOf("https://github.com/infracost/infra/pull/x"))
	assert.Equal(t, 0, PRNumberOf("nothing"))
}

// An agent asking you used to mean dnd. Under acceptEdits almost every shell
// call is an ask, so that read as dnd permanently and nobody could reach you.
func TestPresenceFollowsWhetherYouAreHere(t *testing.T) {
	assert.Equal(t, agentbus.PresenceActive, PresenceFor(true, ""))
	assert.Equal(t, agentbus.PresenceIdle, PresenceFor(false, ""))
}

func TestWhatYouSetByHandWins(t *testing.T) {
	assert.Equal(t, agentbus.PresenceIdle, PresenceFor(true, agentbus.PresenceIdle))
	assert.Equal(t, agentbus.PresenceActive, PresenceFor(false, agentbus.PresenceActive))
	assert.Equal(t, agentbus.PresenceDND, PresenceFor(true, agentbus.PresenceDND))
}

// Anything else is not a presence, and taking it would put a word on the bus
// that nobody can route against.
func TestNonsenseIsNotAnOverride(t *testing.T) {
	assert.Equal(t, agentbus.PresenceActive, PresenceFor(true, "busy"))
	assert.Equal(t, agentbus.PresenceIdle, PresenceFor(false, "lunch"))
}

// Active first, then idle, then dnd and anything unknown.
func TestPresenceRanksWhoWouldAnswerFirst(t *testing.T) {
	assert.Less(t, RankPresence(agentbus.PresenceActive), RankPresence(agentbus.PresenceIdle))
	assert.Less(t, RankPresence(agentbus.PresenceIdle), RankPresence(agentbus.PresenceDND))
	assert.Equal(t, RankPresence(agentbus.PresenceDND), RankPresence("nonsense"))
}

// A request aimed at you reads the same as one offered to the team unless the
// target survives the frame.
func TestAnOfferKeepsWhoItWasAddressedTo(t *testing.T) {
	body, err := json.Marshal(agentbus.ReviewRequestBody{PR: "https://github.com/o/r/pull/7"})
	require.NoError(t, err)
	for _, tt := range []struct {
		name string
		to   *agentbus.Target
		want string
	}{
		{"a person", &agentbus.Target{User: "owen@infracost.io"}, "owen@infracost.io"},
		{"a group", &agentbus.Target{Group: "engineering"}, "engineering"},
		{"anyone", &agentbus.Target{Anyone: true}, "anyone"},
		{"everyone", &agentbus.Target{All: true}, "*"},
		{"unaddressed", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := OfferOf(agentbus.Frame{
				ID: "1", Type: agentbus.TypeReviewRequest, To: tt.to, Body: body,
			})
			require.NotNil(t, got)
			assert.Equal(t, tt.want, got.To)
		})
	}
}

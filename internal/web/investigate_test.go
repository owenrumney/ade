package web

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheSkillIsTheMainChatsPromptAndNoOneElses(t *testing.T) {
	s := newTestServer(t)
	s.agent.projectPrompt = func(bool) string { return "project notes" }
	s.agent.setSkill("Never mutate.")

	assert.Contains(t, s.agent.mainPrompt(), "Never mutate.")
	assert.Contains(t, s.agent.mainPrompt(), "project notes")
	// A named agent gets its own brief; the session's skill is not it.
	assert.NotContains(t, s.sideProject(), "Never mutate.")
}

func TestTheOpeningTurnNamesTheTicketAndForbidsChanges(t *testing.T) {
	got := InvestigatePrompt("TRK-1072", "prod-triage")

	assert.Contains(t, got, "TRK-1072")
	assert.Contains(t, got, "prod-triage")
	assert.Contains(t, got, "Change nothing")
}

func TestUnboxDoesNotRememberWhatOneInvestigationNeeded(t *testing.T) {
	s := newTestServer(t)
	s.agent.sandbox = true

	s.agent.unbox()

	assert.False(t, s.agent.sandboxOn(), "the MCPs it needs are on this machine")
	on, known := s.agent.mem.Sandbox()
	assert.False(t, known && !on, "the next ade in this checkout is boxed as before")
}

func TestInvestigateHandoverRefusesWhatItCannotName(t *testing.T) {
	for _, tc := range []struct {
		name, body, wants string
	}{
		{"no ticket", `{"skill":"prod-triage"}`, "name a ticket"},
		{"prose", `{"ticket":"the login one"}`, "name a ticket"},
		{"unknown skill", `{"ticket":"TRK-1072","skill":"nowhere-at-all"}`, "read skill nowhere-at-all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			rec := do(t, s, http.MethodPost, "/api/investigate", tc.body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.wants)
		})
	}
}

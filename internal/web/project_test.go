package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/owenrumney/ade/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// remembered turns memory on for the test server's checkout and hands back its
// store. HOME is the test's own, so nothing touches the real ~/.ade.
func remembered(t *testing.T, s *Server) *memory.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	store := s.projectStore()
	require.NotNil(t, store, "the test checkout is a git repository")
	require.NoError(t, store.Remember(true))
	return store
}

func TestACheckoutWithNoRemoteStillHasAProject(t *testing.T) {
	// The test repository has no origin, so this is the path fallback and the
	// ordinary case for a local-only checkout.
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	store := s.projectStore()
	require.NotNil(t, store)
	assert.NotEmpty(t, store.Identity().Slug)
	assert.Empty(t, store.Identity().Remote)
}

func TestAProjectNobodyRememberedAddsNothingToARun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	assert.Empty(t, s.projectPrompt(true))
}

func TestTheMainConversationGetsBothAndANamedAgentOnlyTheInstructions(t *testing.T) {
	s := newTestServer(t)
	store := remembered(t, s)
	require.NoError(t, store.SetInstructions("Only ever drive the local dev environment."))
	propose(t, store, "Dev environment", "Runs on localhost:3000 with seeded data.")

	both := s.agent.project(true)
	assert.Contains(t, both, "Only ever drive the local dev environment.")
	assert.Contains(t, both, "Dev environment")

	// A named agent's job is narrow and its definition is its system prompt,
	// so it gets the instructions and not the notes.
	only := s.agent.project(false)
	assert.Contains(t, only, "Only ever drive the local dev environment.")
	assert.NotContains(t, only, "localhost:3000")
}

func TestOpeningTheProjectRecordsThisCheckoutAgainstIt(t *testing.T) {
	s := newTestServer(t)
	store := remembered(t, s)

	assert.Equal(t, store.Identity().Slug, memory.Ledger()[s.space().homeRoot],
		"so a worktree removed later still resolves to this project")
}

func TestTheApiHandsBackTheInstructionsAndWhatIsWaiting(t *testing.T) {
	s := newTestServer(t)
	store := remembered(t, s)
	require.NoError(t, store.SetInstructions("dev only"))
	propose(t, store, "Dev environment", "localhost:3000")

	var view ProjectView
	rec := do(t, s, "GET", "/api/project", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))

	assert.True(t, view.Remembered)
	assert.Equal(t, "dev only", view.Instructions)
	assert.Equal(t, memory.MaxInstructions, view.MaxLength)
	require.Len(t, view.Entries, 1)
	assert.Equal(t, "Dev environment", view.Entries[0].Heading)
}

func TestSavingInstructionsOverTheCapIsRefused(t *testing.T) {
	s := newTestServer(t)
	remembered(t, s)

	long := make([]byte, memory.MaxInstructions+1)
	for i := range long {
		long[i] = 'x'
	}
	rec := do(t, s, "PUT", "/api/project", `{"instructions":"`+string(long)+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRejectingAProposalTakesItOutOfTheQueue(t *testing.T) {
	s := newTestServer(t)
	store := remembered(t, s)
	added, err := store.Propose([]memory.Candidate{
		{Entry: memory.Entry{Heading: "Wrong", Body: "Not true"}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, added)

	id := store.Proposed()[0].ID
	rec := do(t, s, "POST", "/api/project/entry", `{"action":"reject","id":"`+id+`"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, store.Proposed())
	assert.Empty(t, store.Accepted())
}

func TestAnUnknownEntryActionIsRefused(t *testing.T) {
	s := newTestServer(t)
	remembered(t, s)
	rec := do(t, s, "POST", "/api/project/entry", `{"action":"burn","id":"x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestDistillingAProjectNobodyRememberedIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/project/distil", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTheDebtBadgeCountsWhatIsWaitingWithoutTheMemoryItself(t *testing.T) {
	s := newTestServer(t)
	store := remembered(t, s)
	_, err := store.Propose([]memory.Candidate{
		{Entry: memory.Entry{Heading: "One", Body: "a"}},
		{Entry: memory.Entry{Heading: "Two", Body: "b"}},
	})
	require.NoError(t, err)

	s.refreshDebt()
	debt := s.projectDebt()
	require.NotNil(t, debt)
	assert.True(t, debt.Remembered)
	assert.Equal(t, 2, debt.Proposed)
}

// propose puts one entry through the queue and accepts it, the only way in.
func propose(t *testing.T, store *memory.Store, heading, body string) {
	t.Helper()
	added, err := store.Propose([]memory.Candidate{{Entry: memory.Entry{Heading: heading, Body: body}}})
	require.NoError(t, err)
	require.Equal(t, 1, added)
	waiting := store.Proposed()
	_, err = store.Accept(waiting[len(waiting)-1].ID, "", "")
	require.NoError(t, err)
}

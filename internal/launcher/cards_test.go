package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/concierge"
)

// cardLauncher is a desk launcher whose redrafts and sends are stood in: a
// card must be exercised without a model and without a session.
func cardLauncher(t *testing.T) (*Server, *[]string) {
	t.Helper()
	s, _ := deskLauncher(t)
	var sent []string
	oldSend, oldDraft := sendFirst, redrafter
	sendFirst = func(_ context.Context, url, body string) error {
		sent = append(sent, url+" "+body)
		return nil
	}
	redrafter = func(_ context.Context, prompt string) (string, error) {
		return "redrafted, given: " + prompt[len(prompt)-20:], nil
	}
	t.Cleanup(func() { sendFirst, redrafter = oldSend, oldDraft })
	return s, &sent
}

func checkout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", dir, "init", "-q").Run())
	return dir
}

func cards(t *testing.T, s *Server) []Card {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/concierge/cards", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Cards []Card `json:"cards"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.Cards
}

// propose runs a tool call and returns the cards waiting afterwards.
func propose(t *testing.T, s *Server, args string) map[string]any {
	t.Helper()
	return call(t, s, "tools/call", `,"params":{"name":"start","arguments":`+args+`}`)
}

func TestStartingWorkOnlyDrawsACard(t *testing.T) {
	s, sent := cardLauncher(t)
	dir := checkout(t)

	answer := propose(t, s, `{"repo":`+quote(dir)+`,"text":"rename the worker image"}`)

	assert.Contains(t, said(t, answer), "Nothing happens until they press Start")
	assert.Empty(t, *sent, "and nothing has been started")
	waiting := cards(t, s)
	require.Len(t, waiting, 1)
	assert.Equal(t, CardStart, waiting[0].Kind)
	assert.Equal(t, "rename the worker image", waiting[0].Text)
}

func TestACardForACheckoutThatIsNotOneIsRefused(t *testing.T) {
	s, _ := cardLauncher(t)

	answer := propose(t, s, `{"repo":"/nowhere/at/all","text":"do a thing"}`)

	result := answer["result"].(map[string]any)
	assert.Equal(t, true, result["isError"])
	assert.Empty(t, cards(t, s))
}

func TestACardForASessionThatIsNotRunningIsRefused(t *testing.T) {
	s, _ := cardLauncher(t)

	answer := call(t, s, "tools/call",
		`,"params":{"name":"say","arguments":{"session":"s9","message":"run the tests"}}`)

	result := answer["result"].(map[string]any)
	assert.Equal(t, true, result["isError"])
	assert.Contains(t, said(t, answer), "not a conversation running on this machine")
}

func TestThePressIsWhatStartsTheWork(t *testing.T) {
	s, sent := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"rename the worker image"}`)
	id := cards(t, s)[0].ID

	rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"start"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, *sent, 1, "the new session is handed exactly what was on the card")
	assert.Contains(t, (*sent)[0], "rename the worker image")
	assert.Empty(t, cards(t, s), "and the card is gone, so it cannot be pressed twice")
}

func TestACancelledCardDoesNothingAtAll(t *testing.T) {
	s, sent := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"never mind"}`)
	id := cards(t, s)[0].ID

	rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"cancel"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, *sent)
	assert.Empty(t, cards(t, s))
}

func TestAnEditIsAnInstructionAndNotTheText(t *testing.T) {
	s, _ := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"rename the worker image"}`)
	id := cards(t, s)[0].ID

	rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id,
		`{"action":"edit","said":"start from the design, not the branch"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	after := cards(t, s)
	require.Len(t, after, 1)
	// What the person said is not what the session would be sent: the desk
	// writes it again, and the card still holds the desk's words.
	assert.NotEqual(t, "start from the design, not the branch", after[0].Text)
	assert.Contains(t, after[0].Text, "redrafted")
	assert.Equal(t, 1, after[0].Drafts)
}

func TestAnEditThatSaysNothingIsRefused(t *testing.T) {
	s, _ := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"rename the worker image"}`)
	id := cards(t, s)[0].ID

	rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"edit","said":"  "}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "rename the worker image", cards(t, s)[0].Text)
}

func TestACardCannotBeRedraftedForever(t *testing.T) {
	s, _ := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"rename the worker image"}`)
	id := cards(t, s)[0].ID

	for i := 0; i < maxDrafts; i++ {
		rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"edit","said":"again"}`)
		require.Equalf(t, http.StatusOK, rec.Code, "redraft %d: %s", i, rec.Body.String())
	}
	rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"edit","said":"again"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "start it or cancel it")
}

func TestAPressedCardCannotBePressedTwice(t *testing.T) {
	s, _ := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"once is enough"}`)
	id := cards(t, s)[0].ID
	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"start"}`).Code)

	rec := do(t, s, http.MethodPost, "/api/concierge/cards/"+id, `{"action":"start"}`)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTheDeskCannotPileUpProposals(t *testing.T) {
	s, _ := cardLauncher(t)
	dir := checkout(t)

	for i := 0; i < maxCards; i++ {
		propose(t, s, `{"repo":`+quote(dir)+`,"text":"one of many"}`)
	}
	answer := propose(t, s, `{"repo":`+quote(dir)+`,"text":"one too many"}`)

	assert.Contains(t, said(t, answer), "already waiting")
	assert.Len(t, cards(t, s), maxCards)
}

func TestACardThatStoodTooLongIsNotOffered(t *testing.T) {
	s, _ := cardLauncher(t)
	dir := checkout(t)
	propose(t, s, `{"repo":`+quote(dir)+`,"text":"this morning's idea"}`)

	s.cardMu.Lock()
	for id, card := range s.cards {
		card.At = time.Now().Add(-cardLife - time.Minute)
		s.cards[id] = card
	}
	s.cardMu.Unlock()

	assert.Empty(t, cards(t, s), "a proposal about an hour ago is not a proposal")
}

func TestTheCardRoutesNeedTheToken(t *testing.T) {
	s, _ := cardLauncher(t)

	for _, route := range [][2]string{
		{http.MethodGet, "/api/concierge/cards"},
		{http.MethodPost, "/api/concierge/cards/c1"},
	} {
		rec := unauthenticated(t, s, route[0], route[1])
		assert.Equalf(t, http.StatusForbidden, rec.Code, "%s was open", route[1])
	}
}

func TestTheRedraftIsToldWhatItWroteAndWhatWasSaid(t *testing.T) {
	card := Card{Kind: CardStart, Name: "ade", Text: "rename the worker image"}

	prompt := redraftPrompt(card, "start from the design")

	assert.Contains(t, prompt, "rename the worker image")
	assert.Contains(t, prompt, "start from the design")
	assert.Contains(t, prompt, "first turn of a new session in ade")
	assert.Contains(t, prompt, "nothing else")
}

func TestTheDeskKnowsItsOwnMCPPath(t *testing.T) {
	assert.Equal(t, "/mcp/concierge", concierge.MCPPath)
}

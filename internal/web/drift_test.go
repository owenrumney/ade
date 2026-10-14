package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/gitdiff"
)

const driftDoc = `# Cart

## Problem

The cart forgets itself.

## Approach

### Storage

- Cache the cart in Redis.
- Expire it after an hour.

### Wiring

- Read the cart on every request.
`

func getDrift(t *testing.T, s *Server) DriftState {
	t.Helper()
	rec := do(t, s, "GET", "/api/drift", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body DriftState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func itemByText(t *testing.T, items []DriftItem, text string) DriftItem {
	t.Helper()
	for _, item := range items {
		if item.Text == text {
			return item
		}
	}
	require.FailNowf(t, "no such requirement", "%q", text)
	return DriftItem{}
}

func TestTheChecklistIsEmptyWithNoDesignOpen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/drift", "")
	require.Equal(t, http.StatusOK, rec.Code)
	// Empty rather than null: the panel maps over it without a guard.
	assert.Contains(t, rec.Body.String(), `"items":[]`)
}

func TestTheChecklistIsTheApproachSectionAndNothingElse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")

	state := getDrift(t, s)
	assert.Equal(t, "cart.md", state.File)
	assert.True(t, state.At.IsZero(), "no pass has run")

	var texts []string
	for _, item := range state.Items {
		texts = append(texts, item.Text)
		assert.Empty(t, item.Verdict, "nothing is scored before a pass")
		assert.NotEmpty(t, item.Hash)
	}
	assert.Equal(t, []string{
		"Storage", "Cache the cart in Redis.", "Expire it after an hour.",
		"Wiring", "Read the cart on every request.",
	}, texts)

	assert.Equal(t, design.KindHeading, state.Items[0].Kind)
	assert.Equal(t, design.KindBullet, state.Items[1].Kind)
}

// The checklist is rebuilt from the document on every read, so editing the
// design changes it without a pass having to run again.
func TestEditingTheDesignChangesTheChecklistWithoutAPass(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")
	require.Len(t, getDrift(t, s).Items, 5)

	writeDoc(t, s, "cart.md", driftDoc+"- Log every eviction.\n")
	after := getDrift(t, s)
	require.Len(t, after.Items, 6)
	assert.Equal(t, "Log every eviction.", after.Items[5].Text)
}

// A verdict was about the wording it was given. Rewording the requirement it
// scored must not carry the old score over, but rewording a different one must
// not lose it either.
func TestAVerdictOnlySurvivesWhileItsWordingDoes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")

	scored := getDrift(t, s).Items
	for i := range scored {
		scored[i].Verdict = design.DriftDone
	}
	s.mu.Lock()
	s.drift = &DriftState{File: "cart.md", At: time.Now(), Items: scored}
	s.mu.Unlock()

	carried := getDrift(t, s)
	assert.False(t, carried.At.IsZero(), "the pass time comes back with the checklist")
	for _, item := range carried.Items {
		assert.Equal(t, design.DriftDone, item.Verdict, item.Text)
	}

	reworded := `# Cart

## Approach

### Storage

- Cache the cart in Memcached.
- Expire it after an hour.

### Wiring

- Read the cart on every request.
`
	writeDoc(t, s, "cart.md", reworded)
	items := getDrift(t, s).Items
	assert.Empty(t, itemByText(t, items, "Cache the cart in Memcached.").Verdict,
		"the reworded requirement is unscored")
	assert.Equal(t, design.DriftDone, itemByText(t, items, "Expire it after an hour.").Verdict,
		"its neighbours keep their verdicts")
}

// A score belongs to the document it was about, so opening another design shows
// an unscored checklist rather than the last one's verdicts.
func TestAScoreDoesNotFollowYouToAnotherDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", driftDoc)
	writeDoc(t, s, "other.md", driftDoc)
	s.setDoc("cart.md")

	scored := getDrift(t, s).Items
	for i := range scored {
		scored[i].Verdict = design.DriftDone
	}
	s.mu.Lock()
	s.drift = &DriftState{File: "cart.md", At: time.Now(), Items: scored}
	s.mu.Unlock()

	s.setDoc("other.md")
	state := getDrift(t, s)
	assert.True(t, state.At.IsZero())
	for _, item := range state.Items {
		assert.Empty(t, item.Verdict, item.Text)
	}
}

func TestDismissingARequirementSticksUntilItIsReworded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")

	id := itemByText(t, getDrift(t, s).Items, "Cache the cart in Redis.").ID
	rec := do(t, s, "POST", "/api/drift/dismiss", `{"id":"`+id+`","dismissed":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	items := getDrift(t, s).Items
	assert.True(t, itemByText(t, items, "Cache the cart in Redis.").Dismissed)
	assert.False(t, itemByText(t, items, "Expire it after an hour.").Dismissed,
		"waving one off does not wave off its neighbours")

	// Rewording it makes it a different requirement, so it comes back.
	writeDoc(t, s, "cart.md", `# Cart

## Approach

### Storage

- Cache the cart in Memcached.
`)
	assert.False(t, itemByText(t, getDrift(t, s).Items, "Cache the cart in Memcached.").Dismissed)
}

func TestRestoringARequirementScoresItAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")

	id := itemByText(t, getDrift(t, s).Items, "Wiring").ID
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/drift/dismiss", `{"id":"`+id+`","dismissed":true}`).Code)
	require.True(t, itemByText(t, getDrift(t, s).Items, "Wiring").Dismissed)

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/drift/dismiss", `{"id":"`+id+`","dismissed":false}`).Code)
	assert.False(t, itemByText(t, getDrift(t, s).Items, "Wiring").Dismissed)
}

func TestDismissRefusesWhatItCannotIdentify(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	// No document open at all.
	rec := do(t, s, "POST", "/api/drift/dismiss", `{"id":"approach#b1","dismissed":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")

	assert.Equal(t, http.StatusBadRequest,
		do(t, s, "POST", "/api/drift/dismiss", `{"id":"  ","dismissed":true}`).Code)
	assert.Equal(t, http.StatusNotFound,
		do(t, s, "POST", "/api/drift/dismiss", `{"id":"nosuch#b9","dismissed":true}`).Code)
}

func TestARunNeedsADesignWithSomethingToScore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/drift", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no design document")

	// A document with nothing in it is nothing to score, not an empty pass.
	writeDoc(t, s, "empty.md", "# Cart\n\n## Approach\n")
	s.setDoc("empty.md")
	rec = do(t, s, "POST", "/api/drift", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	// Named as the user would type them: this repository heads its work
	// "What this builds", and a message naming only one of them misled.
	for _, heading := range design.ApproachHeadings {
		assert.Contains(t, rec.Body.String(), "## "+heading)
	}

	// A branch that has changed nothing has not drifted from anything.
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")
	s.mu.Lock()
	s.diff = nil
	s.mu.Unlock()
	rec = do(t, s, "POST", "/api/drift", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "the diff is empty")
}

func TestAContradictionCarriesTheDesignLineItIsAnswering(t *testing.T) {
	// The thread is read on the diff, away from the document, so the
	// requirement has to travel with the note.
	assert.Equal(t, "it writes straight through\n\nThe design says: Cache the cart in Redis.",
		contradiction("it writes straight through", "Cache the cart in Redis."))
	assert.Equal(t, "this contradicts the design\n\nThe design says: Cache the cart in Redis.",
		contradiction("  ", "Cache the cart in Redis."))
}

func TestTheSummaryCountsVerdictsAndSkipsWhatWasWavedOff(t *testing.T) {
	items := []DriftItem{
		{Verdict: design.DriftDone},
		{Verdict: design.DriftDone},
		{Verdict: design.DriftPartial},
		{Verdict: design.DriftAbsent},
		{Verdict: design.DriftContradicted},
		{Verdict: design.DriftContradicted, Dismissed: true},
	}
	assert.Equal(t,
		"Drift against cart.md: 2 done, 1 partial, 1 absent, 1 contradicted, 3 filed.",
		driftSummary("cart.md", items, 3))
}

// The checklist is on the page, and a pass says only its count in the
// conversation, so this is the agent's only route to which ones are not done.
func TestTheAgentReadsTheChecklistOverTheCapability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "secret"
	writeDoc(t, s, "cart.md", driftDoc)
	s.setDoc("cart.md")

	// The page's own token is not this one.
	rec := do(t, s, http.MethodGet, "/api/internal/delegate/drift", "")
	assert.Equal(t, http.StatusForbidden, rec.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/internal/delegate/drift", nil)
	req.Header.Set("X-Ade-Delegate-Token", "secret")
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got DriftState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "cart.md", got.File)
	itemByText(t, got.Items, "Cache the cart in Redis.")
}

// Scoring reads the diff and the document and writes neither, so a turn in
// flight is no reason to refuse it: it runs beside the conversation.
func TestAScoreRunsWhileTheAgentIsBusy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.setDoc("cart.md")
	writeDoc(t, s, "cart.md", driftDoc)
	s.diff = &gitdiff.Diff{Files: []gitdiff.File{{Path: "cart.go"}}}

	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok, "the conversation is mid-turn")

	rec := do(t, s, "POST", "/api/drift", "")
	assert.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
}

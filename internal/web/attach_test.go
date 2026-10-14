package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestASelectionWaitsForTheNextMessage(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/attach", `{"from":"a.go:12","text":"func f() {}"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	got := s.currentAttachment()
	require.NotNil(t, got)
	assert.Equal(t, "a.go:12", got.From)
	assert.Equal(t, "func f() {}", got.Text)
}

func TestASecondSelectionReplacesTheFirst(t *testing.T) {
	s := newTestServer(t)

	do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"first"}`)
	do(t, s, "POST", "/api/attach", `{"from":"b.go:2","text":"second"}`)

	// Two glued together is neither of the things that were selected.
	require.NotNil(t, s.currentAttachment())
	assert.Equal(t, "second", s.currentAttachment().Text)
}

func TestTakingTheSelectionClearsIt(t *testing.T) {
	s := newTestServer(t)
	do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"x"}`)

	rec := do(t, s, "DELETE", "/api/attach", "")
	require.Equal(t, http.StatusOK, rec.Code)

	// Otherwise reopening the page re-attaches what was already sent.
	assert.Nil(t, s.currentAttachment())
}

func TestNothingSelectedIsNotAnAttachment(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"   \n  "}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Nil(t, s.currentAttachment())
}

func TestASelectionTooLongToBeContextIsRefused(t *testing.T) {
	s := newTestServer(t)
	body := `{"from":"a.go:1","text":"` + strings.Repeat("x", maxAttachText+1) + `"}`

	rec := do(t, s, "POST", "/api/attach", body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Nil(t, s.currentAttachment())
}

// The label is drawn in a chip and sent on every state poll, so it is bounded
// too, not only the selection.
func TestALabelLongerThanAChipIsCut(t *testing.T) {
	s := newTestServer(t)
	body := `{"from":"` + strings.Repeat("p", maxAttachFrom*4) + `","text":"x"}`

	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/attach", body).Code)

	held := s.currentAttachment()
	require.NotNil(t, held)
	assert.Len(t, held.From, maxAttachFrom)
}

// Every open page competes for the one attachment. A page that clears it
// without saying which one throws away a selection made while it was reading.
func TestClearingAnAttachmentOnlyClearsTheOneTaken(t *testing.T) {
	s := newTestServer(t)

	first := do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"one"}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var took Attachment
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &took))
	require.NotZero(t, took.Seq)

	// A second selection lands before the first reader gets round to clearing.
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/attach", `{"from":"b.go:2","text":"two"}`).Code)

	rec := do(t, s, "DELETE", fmt.Sprintf("/api/attach?seq=%d", took.Seq), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"cleared":false`)

	held := s.currentAttachment()
	require.NotNil(t, held, "the newer selection survived the older page's delete")
	assert.Equal(t, "b.go:2", held.From)
}

func TestClearingTheCurrentAttachmentTakesIt(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"one"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var took Attachment
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &took))

	rec = do(t, s, "DELETE", fmt.Sprintf("/api/attach?seq=%d", took.Seq), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"cleared":true`)
	assert.Nil(t, s.currentAttachment())
}

// A client that names no number is the old behaviour: take whatever is there.
func TestClearingWithoutANumberStillWorks(t *testing.T) {
	s := newTestServer(t)
	require.Equal(t, http.StatusOK, do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"one"}`).Code)
	require.Equal(t, http.StatusOK, do(t, s, "DELETE", "/api/attach", "").Code)
	assert.Nil(t, s.currentAttachment())
}

// Each selection is a new one, or a page could clear a later attachment with
// a number it remembered from an earlier one.
func TestEachAttachmentGetsItsOwnNumber(t *testing.T) {
	s := newTestServer(t)
	seen := map[int64]bool{}
	for i := 0; i < 3; i++ {
		rec := do(t, s, "POST", "/api/attach", `{"from":"a.go:1","text":"one"}`)
		require.Equal(t, http.StatusOK, rec.Code)
		var got Attachment
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.False(t, seen[got.Seq], "reused %d", got.Seq)
		seen[got.Seq] = true
	}
}

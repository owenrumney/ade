package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/docnotes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const notesDoc = "# Plan\n\nThe cart recomputes on every keystroke.\n\n## Approach\n\nDebounce it.\n"

func openNotesDoc(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", notesDoc)
	s.setDoc("plan.md")
	return s
}

func listNotes(t *testing.T, s *Server) []docnotes.Note {
	t.Helper()
	rec := do(t, s, "GET", "/api/design/notes", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Notes []docnotes.Note `json:"notes"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got.Notes
}

func fileNote(t *testing.T, s *Server, body string) docnotes.Note {
	t.Helper()
	rec := do(t, s, "POST", "/api/design/notes", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var note docnotes.Note
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &note))
	return note
}

func TestANoteIsFiledOnQuotedTextAndListedBack(t *testing.T) {
	s := openNotesDoc(t)
	note := fileNote(t, s, `{"quote":"Debounce it.","nth":0,"body":"how long?"}`)

	assert.Equal(t, "Debounce it.", note.Anchor.Quote)
	assert.Equal(t, "you", note.Comments[0].Author)

	notes := listNotes(t, s)
	require.Len(t, notes, 1)
	assert.Equal(t, note.ID, notes[0].ID)
}

func TestANoteOnTextTheDocumentDoesNotHaveIsRefused(t *testing.T) {
	s := openNotesDoc(t)
	rec := do(t, s, "POST", "/api/design/notes", `{"quote":"cache it at the edge","body":"no"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// The quote is there, but not that many times.
	rec = do(t, s, "POST", "/api/design/notes", `{"quote":"Debounce it.","nth":3,"body":"no"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestNotesNeedADocumentOpen(t *testing.T) {
	s := newTestServer(t)
	for _, call := range []struct{ method, path string }{
		{"GET", "/api/design/notes"},
		{"POST", "/api/design/notes"},
		{"POST", "/api/design/notes/x/reply"},
		{"DELETE", "/api/design/notes/x"},
	} {
		rec := do(t, s, call.method, call.path, `{"quote":"x","body":"y"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, call.path)
	}
}

func TestReplyingToANoteOpensItAgain(t *testing.T) {
	s := openNotesDoc(t)
	note := fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)

	rec := do(t, s, "POST", "/api/design/notes/"+note.ID+"/resolve", `{"resolved":true}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, listNotes(t, s)[0].Resolved)

	rec = do(t, s, "POST", "/api/design/notes/"+note.ID+"/reply", `{"body":"300ms"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	notes := listNotes(t, s)
	require.Len(t, notes, 1)
	assert.False(t, notes[0].Resolved)
	assert.Len(t, notes[0].Comments, 2)
}

func TestAnUnknownNoteIsFourOhFour(t *testing.T) {
	s := openNotesDoc(t)
	fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)

	rec := do(t, s, "POST", "/api/design/notes/ghost/reply", `{"body":"x"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = do(t, s, "DELETE", "/api/design/notes/ghost", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeletingANoteRemovesIt(t *testing.T) {
	s := openNotesDoc(t)
	note := fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)

	rec := do(t, s, "DELETE", "/api/design/notes/"+note.ID, "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, listNotes(t, s))
}

func TestListingNotesReanchorsAgainstWhatTheDocumentSaysNow(t *testing.T) {
	s := openNotesDoc(t)
	note := fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)

	// The agent rewrites the document underneath the page. The quote survives,
	// so the note moves with it rather than orphaning.
	writeDoc(t, s, "plan.md", "# Plan\n\nA much shorter problem statement.\n\n## Approach\n\nDebounce it.\n")
	notes := listNotes(t, s)
	require.Len(t, notes, 1)
	assert.False(t, notes[0].Orphaned)
	assert.NotEqual(t, note.Anchor.At, notes[0].Anchor.At)

	// Now the quote goes with it. The note stays, marked.
	writeDoc(t, s, "plan.md", "# Plan\n\nNothing of the old document survives.\n")
	notes = listNotes(t, s)
	require.Len(t, notes, 1)
	assert.True(t, notes[0].Orphaned)
}

func TestNotesFollowTheDocumentThatIsOpen(t *testing.T) {
	s := openNotesDoc(t)
	fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)

	writeDoc(t, s, "other.md", notesDoc)
	s.setDoc("other.md")
	assert.Empty(t, listNotes(t, s), "a note belongs to one document")

	s.setDoc("plan.md")
	assert.Len(t, listNotes(t, s), 1)
}

func TestAReviewersBlocksBecomeNotesOnTheDocument(t *testing.T) {
	s := openNotesDoc(t)
	f := s.newFiler(reviewingAgent("architect"), "", "", true)
	f.push("Two points.\n\n```ade-note\n" +
		`{"quote": "Debounce it.", "note": "how long, and debounced where?"}` +
		"\n```\n")
	f.push("```ade-note\n" +
		`{"quote": "a sentence the document has never contained", "note": "this one has nowhere to sit"}` +
		"\n```\n")
	f.done()

	notes := listNotes(t, s)
	require.Len(t, notes, 1, "only the point whose quote is in the document is filed")
	assert.Equal(t, "Debounce it.", notes[0].Anchor.Quote)
	assert.Equal(t, "architect", notes[0].Comments[0].Author)

	// The one that could not be anchored still reaches the conversation: the
	// block is stripped from the reply, so this is all that is left of it.
	assert.Contains(t, f.asideText(), "nowhere to sit")
	said := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, said)
	assert.Contains(t, said[len(said)-2].Body, "filed 1 note(s) on the document")
	assert.Contains(t, said[len(said)-1].Body, "the document does not contain")
}

func TestADocReviewerFilesNothingOnTheDiff(t *testing.T) {
	s := openNotesDoc(t)
	f := s.newFiler(reviewingAgent("architect"), "", "", true)
	// a.go line 3 is the test repo's only change, so this would be filed by
	// any other reviewer. A design review is not reviewing the code.
	f.push("```ade-finding\n" +
		`{"file": "a.go", "line": 3, "category": "correctness", "severity": "high", "summary": "x is magic"}` +
		"\n```\n")
	f.done()

	assert.Empty(t, s.ws.store.List())
	assert.Empty(t, listNotes(t, s))
}

func TestADesignReviewNeedsADocumentWithSomethingInIt(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/review", `{"agent":"architect"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	writeDoc(t, s, "empty.md", "   \n")
	s.setDoc("empty.md")
	rec = do(t, s, "POST", "/api/design/review", `{"agent":"architect"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing in empty.md")
}

func TestADesignReviewRefusesAnAgentThatDoesNotReview(t *testing.T) {
	s := openNotesDoc(t)
	rec := do(t, s, "POST", "/api/design/review", `{"agent":"nobody-by-that-name"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no agent called @nobody-by-that-name")
}

func TestAcceptingRecommendationsStartsOneFold(t *testing.T) {
	s := openNotesDoc(t)
	rec := do(t, s, "POST", "/api/design/accept",
		`{"rows":[{"ref":"1","question":"Where does a note live?","recommendation":"sidecar"},`+
			`{"ref":"2","question":"Who sweeps an orphan?","recommendation":"nobody"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	said := s.conversation()
	require.NotEmpty(t, said)
	assert.Contains(t, said[len(said)-1].Body, "accepted 2 recommendation(s)")
}

func TestAcceptingFoldsIntoTheDocumentTheRowsCameFrom(t *testing.T) {
	// The page may be showing a document this server never opened, so the
	// name it sends wins over the one the server holds.
	s := openNotesDoc(t)
	writeDoc(t, s, "ship.md", "# Ship\n")

	rec := do(t, s, "POST", "/api/design/accept",
		`{"file":"ship.md","rows":[{"ref":"1","question":"Where do gates live?","recommendation":"internal/ship"}]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	said := s.conversation()
	require.NotEmpty(t, said)
	assert.Contains(t, said[len(said)-1].Body, "ship.md")
	assert.NotContains(t, said[len(said)-1].Body, "plan.md")
}

func TestAcceptingRefusesAFileOutsideTheDesignDirectory(t *testing.T) {
	s := openNotesDoc(t)
	rec := do(t, s, "POST", "/api/design/accept",
		`{"file":"../../etc/passwd.md","rows":[{"ref":"1","question":"q","recommendation":"r"}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAcceptingNothingIsRefused(t *testing.T) {
	s := openNotesDoc(t)
	rec := do(t, s, "POST", "/api/design/accept", `{"rows":[]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// A row with no recommendation is not an acceptance.
	rec = do(t, s, "POST", "/api/design/accept", `{"rows":[{"ref":"1","question":"Open?","recommendation":"  "}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAcceptingNeedsADocumentOpen(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/accept", `{"rows":[{"question":"q","recommendation":"r"}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTheDesignReviewersAreTheOnesThatCannotEdit(t *testing.T) {
	// architect ships with reviews: false — that governs findings on a diff,
	// and must not keep the design agent out of a design review.
	assert.True(t, mayNote(agents.Agent{Name: "architect", Tools: []string{"Read", "Grep", "Glob", "Bash"}}))
	assert.True(t, mayNote(agents.Agent{Name: "second-opinion", Tools: []string{"Read", "Grep", "Glob"}}))
	// condenser rewrites the document, so it is not a reviewer of one.
	assert.False(t, mayNote(agents.Agent{Name: "condenser", Tools: []string{"Read", "Write", "Edit"}}))
}

func TestANonReviewingAgentStillFilesItsNotes(t *testing.T) {
	// @architect is reviews: false. Its ade-note blocks still land, because
	// that flag is about the diff and this is not the diff.
	s := openNotesDoc(t)
	f := s.newFiler(agents.Agent{Name: "architect", Reviews: false}, "", "", true)
	f.push("```ade-note\n" +
		`{"quote": "Debounce it.", "note": "how long, and debounced where?"}` +
		"\n```\n")
	f.done()

	notes := listNotes(t, s)
	require.Len(t, notes, 1)
	assert.Equal(t, "architect", notes[0].Comments[0].Author)
}

func TestAFoldCarriesWhatTheAgentFiledNotJustWhatItSaid(t *testing.T) {
	// A reviewer's reply is one line and its points are comments. Folding the
	// reply alone is how ten of eleven points went missing.
	s := openNotesDoc(t)
	f := s.newFiler(reviewingAgent("architect"), "", "", true)
	f.push("```ade-note\n" +
		`{"quote": "Debounce it.", "note": "how long, and debounced where?"}` +
		"\n```\n")
	f.done()

	filed := s.filedBy("architect")
	require.Len(t, filed, 1)
	assert.Contains(t, filed[0].Where, "Debounce it.")
	assert.Equal(t, "how long, and debounced where?", filed[0].Text)

	prompt := design.FoldPrompt("architect", "plan.md", "One point, worst first.", filed...)
	assert.Contains(t, prompt, "One point, worst first.")
	assert.Contains(t, prompt, "how long, and debounced where?")
}

func TestAResolvedPointIsNotFoldedAgain(t *testing.T) {
	s := openNotesDoc(t)
	note := fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)
	require.Len(t, s.filedBy("you"), 1)

	rec := do(t, s, "POST", "/api/design/notes/"+note.ID+"/resolve", `{"resolved":true}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, s.filedBy("you"), "a point already dealt with is not folded in")
}

func TestNobodyElsesPointsAreFolded(t *testing.T) {
	s := openNotesDoc(t)
	fileNote(t, s, `{"quote":"Debounce it.","body":"how long?"}`)
	assert.Empty(t, s.filedBy("architect"))
	assert.Empty(t, s.filedBy(""))
}

// A window showing a design from another space has it open; this server does
// not, and reviewed nothing because it asked itself which document that was.
func TestADesignReviewTakesTheDocumentItIsGiven(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "   \n")

	rec := do(t, s, "POST", "/api/design/review", `{"agent":"architect","file":"cart.md"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "nothing in cart.md",
		"the named document was read, not this server's own idea of one")
}

func TestADesignReviewRefusesADocumentThatIsNotThere(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/review", `{"agent":"architect","file":"../secrets.md"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

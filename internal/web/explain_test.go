package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplainIsASideRunAndFilesNothing(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")

	rec := do(t, s, "POST", "/api/explain", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no agent called @eli5")

	defineUserAgent(t, "eli5", "---\nname: eli5\nreviews: false\n---\nkeep it simple\n")
	rec = do(t, s, "POST", "/api/explain", `{}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"agent":"eli5"`)

	yours := messagesOfKind(s.conversation(), chat.KindYou)
	require.Len(t, yours, 1)
	assert.Equal(t, "@eli5 explain the diff (1 files)", yours[0].Body)
	// A side run, so the main agent is free.
	assert.False(t, s.agent.State().Running)
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)
}

func TestAnAgentThatDoesNotReviewIsNotAReviewer(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineUserAgent(t, "eli5", "---\nname: eli5\nreviews: false\n---\nkeep it simple\n")
	defineUserAgent(t, "security", "---\nname: security\n---\ntrust nothing\n")

	rec := do(t, s, "POST", "/api/review", `{"agents":["security","eli5"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "@eli5 does not review changes")
	// All or nothing, as with an unknown name: neither of them ran.
	assert.Empty(t, messagesOfKind(s.conversation(), chat.KindYou))
}

// A non-reviewer's ade-finding block is not a comment on the diff, however it
// came by one.
func TestTheFilerIsOffForAnAgentThatDoesNotReview(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defineUserAgent(t, "eli5", "---\nname: eli5\nreviews: false\n---\nkeep it simple\n")
	defineUserAgent(t, "security", "---\nname: security\n---\ntrust nothing\n")

	block := "```ade-finding\n" +
		`{"file": "a.go", "line": 3, "category": "correctness", "severity": "high", "headline": "The handler drops the error.", "summary": "it does"}` +
		"\n```\n"

	list := s.userAgentsList()
	quiet, ok := agents.Find(list, "eli5")
	require.True(t, ok)
	f := s.newFiler(quiet, "", "")
	f.push(block)
	assert.Empty(t, s.space().store.List())

	loud, ok := agents.Find(list, "security")
	require.True(t, ok)
	f = s.newFiler(loud, "", "")
	f.push(block)
	assert.NotEmpty(t, s.space().store.List())
}

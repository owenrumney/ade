package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
)

func TestSendToAgentWritesTheFindingsAndTypesOneLine(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)
	add := func(thread comments.Thread) {
		t.Helper()
		_, err := s.ws.store.Add(thread)
		require.NoError(t, err)
	}
	add(comments.Thread{
		File: "a.go", Side: comments.SideRight, Line: 3, Severity: "low", Category: "naming",
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "x is not a name"}},
	})
	add(comments.Thread{
		File: "a.go", Side: comments.SideRight, Line: 1, Severity: "high",
		Comments: []comments.Comment{{Author: "reviewer", Body: "no package comment"}, {Author: "owen", Body: "agreed"}},
	})

	rec := httptest.NewRecorder()
	s.handleSendFindings(rec, httptest.NewRequest(http.MethodPost, "/api/agent/findings", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body, err := os.ReadFile(filepath.Join(s.ws.gitDir, "ade", findingsFile))
	require.NoError(t, err)
	text := string(body)
	assert.Less(t, strings.Index(text, "no package comment"), strings.Index(text, "x is not a name"), "worst first")
	assert.Contains(t, text, "## 1. a.go:1")
	assert.Contains(t, text, "Severity: low. Category: naming.")
	assert.Contains(t, text, "Reply from owen: agreed")

	assert.Contains(t, seen(t, s.agentTerm(), findingsFile), "each of the 2 review findings")
}

// The id under each heading is how the outcome finds its way back.
func TestTheFindingsFileNamesEachThreadAndAsksForAnOutcome(t *testing.T) {
	s := newTestServer(t)
	thread, err := s.ws.store.Add(comments.Thread{
		File: "a.go", Side: comments.SideRight, Line: 3, Severity: "low",
		Comments: []comments.Comment{{Author: comments.AuthorYou, Body: "x is not a name"}},
	})
	require.NoError(t, err)
	text := findingsText(s.ws.store.Unresolved())
	assert.Contains(t, text, "## 1. a.go:3 · id "+thread.ID)
	assert.Contains(t, text, "`Outcome: fixed` or `Outcome: stands")
}

func TestAFinishedTurnReadsTheOutcomesBack(t *testing.T) {
	s := newTestServer(t)
	add := func(line int, body string) comments.Thread {
		t.Helper()
		thread, err := s.ws.store.Add(comments.Thread{
			File: "a.go", Side: comments.SideRight, Line: line,
			Comments: []comments.Comment{{Author: "reviewer", Body: body}},
		})
		require.NoError(t, err)
		return thread
	}
	fixed, stands, silent := add(1, "rename x"), add(2, "restore the check"), add(3, "add a test")
	text := findingsText(s.ws.store.Unresolved())
	text = strings.Replace(text, "\nrename x\n", "\nrename x\n\nOutcome: fixed — renamed to count\n", 1)
	text = strings.Replace(text, "\nrestore the check\n", "\nrestore the check\n\nOutcome: stands — the removal was deliberate\n", 1)
	dir := filepath.Join(s.ws.gitDir, "ade")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, findingsFile), []byte(text), 0o600))

	s.hookToken = "hook-secret"
	req := httptest.NewRequest(http.MethodPost, BadgePath, strings.NewReader(`{"hook_event_name":"Stop"}`))
	req.Header.Set("Authorization", "Bearer hook-secret")
	rec := httptest.NewRecorder()
	s.handleAgentBadge(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	got, err := s.ws.store.Get(fixed.ID)
	require.NoError(t, err)
	assert.True(t, got.Resolved, "a fixed finding is a resolved comment")
	assert.Equal(t, "renamed to count", got.Comments[1].Body)

	got, err = s.ws.store.Get(stands.ID)
	require.NoError(t, err)
	assert.False(t, got.Resolved)
	assert.Equal(t, comments.FateDeclined, got.Fate())
	assert.Equal(t, "the removal was deliberate", got.Comments[1].Body)

	got, err = s.ws.store.Get(silent.ID)
	require.NoError(t, err)
	assert.Len(t, got.Comments, 1, "a finding with no outcome line is left as it was")

	// The next turn's Stop reads the same file: nothing is said twice.
	s.handleAgentBadge(httptest.NewRecorder(), req)
	got, err = s.ws.store.Get(fixed.ID)
	require.NoError(t, err)
	assert.Len(t, got.Comments, 2)
}

func TestSendToAgentRefusesWithNothingOpen(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.handleSendFindings(rec, httptest.NewRequest(http.MethodPost, "/api/agent/findings", nil))

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Nil(t, s.agentTerm(), "no agent is started for nothing")
}

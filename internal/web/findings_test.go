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

func TestSendToAgentRefusesWithNothingOpen(t *testing.T) {
	s := newTestServer(t)

	rec := httptest.NewRecorder()
	s.handleSendFindings(rec, httptest.NewRequest(http.MethodPost, "/api/agent/findings", nil))

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Nil(t, s.agentTerm(), "no agent is started for nothing")
}

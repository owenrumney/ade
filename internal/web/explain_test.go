package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func TestExplainingTheChangeWritesTheDiffAndTypesOneLineAtTheAgent(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)

	rec := do(t, s, "POST", "/api/agent/explain", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	path := filepath.Join(s.ws.gitDir, "ade", changeFile)
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(body), "a.go", "the rendered diff names its files")

	got := seen(t, s.agentTerm(), "Mermaid flowchart")
	assert.Contains(t, got, "Explain this change as a Claude artifact")
	assert.Contains(t, got, changeFile)
	assert.Contains(t, got, "Paste the link here")
}

func TestTheExplainAskNamesTheFileTheScopeAndThePullRequest(t *testing.T) {
	branch := explainAsk("/x/change.diff", gitdiff.ScopeBranch, "main", "https://github.com/o/r/pull/7")
	assert.Contains(t, branch, "this branch against main is already in /x/change.diff")
	assert.Contains(t, branch, "The pull request is https://github.com/o/r/pull/7.")
	assert.Contains(t, branch, "rather than re-running git")

	change := explainAsk("/x/change.diff", gitdiff.ScopeUncommitted, "main", "")
	assert.Contains(t, change, "the uncommitted change is already in")
	assert.NotContains(t, change, "pull request")

	assert.Contains(t, explainAsk("/x/c", gitdiff.ScopeBranch, "", ""), "against its base")
}

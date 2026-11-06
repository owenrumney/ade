package web

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The button used to run gh --fill, which pastes the commit messages in as
// the description. The agent's skill writes one for a reviewer instead.
func TestCreatePRHandsTheBranchToTheAgentsSkill(t *testing.T) {
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)
	origin := t.TempDir()
	for _, args := range [][]string{
		{"-C", origin, "init", "--bare", "-q", "-b", "main"},
		{"-C", s.startRoot, "remote", "add", "origin", origin},
	} {
		out, err := exec.Command("git", args...).CombinedOutput()
		require.NoError(t, err, string(out))
	}

	rec := httptest.NewRecorder()
	s.handleCreatePR(rec, httptest.NewRequest(http.MethodPost, "/api/pr/create", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"asked":true`)
	assert.Contains(t, seen(t, s.agentTerm(), prCreateSkill), prCreateSkill)
}

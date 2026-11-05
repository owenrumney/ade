package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	convOld = "11111111-1111-4111-8111-111111111111"
	convNew = "22222222-2222-4222-8222-222222222222"
)

func TestContinuingMovesTheAgentOntoThePickedConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fakeAgent(t)
	s := newTestServer(t)
	t.Cleanup(s.closeTerminal)
	transcript(t, s.agent.dir, convNew, `{"type":"user","message":{"content":"hello"}}`)
	fresh, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)

	ask := func(id string) int {
		rec := httptest.NewRecorder()
		body := strings.NewReader(fmt.Sprintf(`{"id":%q}`, id))
		s.handleContinue(rec, httptest.NewRequest(http.MethodPost, "/api/agent/conversation", body))
		return rec.Code
	}
	assert.Equal(t, http.StatusBadRequest, ask(convOld), "claude holds no such conversation for this checkout")
	assert.Equal(t, http.StatusBadRequest, ask("../../etc/passwd"))

	openElsewhere(t, convNew)
	assert.Equal(t, http.StatusConflict, ask(convNew), "two claudes on one transcript write over each other")
	require.NoError(t, os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".claude", "sessions")))

	require.Equal(t, http.StatusOK, ask(convNew))
	require.Eventually(t, func() bool { return !fresh.Alive() }, 10*time.Second, 20*time.Millisecond,
		"an agent nobody typed at gives way")
	assert.Equal(t, convNew, s.agent.Current())
	pane, err := s.agentPane(t.Context(), "")
	require.NoError(t, err)
	assert.Contains(t, seen(t, pane, "ARGS:"), "--resume "+convNew)

	transcript(t, s.agent.dir, convOld, `{"type":"user","message":{"content":"older"}}`)
	assert.Equal(t, http.StatusConflict, ask(convOld), "its agent is running in its terminal")
	assert.Equal(t, convNew, s.agent.Current())
	quit(t, pane)
}

var notInProjectName = regexp.MustCompile(`[^A-Za-z0-9]`)

// transcript writes what claude would have kept for a conversation had in dir.
func transcript(t *testing.T, dir, id string, lines ...string) string {
	t.Helper()
	project := notInProjectName.ReplaceAllString(dir, "-")
	path := filepath.Join(os.Getenv("HOME"), ".claude", "projects", project, id+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
	return path
}

// openElsewhere is claude's record of a living process on a conversation.
func openElsewhere(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".claude", "sessions", "1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q}`, os.Getpid(), id)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

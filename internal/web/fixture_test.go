package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/fixtures"
)

func TestSavingATurnAsAFixtureKeepsThePromptAndThePreTurnTree(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n\nvar x = 9\n"), 0o600))
	said := s.say(ChatMessage{Kind: chat.KindYou, Body: "Narrow the comment in a.go"})
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package main\n\nvar x = 10\n"), 0o600))

	rec := do(t, s, "POST", "/api/turns/"+said.ID+"/fixture", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct{ Name, Dir string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "narrow-the-comment-in", got.Name)

	f, err := fixtures.Load(got.Dir, got.Name)
	require.NoError(t, err)
	assert.Equal(t, "Narrow the comment in a.go", f.Prompt)
	assert.Contains(t, string(f.Patch), "+var x = 9", "the tree before the turn, not after it")
	assert.NotContains(t, string(f.Patch), "var x = 10")

	rec = do(t, s, "POST", "/api/turns/"+said.ID+"/fixture", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"narrow-the-comment-in-2"`)

	agent := s.say(ChatMessage{Kind: chat.KindAgent, Body: "done"})
	assert.Equal(t, http.StatusNotFound, do(t, s, "POST", "/api/turns/"+agent.ID+"/fixture", "").Code)
}

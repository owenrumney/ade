package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
)

func TestUndoingATurnPutsTheFilesBackAndTellsTheAgent(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	file := filepath.Join(root, "a.go")
	was, err := os.ReadFile(file)
	require.NoError(t, err)

	said := s.say(ChatMessage{Kind: chat.KindYou, Body: "make x three\nand more"})
	require.NotEmpty(t, said.Undo, "a message of yours is snapshotted before it runs")
	require.NoError(t, os.WriteFile(file, []byte("package main\n\nvar x = 3\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "new.go"), []byte("package main\n"), 0o600))
	path := "/api/turns/" + said.ID + "/undo"

	rec := do(t, s, "GET", path, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"changes":[{"path":"a.go","do":"restore"},{"path":"new.go","do":"delete"}]}`, rec.Body.String())

	rec = do(t, s, "POST", path, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	now, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, string(was), string(now))
	assert.NoFileExists(t, filepath.Join(root, "new.go"))

	heard := s.heardSoFar()
	require.Len(t, heard, 1)
	assert.Contains(t, heard[0].Text, `undid every file change made since their message "make x three"`)
	assert.Contains(t, heard[0].Text, "a.go, new.go")

	t.Run("and undoing the undo brings the agent's version back", func(t *testing.T) {
		msg, _, ok := s.undoTarget(said.ID)
		require.True(t, ok)
		require.NotEmpty(t, msg.Undone)

		rec := do(t, s, "POST", path, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		now, err := os.ReadFile(file)
		require.NoError(t, err)
		assert.Equal(t, "package main\n\nvar x = 3\n", string(now))
		assert.FileExists(t, filepath.Join(root, "new.go"))

		msg, _, _ = s.undoTarget(said.ID)
		assert.Empty(t, msg.Undone)
		assert.Contains(t, s.heardSoFar()[1].Text, "put back the file changes they had undone")
	})
}

func TestUndoIsRefusedWhileATurnRuns(t *testing.T) {
	s := newTestServer(t)
	said := s.say(ChatMessage{Kind: chat.KindYou, Body: "go"})
	file := filepath.Join(s.space().homeRoot, "a.go")
	require.NoError(t, os.WriteFile(file, []byte("package main\n"), 0o600))

	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/turns/"+said.ID+"/undo", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	now, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "package main\n", string(now))
}

func TestUndoOfAMessageWithNoSnapshotIsNotFound(t *testing.T) {
	s := newTestServer(t)
	reply := s.say(ChatMessage{Kind: chat.KindAgent, Body: "done"})
	assert.Empty(t, reply.Undo)
	assert.Equal(t, http.StatusNotFound, do(t, s, "POST", "/api/turns/"+reply.ID+"/undo", "").Code)
	assert.Equal(t, http.StatusNotFound, do(t, s, "GET", "/api/turns/nope/undo", "").Code)
}

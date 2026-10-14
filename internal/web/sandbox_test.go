package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/browse"

	"github.com/owenrumney/ade/internal/sandbox"
)

func TestTheSandboxIsEditedFromThePageAndKeptUnderGit(t *testing.T) {
	s := newTestServer(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	other := filepath.Join(home, "code", "other")
	require.NoError(t, os.MkdirAll(other, 0o750))

	rec := do(t, s, "GET", "/api/sandbox", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view sandboxView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.False(t, view.On)
	assert.Empty(t, view.Paths)
	assert.Equal(t, filepath.Join(s.commonGitDir, "ade", "sandbox.json"), view.Path, "the repository's, so every worktree shares it")

	rec = do(t, s, "PUT", "/api/sandbox", `{"paths":[{"path":"`+other+`","rw":true}],"env":{"GOFLAGS":"-mod=mod"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, []sandbox.Path{{Path: other, RW: true}}, view.Paths)
	assert.Equal(t, map[string]string{"GOFLAGS": "-mod=mod"}, view.Env)

	rec = do(t, s, "PUT", "/api/sandbox", `{"paths":[{"path":"nowhere"}]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "absolute")

	ws, err := sandbox.LoadWorkspace(s.agent.workspace)
	require.NoError(t, err)
	assert.Len(t, ws.Paths, 1, "the bad save changed nothing")
}

func TestTheSandboxMarkerComingBackKeepsTheStoredSecret(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	rec := do(t, s, "PUT", "/api/sandbox", `{"env":{"NPM_TOKEN":"npm_real","CI":"1"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view sandboxView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	require.Equal(t, sandbox.Redacted, view.Env["NPM_TOKEN"])

	rec = do(t, s, "PUT", "/api/sandbox", `{"env":{"NPM_TOKEN":"`+sandbox.Redacted+`","CI":"2"}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	ws, err := sandbox.LoadWorkspace(s.agent.workspace)
	require.NoError(t, err)
	assert.Equal(t, "npm_real", ws.Env["NPM_TOKEN"], "the marker is not the value")
	assert.Equal(t, "2", ws.Env["CI"])

	// Renamed under the marker there is nothing to put back, so the save is
	// refused rather than writing the marker or an empty value.
	rec = do(t, s, "PUT", "/api/sandbox", `{"env":{"NPM_AUTH_TOKEN":"`+sandbox.Redacted+`"}}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	ws, err = sandbox.LoadWorkspace(s.agent.workspace)
	require.NoError(t, err)
	assert.Equal(t, "npm_real", ws.Env["NPM_TOKEN"])
}

// A mount change means a new container, and a new container means a new
// subprocess. The conversation is not meant to go with it.
func TestRestartSandboxKeepsAFreshConversation(t *testing.T) {
	a := &agentRunner{sandbox: true}
	a.note("sess-from-claude") // a conversation that started fresh: current only

	require.Empty(t, a.session)
	a.restartSandbox()
	assert.Equal(t, "sess-from-claude", a.session, "the next turn must resume, not start over")
}

func TestSetSandboxKeepsAFreshConversation(t *testing.T) {
	a := &agentRunner{}
	a.note("sess-from-claude")

	a.setSandbox(true)
	assert.Equal(t, "sess-from-claude", a.session)
}

// A container going out from under a running agent is invisible unless it is
// said; with nothing running there is nothing to announce.
func TestRestartSandboxReportsADroppedContainer(t *testing.T) {
	t.Run("nothing running", func(t *testing.T) {
		a := &agentRunner{sandbox: true}
		assert.False(t, a.restartSandbox())
	})

	t.Run("on the host", func(t *testing.T) {
		a := &agentRunner{}
		assert.False(t, a.restartSandbox())
	})
}

// The picker names a path; it does not read the disk. Directories only,
// with checkouts marked, since a checkout is usually what is wanted.
func TestBrowseListsDirectoriesOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "service", ".git"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cache"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a-file.txt"), []byte("x"), 0o600))

	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleBrowse(rec, httptest.NewRequest("GET", "/api/sandbox/browse?path="+url.QueryEscape(dir), nil))
	require.Equal(t, 200, rec.Code)

	var got browse.View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	names := []string{}
	for _, d := range got.Dirs {
		names = append(names, d.Name)
	}
	assert.Equal(t, []string{"notes", "service", ".cache"}, names, "directories, dotfiles last, no files")
	assert.Equal(t, filepath.Dir(dir), got.Parent)

	for _, d := range got.Dirs {
		assert.Equal(t, d.Name == "service", d.Repo, d.Name+": a checkout is marked")
	}
}

func TestBrowseRefusesARelativePath(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleBrowse(rec, httptest.NewRequest("GET", "/api/sandbox/browse?path=../..", nil))
	assert.Equal(t, 400, rec.Code)
}

func TestTheSandboxIsSwitchedOnAndOffForTheSessionInFlight(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())

	// No runtime is probed for turning it off: leaving a container never
	// depends on finding one.
	s.agent.setSandbox(true)
	rec := do(t, s, "PUT", "/api/sandbox/on", `{"on":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view sandboxView
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.False(t, view.On)
	assert.False(t, s.agent.State().Sandbox)

	// Turning it on finds the runtime now rather than failing on the next turn.
	was := detectRuntime
	t.Cleanup(func() { detectRuntime = was })
	detectRuntime = func(context.Context) (sandbox.Runtime, error) { return nil, errors.New("no container runtime") }
	rec = do(t, s, "PUT", "/api/sandbox/on", `{"on":true}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no container runtime")
	assert.False(t, s.agent.State().Sandbox, "a switch that failed did not flip")
}

func TestTheSandboxSwitchWaitsForTheTurnInFlight(t *testing.T) {
	s := newTestServer(t)
	s.agent.setSandbox(true)

	// Flipping kills the subprocess, which would take the run with it.
	_, ok := s.agent.begin(context.Background(), kindChat)
	require.True(t, ok)

	rec := do(t, s, "PUT", "/api/sandbox/on", `{"on":false}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "cancel it first")
	assert.True(t, s.agent.State().Sandbox)
}

func TestCodexCanBeSwitchedIntoAContainer(t *testing.T) {
	s := newTestServer(t)
	s.agent.resetProvider("codex", "")
	was := detectRuntime
	t.Cleanup(func() { detectRuntime = was })
	detectRuntime = func(context.Context) (sandbox.Runtime, error) { return sandbox.Docker{}, nil }

	rec := do(t, s, "PUT", "/api/sandbox/on", `{"on":true}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, s.agent.State().Sandbox)
}

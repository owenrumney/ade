package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

type switchable struct {
	Worktrees []struct {
		Path string `json:"path"`
		Main bool   `json:"main"`
	} `json:"worktrees"`
	Here string `json:"here"`
}

func TestTheSwitcherOffersThisRepositorysCheckouts(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.space().homeRoot, path, "cart"))

	rec := do(t, s, "GET", "/api/worktrees/switch", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got switchable
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, s.space().homeRoot, got.Here)
	require.Len(t, got.Worktrees, 2)
	assert.True(t, got.Worktrees[0].Main, "the main checkout comes first")
	// By name: git reports the real path, which a temp dir reaches by symlink.
	assert.Equal(t, filepath.Base(path), filepath.Base(got.Worktrees[1].Path))
}

// The path is gated on a fresh scan, not taken from the request: a switch is
// what the agent and the diff follow.
func TestSwitchingRefusesAPathTheScanDidNotOffer(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/worktrees/switch", `{"path":"`+t.TempDir()+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not one of this repository's worktrees")

	// The refusal came before the claim, so nothing is left holding the agent.
	_, free := s.agent.begin(context.Background(), "after")
	assert.True(t, free, "the agent lock leaked on a refused switch")
	s.agent.finish(nil)
}

func TestSwitchingWaitsForABusyAgent(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.space().homeRoot, path, "cart"))

	home := s.space().homeRoot
	_, free := s.agent.begin(context.Background(), "chat")
	require.True(t, free)
	t.Cleanup(func() { s.agent.finish(nil) })

	rec := do(t, s, "POST", "/api/worktrees/switch", `{"path":`+strconv.Quote(path)+`}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, home, s.space().homeRoot, "a busy agent is not moved out from under")
}

func TestSwitchingMovesTheWholeCheckout(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(t.TempDir(), "ade-cart")
	require.NoError(t, gitdiff.AddWorktree(context.Background(), s.space().homeRoot, path, "cart"))
	before := s.space()

	rec := do(t, s, "POST", "/api/worktrees/switch", `{"path":`+strconv.Quote(path)+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct{ Path, Branch string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "cart", got.Branch)

	after := s.space()
	assert.Equal(t, got.Path, after.homeRoot)
	// By name: git reports the real path, which a temp dir reaches by symlink.
	assert.Equal(t, filepath.Base(path), filepath.Base(after.homeRoot))
	assert.NotEqual(t, before.gitDir, after.gitDir, "the comment store moved with it")

	// Taken and given back: the next run is not refused by the switch.
	_, free := s.agent.begin(context.Background(), "after")
	assert.True(t, free, "the agent lock was not released")
	s.agent.finish(nil)
}

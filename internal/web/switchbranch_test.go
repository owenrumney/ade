package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/settings"
)

type offered struct {
	Branches []struct {
		Name    string `json:"name"`
		At      string `json:"at"`
		Stashed int    `json:"stashed"`
	} `json:"branches"`
}

// wroteIn is a file in the checkout, as an editor would leave it.
func wroteIn(t *testing.T, s *Server, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(s.space().homeRoot, name), []byte(body), 0o600))
}

func branches(t *testing.T, s *Server) offered {
	t.Helper()
	rec := do(t, s, "GET", "/api/worktrees/switch", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got offered
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

func switchTo(t *testing.T, s *Server, branch string) switched {
	t.Helper()
	rec := do(t, s, "POST", "/api/worktrees/switch", `{"branch":"`+branch+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got switched
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

func TestTheSwitcherOffersBranchesItIsNotAlreadyOn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	got := branches(t, s)
	require.Len(t, got.Branches, 1, "main is offered; feature is where we are")
	assert.Equal(t, "main", got.Branches[0].Name)
	assert.NotEmpty(t, got.Branches[0].At, "a row says how old the branch is")
	assert.Zero(t, got.Branches[0].Stashed)
}

func TestABranchAWorktreeHoldsIsNotOffered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	path := filepath.Join(t.TempDir(), "ade-main")
	// A worktree on main: git refuses a second checkout of it, so offering it
	// would be offering a refusal.
	require.NoError(t, gitdiff.AddWorktreeOn(context.Background(), s.space().homeRoot, path, "main"))

	assert.Empty(t, branches(t, s).Branches)
}

func TestSwitchingBranchStaysInTheSameCheckout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	home := s.space().homeRoot

	got := switchTo(t, s, "main")
	assert.Equal(t, "main", got.Branch)
	assert.Zero(t, got.Stashed)
	assert.Equal(t, home, s.space().homeRoot, "a branch switch moves nothing")
	assert.Equal(t, "main", gitdiff.Branch(context.Background(), home))

	// Taken and given back: the next run is not refused by the switch.
	_, free := s.agent.begin(context.Background(), "after")
	assert.True(t, free, "the agent lock was not released")
	s.agent.finish(nil)
}

func TestSwitchingRefusesABranchItDidNotOffer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	for _, name := range []string{"nope", "feature"} {
		rec := do(t, s, "POST", "/api/worktrees/switch", `{"branch":"`+name+`"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, name)
		assert.Contains(t, rec.Body.String(), "not a branch this checkout can switch to")
	}

	// The refusal came before the claim, so nothing is left holding the agent.
	_, free := s.agent.begin(context.Background(), "after")
	assert.True(t, free, "the agent lock leaked on a refused switch")
	s.agent.finish(nil)
}

func TestSwitchingBranchWaitsForABusyAgent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	_, free := s.agent.begin(context.Background(), "chat")
	require.True(t, free)
	t.Cleanup(func() { s.agent.finish(nil) })

	rec := do(t, s, "POST", "/api/worktrees/switch", `{"branch":"main"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "feature", gitdiff.Branch(context.Background(), s.space().homeRoot))
}

// The whole point: work in the way is set aside, recorded, and put back when
// you come back to the branch it came off.
func TestWorkInTheWayIsSetAsideAndComesBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	home := s.space().homeRoot
	repo := repoKey(context.Background(), home)

	// a.go differs between the branches, so an edit to it blocks the checkout.
	wroteIn(t, s, "a.go", "package main\n\nvar x = 99\n")

	away := switchTo(t, s, "main")
	assert.Equal(t, 1, away.Stashed, "the edit was set aside")
	assert.NotEmpty(t, settings.Stashed(repo, "feature"), "and recorded against the branch")
	body, err := os.ReadFile(filepath.Join(home, "a.go"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "var x = 1", "main's own copy is in the tree")

	// Only one row now, and it says what is waiting on it.
	rows := branches(t, s).Branches
	require.Len(t, rows, 1)
	assert.Equal(t, "feature", rows[0].Name)
	assert.Equal(t, 1, rows[0].Stashed)

	back := switchTo(t, s, "feature")
	assert.Equal(t, 1, back.Restored)
	assert.Empty(t, back.Stuck)
	assert.Empty(t, settings.Stashed(repo, "feature"), "the record goes with the entry")
	body, err = os.ReadFile(filepath.Join(home, "a.go"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "var x = 99", "the work came back")
}

// Work that does not conflict is never stashed: the tree is not moved out from
// under an editor for no reason.
func TestWorkThatCanComeAlongIsNotStashed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	wroteIn(t, s, "notes.md", "half a thought\n")

	got := switchTo(t, s, "main")
	assert.Zero(t, got.Stashed)
	body, err := os.ReadFile(filepath.Join(s.space().homeRoot, "notes.md"))
	require.NoError(t, err)
	assert.Equal(t, "half a thought\n", string(body), "it came across with the switch")
}

// A record whose entry someone dropped by hand says so, rather than reporting
// work put back that is not there.
func TestAStashDroppedByHandIsSaidAndForgotten(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	home := s.space().homeRoot
	repo := repoKey(context.Background(), home)

	require.NoError(t, settings.KeepStash(repo, "main", "0000000000000000000000000000000000000000"))

	got := switchTo(t, s, "main")
	assert.Zero(t, got.Restored)
	assert.Contains(t, got.Note, "no longer in the stash")
	assert.Empty(t, settings.Stashed(repo, "main"), "the record is dropped with it")
}

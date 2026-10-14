package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullBaseFailsWithoutARemote(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/base/pull", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "fetch origin/main")
}

// A rebase replays commits; uncommitted work is not replayed, it is lost. The
// server refuses rather than trusting the page to have disabled the button.
func TestRebaseRefusesOverUncommittedWork(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.startRoot, "a.go"), []byte("package main\n\nvar x = 3\n"), 0o600))

	rec := do(t, s, "POST", "/api/base/rebase", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "commit or stash")
	assert.Contains(t, rec.Body.String(), "loses them")
}

// The fetch comes before the replay, so a branch is rebased onto the base as
// it is now rather than onto a stale copy of it.
func TestRebaseStopsWhenTheBaseCannotBeFetched(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/base/rebase", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "fetch origin/main")
}

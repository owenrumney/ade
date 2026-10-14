package launcher

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/repostats"
)

func repoCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "owen@example.com"},
		{"config", "user.name", "Owen"},
		{"remote", "add", "origin", "git@github.com:owenrumney/ade.git"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600))
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", "first"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	return dir
}

func TestTheRepoEndpointAnswersWhatTheCheckoutIs(t *testing.T) {
	s := newTestLauncher(t)
	dir := repoCheckout(t)

	rec := do(t, s, http.MethodGet, "/api/repo?dir="+dir, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got repostats.Stats
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "owenrumney/ade", got.Repo)
	assert.Equal(t, "main", got.Branch)
	assert.Equal(t, 1, got.Files)
	require.NotEmpty(t, got.Languages)
	assert.Equal(t, "Go", got.Languages[0].Name)
	assert.Len(t, got.Days, 365, "a year, with the quiet days in it")
}

func TestTheRepoEndpointNeedsADirectoryThatIsOne(t *testing.T) {
	s := newTestLauncher(t)

	assert.Equal(t, http.StatusBadRequest, do(t, s, http.MethodGet, "/api/repo", "").Code)
	rec := do(t, s, http.MethodGet, "/api/repo?dir="+t.TempDir(), "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "not a git repository")
}

func TestARepoReadingIsCachedPerDirectory(t *testing.T) {
	s := newTestLauncher(t)
	dir := repoCheckout(t)

	require.Equal(t, http.StatusOK, do(t, s, http.MethodGet, "/api/repo?dir="+dir, "").Code)
	// A second file would change the count, but the cached reading stands:
	// counting the lines again is a read of every tracked file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "two.go"), []byte("package main\n"), 0o600))

	rec := do(t, s, http.MethodGet, "/api/repo?dir="+dir, "")
	var got repostats.Stats
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, 1, got.Files, "the reading inside the TTL is the one already taken")
}

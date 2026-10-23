package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func saveSource(t *testing.T, s *Server, path, source, was string) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"path": path, "source": source, "was": was})
	require.NoError(t, err)
	rec := do(t, s, http.MethodPut, "/api/source", string(body))
	return rec.Code, rec.Body.String()
}

func TestASavedFileIsWrittenAndKeepsItsMode(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(s.space().homeRoot, "a.go")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	code, body := saveSource(t, s, "a.go", "package main\n\nvar x = 3\n", sourceSum(before))
	require.Equal(t, http.StatusOK, code, body)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "package main\n\nvar x = 3\n", string(after))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestASaveOverSomeoneElsesChangeIsRefused(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(s.space().homeRoot, "a.go")
	opened, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("package main\n// the agent's\n"), 0o600))

	code, _ := saveSource(t, s, "a.go", "mine", sourceSum(opened))
	assert.Equal(t, http.StatusConflict, code)
	now, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "package main\n// the agent's\n", string(now))
}

func TestASaveOutsideTheCheckoutIsRefused(t *testing.T) {
	s := newTestServer(t)
	root := s.space().homeRoot
	outside := filepath.Join(t.TempDir(), "secret")
	require.NoError(t, os.WriteFile(outside, []byte("key"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

	for _, path := range []string{"", "../x", "/etc/passwd", "link", ".git/config", "sub/.git/HEAD", "new.go"} {
		code, _ := saveSource(t, s, path, "x", sourceSum([]byte("key")))
		assert.Contains(t, []int{http.StatusBadRequest, http.StatusNotFound}, code, path)
	}
	got, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "key", string(got))
}

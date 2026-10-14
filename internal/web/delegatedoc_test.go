package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/ade/internal/docsave"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asDelegate is a request carrying the capability token the agent holds,
// rather than the page's own.
func asDelegate(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, docsave.Path, strings.NewReader(body))
	req.Header.Set("X-Ade-Delegate-Token", s.delegateToken)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

// The condenser cannot write under .git itself, so this route is the only way
// its rewrite reaches the document.
func TestAnAgentWritesADocumentOverTheCapability(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "secret"
	writeDoc(t, s, "cart.md", "# Cart\n\nthe long version\n")

	rec := asDelegate(t, s, `{"name":"cart.md","body":"# Cart\n\nthe short version\n"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got docsave.Answer
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "cart.md", got.File)
	assert.Equal(t, filepath.Join(s.docsDir, "cart.md"), got.Path)

	body, err := os.ReadFile(filepath.Join(s.docsDir, "cart.md"))
	require.NoError(t, err)
	assert.Equal(t, "# Cart\n\nthe short version\n", string(body))
}

func TestThePagesOwnTokenDoesNotWriteADocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "secret"
	writeDoc(t, s, "cart.md", "original\n")

	rec := do(t, s, http.MethodPost, docsave.Path, `{"name":"cart.md","body":"replaced"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	body, err := os.ReadFile(filepath.Join(s.docsDir, "cart.md"))
	require.NoError(t, err)
	assert.Equal(t, "original\n", string(body))
}

// The capability hands back what the agent was given. A name nobody asked it
// to work on is not a document it may create.
func TestTheCapabilityCreatesNoDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "secret"
	require.NoError(t, os.MkdirAll(s.docsDir, 0o750))

	rec := asDelegate(t, s, `{"name":"invented.md","body":"hello"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NoFileExists(t, filepath.Join(s.docsDir, "invented.md"))
}

func TestAnEmptyBodyDoesNotWipeADocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "secret"
	writeDoc(t, s, "cart.md", "original\n")

	rec := asDelegate(t, s, `{"name":"cart.md","body":"   "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	body, err := os.ReadFile(filepath.Join(s.docsDir, "cart.md"))
	require.NoError(t, err)
	assert.Equal(t, "original\n", string(body))
}

// A name that climbs out of the docs directory is refused by docs.Resolve,
// the same as every other route that takes one.
func TestANameCannotClimbOutOfTheDocsDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.delegateToken = "secret"
	require.NoError(t, os.MkdirAll(s.docsDir, 0o750))
	outside := filepath.Join(filepath.Dir(s.docsDir), "escaped.md")
	require.NoError(t, os.WriteFile(outside, []byte("untouched\n"), 0o600))

	rec := asDelegate(t, s, `{"name":"../escaped.md","body":"owned"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	body, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, "untouched\n", string(body))
}

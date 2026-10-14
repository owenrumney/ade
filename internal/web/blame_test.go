package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blameAt(t *testing.T, s *Server, path string, line int) *httptest.ResponseRecorder {
	t.Helper()
	return do(t, s, "GET", fmt.Sprintf("/api/blame?path=%s&line=%d", path, line), "")
}

// The fixture's a.go has line 3 from the branch commit and line 1 from the
// base, so one file answers for both.
func TestBlameNamesTheCommitALineCameFrom(t *testing.T) {
	s := newTestServer(t)

	rec := blameAt(t, s, "a.go", 3)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		SHA       string `json:"sha"`
		Short     string `json:"short"`
		Summary   string `json:"summary"`
		Author    string `json:"author"`
		Committed bool   `json:"committed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "change", got.Summary)
	assert.Equal(t, "t", got.Author)
	assert.True(t, got.Committed)
	assert.Len(t, got.SHA, 40)
	assert.NotEmpty(t, got.Short)

	rec = blameAt(t, s, "a.go", 1)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "base", got.Summary, "an untouched line is still the base commit's")
}

// An uncommitted line is answered, not refused: the editor says so rather than
// showing an error for a line you just typed.
func TestBlameSaysWhenALineIsNotCommitted(t *testing.T) {
	s := newTestServer(t)
	path := filepath.Join(s.space().homeRoot, "a.go")
	require.NoError(t, os.WriteFile(path, []byte("package main\n\nvar x = 2\nvar y = 3\n"), 0o600))

	rec := blameAt(t, s, "a.go", 4)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Committed bool `json:"committed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.False(t, got.Committed)
}

func TestBlameRefusesAPathOutsideTheCheckout(t *testing.T) {
	s := newTestServer(t)

	for _, path := range []string{"", "%20", ".", "..%2F..%2Fetc%2Fpasswd", "%2Fetc%2Fpasswd"} {
		rec := blameAt(t, s, path, 1)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "path %q", path)
	}
}

func TestBlameNeedsALineNumber(t *testing.T) {
	s := newTestServer(t)

	for _, line := range []string{"", "0", "-3", "x"} {
		rec := do(t, s, "GET", "/api/blame?path=a.go&line="+line, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "line %q", line)
	}
}

func TestBlameHasNothingToSayPastTheEndOfAFile(t *testing.T) {
	s := newTestServer(t)

	rec := blameAt(t, s, "a.go", 9000)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no blame")
}

func TestHistoryListsWhatTouchedAFile(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/history?path=a.go", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Commits []struct {
			SHA     string `json:"sha"`
			Subject string `json:"subject"`
			Path    string `json:"path"`
		} `json:"commits"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Commits, 2)
	assert.Equal(t, "change", got.Commits[0].Subject, "newest first")
	assert.Equal(t, "base", got.Commits[1].Subject)
	assert.Equal(t, "a.go", got.Commits[0].Path)
}

func TestHistoryRefusesAPathOutsideTheCheckout(t *testing.T) {
	s := newTestServer(t)

	for _, path := range []string{"", "%20", ".", "..%2F..%2Fetc%2Fpasswd", "%2Fetc%2Fpasswd"} {
		rec := do(t, s, "GET", "/api/history?path="+path, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "path %q", path)
	}
}

func TestAFileAtACommitIsWhatThatCommitLeft(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/history?path=a.go", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Commits []struct {
			SHA  string `json:"sha"`
			Path string `json:"path"`
		} `json:"commits"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Commits, 2)

	at := func(i int) string {
		rec := do(t, s, "GET", fmt.Sprintf("/api/file/at?sha=%s&path=%s", got.Commits[i].SHA, got.Commits[i].Path), "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return rec.Body.String()
	}
	assert.Contains(t, at(0), "var x = 2")
	assert.Contains(t, at(1), "var x = 1", "the base commit, not the working tree")
}

// A ref is not a SHA: git would resolve HEAD@{1} and main^ too, and the
// argument comes from a history row, which always answers a SHA.
func TestAFileAtACommitTakesOnlyASHA(t *testing.T) {
	s := newTestServer(t)

	for _, sha := range []string{"", "HEAD", "main^", "feature", "../etc", "zzzzzzz", "abc"} {
		rec := do(t, s, "GET", "/api/file/at?sha="+url.QueryEscape(sha)+"&path=a.go", "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "sha %q", sha)
	}
}

func TestAFileAtACommitSaysWhenItWasNotThere(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/history?path=a.go", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Commits []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.NotEmpty(t, got.Commits)

	rec = do(t, s, "GET", "/api/file/at?sha="+got.Commits[0].SHA+"&path=never-existed.go", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "never-existed.go")
}

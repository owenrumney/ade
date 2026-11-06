package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func putShot(t *testing.T, s *Server, name string, body []byte, at time.Time) {
	t.Helper()
	box := s.outbox()
	require.NoError(t, os.MkdirAll(box, 0o750))
	to := filepath.Join(box, name)
	require.NoError(t, os.WriteFile(to, body, 0o600))
	require.NoError(t, os.Chtimes(to, at, at))
}

func TestShotsAreTheOutboxNewestFirst(t *testing.T) {
	s := newTestServer(t)
	now := time.Now()
	putShot(t, s, "old.png", []byte("one"), now.Add(-time.Hour))
	putShot(t, s, "new.png", []byte("two"), now)
	putShot(t, s, "notes.txt", []byte("three"), now)

	got := s.shots()
	require.Len(t, got, 2, "only pictures belong in the carousel")
	assert.Equal(t, "new.png", got[0].Name)
	assert.Equal(t, "old.png", got[1].Name)
	assert.EqualValues(t, 3, got[0].Size)
}

func TestShotsIsEmptyWithNoOutbox(t *testing.T) {
	assert.Empty(t, newTestServer(t).shots())
}

func TestAShotIsServedAsThePictureItIs(t *testing.T) {
	s := newTestServer(t)
	putShot(t, s, "shot.png", []byte("\x89PNG\r\n\x1a\n"), time.Now())

	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/shots/shot.png", nil)
	r.SetPathValue("name", "shot.png")
	s.handleShot(rec, r)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "\x89PNG\r\n\x1a\n", rec.Body.String())
}

// The name is a base name in the outbox: a path here would read any file the
// server can.
func TestAShotNameCannotLeaveTheOutbox(t *testing.T) {
	s := newTestServer(t)
	for _, name := range []string{"../../a.go", "a.go", "..", "sub/shot.png", ".hidden.png"} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/shots/x", nil)
		r.SetPathValue("name", name)
		s.handleShot(rec, r)
		assert.NotEqual(t, http.StatusOK, rec.Code, name)
	}
}

func TestTheStampChangesWhenTheOutboxDoes(t *testing.T) {
	s := newTestServer(t)
	now := time.Now()
	putShot(t, s, "shot.png", []byte("one"), now)
	was := shotsStamp(s.shots())

	putShot(t, s, "shot.png", []byte("one again"), now.Add(time.Second))
	assert.NotEqual(t, was, shotsStamp(s.shots()), "a redrawn picture is a change")

	putShot(t, s, "other.png", []byte("two"), now)
	assert.NotEqual(t, was, shotsStamp(s.shots()))
}

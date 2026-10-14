package launcher

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Answering reaches across to another session, so what it will take is
// guarded: an id this machine does not serve must not become a request.
func TestAnsweringASessionIsGuarded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/say", `{"id":"a","message":"  "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "say something")

	rec = do(t, s, http.MethodPost, "/api/say", `{"id":"nobody","message":"SQLite"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "this machine")
}

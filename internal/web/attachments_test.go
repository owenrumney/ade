package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
)

func TestAnAttachmentIsServedBackForItsThumbnail(t *testing.T) {
	s := newTestServer(t)

	files, err := chat.Save(s.space().gitDir, []chat.Incoming{
		{Name: "shot.png", MediaType: "image/png", Data: "aGk="},
	})
	require.NoError(t, err)

	rec := do(t, s, "GET", "/attachments/"+filepath.Base(files[0].Path), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "hi", rec.Body.String())
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}

// A purged attachment is a 404, which is what renders its name struck
// through rather than a broken picture.
func TestAPurgedAttachmentIsNotFound(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "GET", "/attachments/0001-gone.png", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAnAttachmentNameCannotReachAnythingElse(t *testing.T) {
	s := newTestServer(t)
	secret := filepath.Join(s.space().gitDir, "ade", "secret.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(secret), 0o750))
	require.NoError(t, os.WriteFile(secret, []byte("PRIVATE-CONTENTS"), 0o600))

	for _, name := range []string{"../secret.txt", "..%2Fsecret.txt", "....//secret.txt"} {
		rec := do(t, s, "GET", "/attachments/"+name, "")
		assert.NotEqual(t, http.StatusOK, rec.Code, name)
		assert.NotContains(t, rec.Body.String(), "PRIVATE-CONTENTS", name)
	}
}

// A type ade does not render is downloaded rather than served as a page: an
// attachment must not be talked into running on ade's own origin.
func TestAnUnknownTypeIsDownloadedRatherThanRendered(t *testing.T) {
	s := newTestServer(t)

	files, err := chat.Save(s.space().gitDir, []chat.Incoming{
		{Name: "page.html", MediaType: "text/html", Data: "aGk="},
	})
	require.NoError(t, err)

	rec := do(t, s, "GET", "/attachments/"+filepath.Base(files[0].Path), "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
}

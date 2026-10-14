package launcher

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The shell holds the launcher's event stream for as long as it runs, so a
// session started from a terminal must not also open a browser.
func TestWatchedAtAsksTheLauncherWithItsOwnToken(t *testing.T) {
	for _, one := range []struct {
		name    string
		answer  string
		status  int
		watched bool
	}{
		{name: "something is looking", answer: `{"watching":true}`, status: 200, watched: true},
		{name: "nothing is", answer: `{"watching":false}`, status: 200, watched: false},
		{name: "not json", answer: `<html>`, status: 200, watched: false},
		{name: "turned away", answer: `{}`, status: 403, watched: false},
	} {
		t.Run(one.name, func(t *testing.T) {
			var token, path string
			far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				token, path = r.Header.Get("X-Ade-Token"), r.URL.Path
				w.WriteHeader(one.status)
				_, _ = w.Write([]byte(one.answer))
			}))
			defer far.Close()

			got := WatchedAt(context.Background(), far.URL+"/?t=secret")
			assert.Equal(t, one.watched, got)
			assert.Equal(t, "/api/watching", path)
			// In the header, not the query: the token stays out of any log.
			assert.Equal(t, "secret", token)
		})
	}
}

func TestNoLauncherIsNotWatched(t *testing.T) {
	assert.False(t, WatchedAt(context.Background(), "http://127.0.0.1:1/?t=x"))
	assert.False(t, WatchedAt(context.Background(), "nonsense://"))
}

// The real endpoint, so the shape the caller decodes stays the shape served.
func TestWatchingCountsTheEventStreamsOpen(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "GET", "/api/watching", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"watching":false}`, rec.Body.String())
}

// A terminal that asked for a checkout wants that session in front of it, not
// a rail with it somewhere on the list.
func TestShowAtCarriesTheCheckoutAndReportsWhoTookIt(t *testing.T) {
	for _, one := range []struct {
		name     string
		answer   string
		dir      string
		revealed bool
	}{
		{name: "the shell took it", answer: `{"revealed":true}`, dir: "/repo", revealed: true},
		{name: "nobody is watching", answer: `{"revealed":false}`, dir: "/repo", revealed: false},
		{name: "not json", answer: `<html>`, dir: "/repo", revealed: false},
		{name: "no checkout to show", answer: `{"revealed":true}`, dir: "", revealed: false},
	} {
		t.Run(one.name, func(t *testing.T) {
			var token, path, body string
			far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				token, path, body = r.Header.Get("X-Ade-Token"), r.URL.Path, string(raw)
				_, _ = w.Write([]byte(one.answer))
			}))
			defer far.Close()

			got := ShowAt(context.Background(), far.URL+"/?t=secret", Reveal{Dir: one.dir, ID: "sess", URL: "http://127.0.0.1:9/?t=x"})
			assert.Equal(t, one.revealed, got)
			if one.dir == "" {
				assert.Empty(t, path, "asked with no checkout to name")
				return
			}
			assert.Equal(t, "/api/reveal", path)
			assert.Equal(t, "secret", token)
			assert.JSONEq(t, `{"dir":"/repo","id":"sess","url":"http://127.0.0.1:9/?t=x"}`, body)
		})
	}
}

func TestNoLauncherRevealsNothing(t *testing.T) {
	assert.False(t, ShowAt(context.Background(), "http://127.0.0.1:1/?t=x", Reveal{Dir: "/repo"}))
	assert.False(t, ShowAt(context.Background(), "nonsense://", Reveal{Dir: "/repo"}))
}

// The real endpoint: an unwatched launcher must say so, or the caller skips
// the browser and the session is opened nowhere at all.
func TestRevealIsRefusedWithoutAnAbsoluteCheckout(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/reveal", `{"dir":"relative"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRevealSaysNobodyTookItWithNothingWatching(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/reveal", `{"dir":"/repo","id":"sess","url":"http://127.0.0.1:9/"}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"revealed":false}`, rec.Body.String())
}

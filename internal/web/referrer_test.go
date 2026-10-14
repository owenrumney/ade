package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheTokenDoesNotLeaveWithAClick(t *testing.T) {
	// It rides in the query for an <img> and the SSE stream, so a link out of
	// a design document would otherwise carry the whole url to wherever it
	// points. See .git/ade/docs/boundaries.md, finding 1.
	s := newTestServer(t)
	for _, path := range []string{"/api/state", "/healthz", "/nope"} {
		rec := do(t, s, "GET", path, "")
		assert.Equalf(t, "no-referrer", rec.Header().Get("Referrer-Policy"), "%s leaks the referrer", path)
	}
}

package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheDevServerPortIsReadOffWhatItPrinted(t *testing.T) {
	vite := "VITE v7.1.0  ready in 412 ms\n\n  ➜  Local:   http://127.0.0.1:5173/\n  ➜  Network: use --host to expose"
	assert.Equal(t, []int{5173}, devPortsIn(vite))

	assert.Equal(t, []int{3000}, devPortsIn("Server listening on http://localhost:3000"))
}

// links.ts anchors a bare host:port too, and the two passes over one output
// have to agree: a link to a port nothing forwarded is a refused connection.
func TestABareHostAndPortIsADevServerToo(t *testing.T) {
	assert.Equal(t, []int{3000}, devPortsIn("Server listening on localhost:3000"))
	assert.Equal(t, []int{5173}, devPortsIn("  ➜  Local: 127.0.0.1:5173/"))
	assert.Empty(t, devPortsIn("see example.com:3000/docs"))
}

func TestEachPortIsAskedForOnce(t *testing.T) {
	twice := "http://127.0.0.1:5173/ and again http://127.0.0.1:5173/health"
	assert.Equal(t, []int{5173}, devPortsIn(twice))
}

// Prose is not an address, and a port out of range is not one either.
func TestProseIsNotADevServer(t *testing.T) {
	assert.Empty(t, devPortsIn("the build took 1:30 and passed"))
	assert.Empty(t, devPortsIn("see http://example.com:3000/docs"))
	assert.Empty(t, devPortsIn("http://127.0.0.1:99999/"))
}

func TestEveryPortOnOneOutputIsFound(t *testing.T) {
	both := "api on http://127.0.0.1:4000 and ui on http://localhost:5173"
	assert.Equal(t, []int{4000, 5173}, devPortsIn(both))
}

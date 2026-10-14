package launcher

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/httpx"
)

// Only a remote session opens a websocket through this proxy.
func TestTheProxyCarriesAWebsocket(t *testing.T) {
	// The guard the real terminal applies, so a proxy that mangles Origin on the
	// way through fails here rather than in a pane that never opens.
	s, _, front := proxied(t, func(w http.ResponseWriter, r *http.Request) {
		if !httpx.LoopbackOrigin(r.Header.Get("Origin")) {
			http.Error(w, "that origin may not open a terminal", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			return
		}
		defer conn.CloseNow() //nolint:errcheck // a dead socket is not news
		_ = conn.Write(r.Context(), websocket.MessageText, []byte("attached"))
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	at := strings.Replace(front.URL, "http://", "ws://", 1) + "/s/abc/api/terminal?t=" + s.token
	conn, _, err := websocket.Dial(ctx, at, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {front.URL}},
	})
	require.NoError(t, err, "the terminal pane never opens if this upgrade fails")
	defer conn.CloseNow() //nolint:errcheck // a dead socket is not news

	kind, got, err := conn.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, websocket.MessageText, kind)
	assert.Equal(t, "attached", string(got))
}

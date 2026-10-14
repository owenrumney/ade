package web

import (
	"net"
	"strings"

	"github.com/owenrumney/ade/internal/httpx"
)

// listen binds the server's port: the checkout's own, so the URL is the same
// one from last time and whatever is pointed at it stays pointed there.
func (s *Server) listen() (net.Listener, string, error) {
	return httpx.Listen(s.startRoot)
}

func portOf(base string) string {
	if i := strings.LastIndex(base, ":"); i >= 0 {
		return base[i+1:]
	}
	return ""
}

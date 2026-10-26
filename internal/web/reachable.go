package web

import (
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/owenrumney/ade/internal/httpx"
)

//nolint:gochecknoglobals // what this server opened beyond loopback
var alsoServing struct {
	sync.Mutex
	at map[string]bool
}

// alsoServe answers at one more address, for a box with no route to loopback.
func (s *Server) alsoServe(srv *http.Server, host string) {
	port := s.listenPort
	if host == "" || port == "" {
		return
	}
	alsoServing.Lock()
	if alsoServing.at == nil {
		alsoServing.at = map[string]bool{}
	}
	if alsoServing.at[host] {
		alsoServing.Unlock()
		return
	}
	alsoServing.at[host] = true
	alsoServing.Unlock()

	ln, err := httpx.AlsoOn(host, port)
	if err != nil || ln == nil {
		if err != nil {
			slog.Warn("a box will not be able to reach this server", "host", host, "err", err)
		}
		alsoServing.Lock()
		delete(alsoServing.at, host)
		alsoServing.Unlock()
		return
	}
	slog.Info("serving a container network as well", "addr", ln.Addr().String())
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("the container network listener stopped", "host", host, "err", err)
		}
	}()
}

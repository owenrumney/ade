package launcher

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/owenrumney/ade/internal/hub"
)

// hubRetry is how often a machine without a running hub is tried again. The
// binary appearing, or the hub dying, are both rare.
const hubRetry = 30 * time.Second

// keepHub has this machine's hub up while the launcher is. Inside a worker
// there is no binary and a container hub instead, so it quietly does nothing.
func (s *Server) keepHub(ctx context.Context) {
	if os.Getenv("ADE_HUB_CONTAINER") != "" {
		return
	}
	try := func() {
		_, err := hub.Ensure(ctx)
		if err != nil && !errors.Is(err, hub.ErrNotInstalled) {
			slog.Warn("mcp-hub is not running and could not be started", "err", err)
		}
	}
	try()
	ticker := time.NewTicker(hubRetry)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			try()
		}
	}
}

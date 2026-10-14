package remote

import (
	"context"
	"fmt"
)

// PrepareAgents installs the default @agents into the worker's HOME, keeping any
// already there. Nobody sits in front of a worker to run it, and @security needs it.
func (h *Host) PrepareAgents(ctx context.Context) error {
	if _, err := h.Run(ctx, "docker", "exec", workerName, "ade", "agents", "install"); err != nil {
		return fmt.Errorf("install the default agents on %s: %w", h.Name, err)
	}
	return nil
}

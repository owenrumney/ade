package remote

import (
	"context"
	"strings"
	"time"
)

// nameWait bounds waiting for docker to let go of a container name. A var so a
// test can shorten it rather than sleep through the real one.
var nameWait = 20 * time.Second

// nameInUse is docker refusing a name it has not finished freeing. `docker rm
// -f` returns when the kill is sent, not when the removal completes.
func nameInUse(err error) bool {
	return err != nil && strings.Contains(err.Error(), "is already in use by container")
}

// gone is docker saying the container is not there, as against the command
// not getting through. An ssh hiccup read as "free" is a blind re-run.
func gone(err error) bool {
	if err == nil {
		return false
	}
	said := strings.ToLower(err.Error())
	return strings.Contains(said, "no such container") || strings.Contains(said, "no such object")
}

// freeName asks again for the removal and waits for the name to go. The race
// is widest on a slow disk: a pi unmounts the layers long after rm returned.
func (h *Host) freeName(ctx context.Context, name string) bool {
	_, _ = h.Run(ctx, "docker", "rm", "-f", name)
	deadline := time.Now().Add(nameWait)
	for {
		if _, err := h.Run(ctx, "docker", "container", "inspect", "-f", "{{.Id}}", name); gone(err) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

package linear

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestReachLinearForReal is a probe, not a test: it talks to Linear. Skipped
// unless asked for, since a test suite must not depend on the network.
func TestReachLinearForReal(t *testing.T) {
	if os.Getenv("ADE_PROBE_LINEAR") == "" {
		t.Skip("set ADE_PROBE_LINEAR=1 to probe the real api.linear.app")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	start := time.Now()
	_, err := Client{Key: "probe-no-real-key"}.MyIssues(ctx)
	t.Logf("took %s, err: %v", time.Since(start).Round(time.Millisecond), err)
}

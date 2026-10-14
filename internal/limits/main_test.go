package limits

import (
	"os"
	"testing"
)

// TestMain keeps the suite out of the user's own ~/.ade. Per-test homes do not
// cover it alone: a refresh runs in a goroutine that outlives t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ade-limits-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

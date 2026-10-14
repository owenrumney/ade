package sessions

import (
	"os"
	"testing"
)

// TestMain keeps the suite out of the user's own ~/.ade. A test that wants a
// home of its own still takes one with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ade-sessions-test-")
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

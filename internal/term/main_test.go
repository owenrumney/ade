package term

import (
	"os"
	"testing"
)

// TestMain keeps the suite out of the user's own ~/.ade.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ade-term-test-")
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

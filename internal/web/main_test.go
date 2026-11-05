package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/ade/internal/agentshim"
)

// TestMain keeps the suite out of the user's own ~/.ade. A test that wants a
// home of its own still takes one with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ade-web-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", dir); err != nil {
		panic(err)
	}
	if err := agentshim.Install(dir); err != nil {
		panic(err)
	}
	// The code under test commits, and a machine may have no git identity.
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if err := os.Setenv(k, "t"); err != nil {
			panic(err)
		}
	}
	// HOME alone does not redirect git: a sandbox sets GIT_CONFIG_GLOBAL, and
	// a commit then fails on a signing key this home does not have.
	if err := os.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, ".gitconfig")); err != nil {
		panic(err)
	}
	launcherBinary = func() (string, error) { return "", errors.New("no launcher is started under test") }
	code := m.Run()
	_ = os.RemoveAll(dir)
	if left := Contained(); len(left) > 0 && code == 0 {
		fmt.Fprintf(os.Stderr, "panics the suite contained rather than fixed:\n%s\n", strings.Join(left, "\n"))
		code = 1
	}
	os.Exit(code)
}

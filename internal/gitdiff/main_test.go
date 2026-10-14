package gitdiff

import (
	"os"
	"testing"
)

// The code under test commits, and a machine may have no git identity.
func TestMain(m *testing.M) {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if err := os.Setenv(k, "t"); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

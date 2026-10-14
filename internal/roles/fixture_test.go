package roles

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

// The prompts ade ships are not in this repository, so every test that
// installs one brings its own. See internal/roles/defaults/README.
func stands(t *testing.T, files fstest.MapFS) {
	t.Helper()
	was := defaults
	defaults = fs.FS(files)
	t.Cleanup(func() { defaults = was })
}

func shippedRoles() fstest.MapFS {
	return fstest.MapFS{
		"defaults/README":  {Data: []byte("not a prompt\n")},
		"defaults/solo.md": {Data: []byte("keep the change small\n")},
	}
}

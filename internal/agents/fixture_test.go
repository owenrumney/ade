package agents

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

// The prompts ade ships are not in this repository, so every test that
// installs one brings its own. See internal/agents/defaults/README.
func stands(t *testing.T, files fstest.MapFS) {
	t.Helper()
	t.Cleanup(UseDefaults(fs.FS(files)))
}

func shippedTester() fstest.MapFS {
	return fstest.MapFS{
		"defaults/README":      {Data: []byte("not a prompt\n")},
		"defaults/tester.md":   {Data: []byte("---\nname: tester\ndescription: shipped\n---\nfind the missing case\n")},
		"defaults/security.md": {Data: []byte("---\nname: security\ndescription: shipped\n---\nlook for the injection\n")},
	}
}

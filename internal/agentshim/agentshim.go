// Package agentshim keeps a test suite from reaching a real agent cli: both
// the config and the job clock fall back to the binary on PATH when unstubbed.
package agentshim

import (
	"os"
	"path/filepath"
)

// Clis are the agent binaries a run could reach for.
var Clis = []string{"claude", "codex"} //nolint:gochecknoglobals // a list, not state

// Install writes a refusal for each under dir and puts it first on PATH.
func Install(dir string) error {
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		return err
	}
	const refuse = "#!/bin/sh\necho 'the test suite does not run real agents' >&2\nexit 1\n"
	for _, name := range Clis {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(refuse), 0o700); err != nil { //nolint:gosec // a shim this process just made
			return err
		}
	}
	return os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

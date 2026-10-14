// Package statedir resolves where ade keeps its state, and carries a directory
// left by the pre-rename name across the first time it is asked for one.
package statedir

import (
	"os"
	"path/filepath"
	"sync"
)

// legacy is the name ade went by before the rename.
const legacy = "sow"

// done keeps the migration to one stat per path per process.
var done sync.Map

// Git is ade's directory inside a git directory: .git/ade.
func Git(gitDir string) string { return adopt(gitDir, "ade") }

// Home is ade's own state directory, ~/.ade.
func Home(home string) string { return adopt(home, ".ade") }

// MachineDir holds the machine snapshot, and only that: ~/.ade itself holds
// credentials.
func MachineDir(home string) string { return filepath.Join(Home(home), "machine") }

// adopt returns parent/name, renaming a legacy directory into it first. A
// failed rename is not fatal: ade then starts from an empty directory.
func adopt(parent, name string) string {
	path := filepath.Join(parent, name)
	if _, seen := done.LoadOrStore(path, true); seen {
		return path
	}
	old := filepath.Join(parent, legacyName(name))
	// An empty directory is not state. A tool that made one ahead of the first
	// run would otherwise strand everything under the old name.
	if _, err := os.Stat(path); err == nil && !empty(path) {
		return path
	}
	if info, err := os.Stat(old); err != nil || !info.IsDir() {
		return path
	}
	_ = os.Remove(path)
	_ = os.Rename(old, path)
	return path
}

// empty reports whether path is a directory with nothing in it.
func empty(path string) bool {
	found, err := os.ReadDir(path)
	return err == nil && len(found) == 0
}

func legacyName(name string) string {
	if name == ".ade" {
		return "." + legacy
	}
	return legacy
}

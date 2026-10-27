package settings

import "strings"

// imported is the sessions pinned by id, each against the checkout holding it.
var imported = &notes{store{name: "imported.json"}}

// ImportedPath is the file naming the pinned sessions.
func ImportedPath() (string, error) { return imported.path() }

// Imported is every pinned session id, mapped to the checkout holding it.
func Imported() map[string]string { return imported.all() }

// SetImported pins a session to a checkout. An empty directory unpins it.
func SetImported(id, dir string) error {
	return imported.set(strings.TrimSpace(id), strings.TrimSpace(dir))
}

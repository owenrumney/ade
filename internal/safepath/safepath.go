// Package safepath resolves a name inside a directory, refusing anything that
// leaves it — by climbing out of it, or by following a link planted in it.
package safepath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Inside is root joined with name, unless that lands outside root once every
// link is followed. The path it returns is the unfollowed one.
func Inside(root, name string) (string, error) {
	clean := Relative(name)
	if root == "" || clean == "" {
		return "", fmt.Errorf("%q is not a name inside that directory", name)
	}
	path := filepath.Join(root, filepath.FromSlash(clean))
	realRoot, err := followed(root)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", root, err)
	}
	real, err := followed(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", name, err)
	}
	if !under(realRoot, real) {
		return "", fmt.Errorf("%q is not a name inside that directory", name)
	}
	return path, nil
}

// Relative normalises a relative name, or returns "" for one that is absolute,
// empty, or climbs out.
func Relative(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return ""
	}
	cleaned := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(name)), "./")
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return cleaned
}

// followed resolves every link in path, from the deepest part that exists: a
// document about to be created still has to land inside the root.
func followed(path string) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", os.ErrNotExist
	}
	up, err := followed(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(up, filepath.Base(path)), nil
}

func under(root, path string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

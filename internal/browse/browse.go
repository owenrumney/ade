// Package browse lists directories for a picker. Directories only and never a
// file's contents: a page needs somewhere to point, not a way to read the disk.
package browse

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// View is one directory as a picker walks it.
type View struct {
	// Path is the directory listed, absolute and cleaned.
	Path string `json:"path"`
	// Parent is the directory above, empty at the root.
	Parent string `json:"parent,omitempty"`
	// Home is where the picker opens, so the page can offer a way back.
	Home string `json:"home"`
	// Dirs are the subdirectories, by name, dotfiles last.
	Dirs []Dir `json:"dirs"`
}

// Dir is one subdirectory.
type Dir struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Repo marks a directory that is itself a checkout, which is usually what
	// is being looked for.
	Repo bool `json:"repo,omitempty"`
}

// ErrRelative is a path the caller could fix by being specific, as against a
// directory that is simply not there. The two are different answers.
var ErrRelative = errors.New("a path to list must be absolute")

// List reads one directory. An empty or ~-prefixed path is resolved against
// home, and the result is always absolute.
func List(path string) (View, error) {
	home, _ := os.UserHomeDir()
	dir := strings.TrimSpace(path)
	if dir == "" {
		dir = home
	}
	if home != "" && strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(home, dir[2:])
	}
	if !filepath.IsAbs(dir) {
		return View{}, ErrRelative
	}
	dir = filepath.Clean(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return View{}, err
	}
	view := View{Path: dir, Home: home, Dirs: []Dir{}}
	if parent := filepath.Dir(dir); parent != dir {
		view.Parent = parent
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// A symlink shows as a link, not a directory; resolve it, since a
		// checkout kept behind one is still a checkout.
		if e.Type()&os.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, e.Name())); err != nil || !info.IsDir() {
				continue
			}
		}
		full := filepath.Join(dir, e.Name())
		view.Dirs = append(view.Dirs, Dir{Name: e.Name(), Path: full, Repo: IsCheckout(full)})
	}
	sort.Slice(view.Dirs, func(i, j int) bool {
		a, b := view.Dirs[i], view.Dirs[j]
		if ad, bd := strings.HasPrefix(a.Name, "."), strings.HasPrefix(b.Name, "."); ad != bd {
			return bd
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return view, nil
}

// IsCheckout reports whether a directory holds a repository. A worktree's
// .git is a file pointing at the real one, so either kind counts.
func IsCheckout(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

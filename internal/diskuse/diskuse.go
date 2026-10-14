// Package diskuse measures what ade has left on disk: the state it keeps
// beside each checkout, and the state it keeps for itself.
package diskuse

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// parts is how many children are named: enough to say what dominates.
const parts = 3

// Part is one child: a file, or a subtree totalled.
type Part struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Dir   bool   `json:"dir,omitempty"`
}

// Store is one place ade keeps state, and what is inside it.
type Store struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`

	Parts []Part `json:"parts,omitempty"`
}

// Measure totals a directory and its largest few children.
func Measure(label, path string) Store { return measure(label, path, parts) }

// Detail is the same walk with every child named, for a view that has room.
func Detail(label, path string) Store { return measure(label, path, 0) }

// measure keeps the largest keep children, or all when keep is zero. A
// missing directory is zero: not every checkout has been served.
func measure(label, path string, keep int) Store {
	out := Store{Label: label, Path: path}
	entries, err := os.ReadDir(path)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		part := Part{Name: entry.Name(), Dir: entry.IsDir()}
		if entry.IsDir() {
			part.Bytes = tree(filepath.Join(path, entry.Name()))
		} else if info, err := entry.Info(); err == nil {
			part.Bytes = info.Size()
		}
		out.Bytes += part.Bytes
		out.Parts = append(out.Parts, part)
	}
	sort.SliceStable(out.Parts, func(i, j int) bool { return out.Parts[i].Bytes > out.Parts[j].Bytes })
	if keep > 0 && len(out.Parts) > keep {
		out.Parts = out.Parts[:keep]
	}
	return out
}

// MeasureAll walks each place once, dropping the ones holding nothing.
func MeasureAll(places []Store) []Store {
	var out []Store
	for _, s := range places {
		if got := Measure(s.Label, s.Path); got.Bytes > 0 {
			out = append(out, got)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out
}

// Total is what every measured place adds up to.
func Total(stores []Store) int64 {
	var total int64
	for _, s := range stores {
		total += s.Bytes
	}
	return total
}

// AdeDir is where a checkout's ade state lives: <git dir>/ade. A worktree's
// .git is a file naming the real directory, so it is read rather than joined.
func AdeDir(checkout string) string {
	dot := filepath.Join(checkout, ".git")
	info, err := os.Stat(dot)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return pick(dot)
	}
	data, err := os.ReadFile(dot) // #nosec G304 -- the checkout's own .git
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	git := strings.TrimSpace(rest)
	if !filepath.IsAbs(git) {
		git = filepath.Join(checkout, git)
	}
	return pick(git)
}

// tree totals a subtree; an unreadable entry is skipped rather than fatal.
func tree(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// pick names the state directory in a git dir. A checkout ade has not opened
// since the rename still has the old name, and its size still counts.
func pick(gitDir string) string {
	path := filepath.Join(gitDir, "ade")
	if _, err := os.Stat(path); err != nil {
		if old := filepath.Join(gitDir, "sow"); dirExists(old) {
			return old
		}
	}
	return path
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

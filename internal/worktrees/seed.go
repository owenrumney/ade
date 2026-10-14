package worktrees

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/statedir"
)

// Seed is the untracked files a new worktree starts with. It is the
// repository's, and lives in the main git dir so it is never committed.
type Seed struct {
	// Carry is copied, so a .env is editable per track.
	Carry []string `json:"carry"`
	// Link is symlinked, because a node_modules copy is a gigabyte and four
	// minutes. It points at the main checkout, which a sandbox does not mount.
	Link []string `json:"link"`
}

// SeedPath is the file for a repository, under its main git directory.
func SeedPath(commonGitDir string) string {
	if commonGitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(commonGitDir), "worktree.json")
}

// LoadSeed reads the file; none is an empty seed.
func LoadSeed(file string) (Seed, error) {
	seed := Seed{Carry: []string{}, Link: []string{}}
	if file == "" {
		return seed, nil
	}
	data, err := os.ReadFile(file) // #nosec G304 -- ade's own state under .git
	if errors.Is(err, os.ErrNotExist) {
		return seed, nil
	}
	if err != nil {
		return seed, fmt.Errorf("read %s: %w", file, err)
	}
	if err := json.Unmarshal(data, &seed); err != nil {
		return seed, fmt.Errorf("parse %s: %w", file, err)
	}
	if seed.Carry == nil {
		seed.Carry = []string{}
	}
	if seed.Link == nil {
		seed.Link = []string{}
	}
	return seed, nil
}

// Sow puts the repository's seed into a worktree just made, from the main
// checkout. A missing source is skipped: the list is a wish, not a manifest.
func Sow(ctx context.Context, root, path string) ([]string, error) {
	common, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return nil, err
	}
	seed, err := LoadSeed(SeedPath(common))
	if err != nil {
		return nil, err
	}
	return sow(filepath.Dir(common), path, seed)
}

func sow(from, to string, seed Seed) ([]string, error) {
	var placed []string
	var refused []string
	for _, spec := range []struct {
		list []string
		link bool
	}{{seed.Carry, false}, {seed.Link, true}} {
		for _, rel := range spec.list {
			src, dst, err := resolve(from, to, rel)
			if err != nil {
				refused = append(refused, err.Error())
				continue
			}
			if _, err := os.Lstat(src); err != nil {
				continue
			}
			if _, err := os.Lstat(dst); err == nil {
				continue
			}
			if err := place(from, src, dst, spec.link); err != nil {
				refused = append(refused, err.Error())
				continue
			}
			placed = append(placed, rel)
		}
	}
	if len(refused) > 0 {
		return placed, errors.New(strings.Join(refused, "; "))
	}
	return placed, nil
}

// resolve turns one entry into a source and a destination, refusing anything
// that would reach outside either checkout.
func resolve(from, to, rel string) (string, string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", "", errors.New("an empty path is not one to carry")
	}
	if filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("%s is absolute; a carried path is relative to the checkout", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%s reaches outside the checkout", rel)
	}
	src, dst := filepath.Join(from, clean), filepath.Join(to, clean)
	// Lexically clean is not enough: a symlink in either checkout can point
	// the same relative path anywhere on the disk.
	if err := inside(from, src); err != nil {
		return "", "", err
	}
	if err := inside(to, dst); err != nil {
		return "", "", err
	}
	return src, dst, nil
}

// inside is whether path lands under root once symlinks are followed, judged
// by the deepest parent that exists when the path itself does not yet.
func inside(root, path string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", root, err)
	}
	probe := path
	for {
		real, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
				return fmt.Errorf("%s resolves outside the checkout", path)
			}
			return nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return fmt.Errorf("resolve %s: %w", path, err)
		}
		probe = parent
	}
}

func place(root, src, dst string, link bool) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(dst), err)
	}
	if link {
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("link %s: %w", dst, err)
		}
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if info.IsDir() {
		if err := copyDir(root, src, dst); err != nil {
			// A half-copied tree would satisfy the next Sow's existence check,
			// so the seed would never be completed.
			_ = os.RemoveAll(dst)
			return fmt.Errorf("copy %s: %w", src, err)
		}
		return nil
	}
	return copyFile(src, dst, info.Mode().Perm())
}

// copyDir copies a tree with each file's own mode kept, since a carried
// directory can hold a 0600 secret. A symlink is reproduced, not followed, and
// only when it stays under root: one out of the checkout is a planted read.
func copyDir(root, src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		to := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			return os.MkdirAll(to, 0o750)
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if inside(root, resolveLink(path, target)) != nil {
				return nil
			}
			return os.Symlink(target, to)
		case info.Mode().IsRegular():
			return copyFile(path, to, info.Mode().Perm())
		default:
			return nil
		}
	})
}

// resolveLink is where a symlink points, read from where the link itself is.
func resolveLink(link, target string) string {
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(filepath.Dir(link), target)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src) // #nosec G304 -- a path the repository's own seed names
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) // #nosec G304 -- checked to be under the new worktree
	if err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("write %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

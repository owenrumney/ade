package lineage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/statedir"
)

// queueName is the file the post-commit hook appends a sha to, under the
// committing worktree's own state directory.
const queueName = "commits.queue"

// hookMark identifies a hook ade wrote, so an upgrade replaces its own and
// never someone else's.
const hookMark = "# ade post-commit: queue the sha for lineage"

// hookScript appends the new sha and nothing else. It never runs ade: inside
// a container ade need not exist, and its store would not be the host's.
const hookScript = `#!/bin/sh
` + hookMark + `
dir=$(git rev-parse --absolute-git-dir 2>/dev/null) || exit 0
[ -n "$dir" ] || exit 0
mkdir -p "$dir/ade" 2>/dev/null || exit 0
git rev-parse HEAD >> "$dir/ade/` + queueName + `" 2>/dev/null
exit 0
`

// QueuePath is where a worktree's queued shas land.
func QueuePath(gitDir string) string {
	return filepath.Join(statedir.Git(gitDir), queueName)
}

// InstallHook writes the post-commit hook for dir's repository. A hook ade did
// not write is left alone and reported: husky and pre-commit own that file.
func InstallHook(ctx context.Context, dir string) error {
	path, err := hookPath(ctx, dir)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path) // #nosec G304 -- path is git's own hooks dir
	switch {
	case err == nil && strings.Contains(string(existing), hookMark):
		if string(existing) == hookScript {
			return nil
		}
	case err == nil:
		return fmt.Errorf("%s is already someone else's hook, so commits made in a terminal will not be stamped", path)
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read post-commit hook: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(hookScript), 0o700); err != nil { // #nosec G306 -- a hook must be executable
		return fmt.Errorf("write post-commit hook: %w", err)
	}
	return nil
}

// Drain takes the shas the hook queued and empties the queue. The file is
// renamed before it is read, so a commit landing mid-drain is not lost.
func Drain(gitDir string) ([]string, error) {
	path := QueuePath(gitDir)
	taken := path + ".draining"

	// A leftover .draining is a previous drain that died after the rename.
	if _, err := os.Stat(taken); err != nil {
		if err := os.Rename(path, taken); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, fmt.Errorf("take commit queue: %w", err)
		}
	}

	data, err := os.ReadFile(taken) // #nosec G304 -- path is ade's own state dir
	if err != nil {
		return nil, fmt.Errorf("read commit queue: %w", err)
	}
	if err := os.Remove(taken); err != nil {
		return nil, fmt.Errorf("clear commit queue: %w", err)
	}

	var shas []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		sha := strings.TrimSpace(line)
		if sha == "" || seen[sha] {
			continue
		}
		seen[sha] = true
		shas = append(shas, sha)
	}
	return shas, nil
}

// hookPath is where post-commit belongs, honouring core.hooksPath.
func hookPath(ctx context.Context, dir string) (string, error) {
	if set := gitdiff.ConfigValue(ctx, dir, "core.hooksPath"); set != "" {
		if !filepath.IsAbs(set) {
			root, err := gitdiff.RepoRoot(ctx, dir)
			if err != nil {
				return "", err
			}
			set = filepath.Join(root, set)
		}
		return filepath.Join(set, "post-commit"), nil
	}
	common, err := gitdiff.CommonGitDir(ctx, dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "hooks", "post-commit"), nil
}

// Package scratch makes a checkout for work that has nowhere to live yet: an
// empty commit on main, the session on a branch above it. See the design doc.
package scratch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/settings"
)

// base is the branch a scratch is cut from, and the diff's base.
const base = "main"

//nolint:gochecknoglobals // a compiled pattern, not state
var slugAlpha = regexp.MustCompile(`[^a-z0-9]+`)

// New makes a scratch checkout for name and returns its path. The name is
// what the work is about, in whatever words; an empty one is dated instead.
func New(ctx context.Context, name string) (string, error) {
	root, err := settings.ScratchDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("make %s: %w", root, err)
	}

	dir, err := reserve(root, Slug(name))
	if err != nil {
		return "", err
	}
	branch := filepath.Base(dir)

	subject := strings.TrimSpace(name)
	if subject == "" {
		subject = branch
	}
	steps := [][]string{
		{"init", "-b", base},
		{"commit", "--allow-empty", "-m", "scratch: " + subject},
		{"checkout", "-b", branch},
	}
	for _, args := range steps {
		if err := git(ctx, dir, args...); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

// Slug turns what the work is about into a directory and branch name.
func Slug(name string) string {
	slug := slugAlpha.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	slug = strings.Trim(slug, "-")
	if parts := strings.Split(slug, "-"); len(parts) > 5 {
		slug = strings.Join(parts[:5], "-")
	}
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		return "scratch-" + time.Now().Format("20060102-150405")
	}
	return slug
}

// reserve makes the first free directory named after slug and returns it.
// Making it is the claim, so two calls at once cannot pick the same name.
func reserve(root, slug string) (string, error) {
	for n := 0; n < 100; n++ {
		dir := filepath.Join(root, slug)
		if n > 0 {
			dir = fmt.Sprintf("%s-%d", dir, n+1)
		}
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("make %s: %w", dir, err)
		}
	}
	return "", fmt.Errorf("there are already a hundred scratches called %s", slug)
}

// git folds what git said into the error; "exit status 1" says nothing.
func git(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- ade's own arguments
	cmd.Dir = dir
	// A machine with no configured identity would fail on the first commit,
	// and the empty base commit is ade's rather than anyone's work.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=ade", "GIT_AUTHOR_EMAIL=ade@localhost",
		"GIT_COMMITTER_NAME=ade", "GIT_COMMITTER_EMAIL=ade@localhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s in %s: %w: %s", args[0], dir, err, strings.TrimSpace(string(out)))
	}
	return nil
}

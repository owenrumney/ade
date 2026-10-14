package transcripts

import (
	"os"
	"path/filepath"
	"strings"
)

// GitBranch reads the checked-out branch for dir, walking up to the repository
// root. It reads .git/HEAD rather than shelling out to git.
func GitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	for {
		if branch, found := headOf(filepath.Join(dir, ".git")); found {
			return branch
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// headOf resolves HEAD under a .git directory, following the gitdir pointer a
// worktree leaves in its place. found stops the caller walking further up.
func headOf(gitPath string) (branch string, found bool) {
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", false
	}
	if !info.IsDir() {
		target, err := os.ReadFile(gitPath) // #nosec G304 -- path derived from the session's own working directory.
		if err != nil {
			return "", false
		}
		pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(target)), "gitdir:"))
		if pointer == "" {
			return "", true
		}
		if !filepath.IsAbs(pointer) {
			pointer = filepath.Join(filepath.Dir(gitPath), pointer)
		}
		gitPath = pointer
	}
	data, err := os.ReadFile(filepath.Join(gitPath, "HEAD")) // #nosec G304 -- path derived from the session's own working directory.
	if err != nil {
		return "", true
	}
	head := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(head, "ref: refs/heads/"); ok {
		return ref, true
	}
	// Detached: a short sha still says which commit you are on.
	if len(head) >= 7 && !strings.ContainsAny(head, " \t") {
		return head[:7], true
	}
	return "", true
}

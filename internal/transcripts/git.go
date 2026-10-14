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

// GitRemote is origin's url for the repository holding dir, as owner/name.
// It reads the config file rather than shelling out; empty with no origin.
func GitRemote(dir string) string {
	common, _ := gitDirs(dir)
	if common == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(common, "config")) // #nosec G304 -- path derived from the session's own working directory.
	if err != nil {
		return ""
	}
	return ownerName(originURL(string(data)))
}

// RepoKey is GitRemote, or the repository's directory name when there is no
// origin. A worktree is named for the repository it belongs to, not itself.
func RepoKey(dir string) string {
	if remote := GitRemote(dir); remote != "" {
		return remote
	}
	common, top := gitDirs(dir)
	if filepath.Base(common) == ".git" {
		return filepath.Base(filepath.Dir(common))
	}
	if top != "" {
		return filepath.Base(top)
	}
	return ""
}

// gitDirs finds the repository holding dir: common is the git directory its
// config lives in, following a worktree's gitdir and commondir; top holds .git.
func gitDirs(dir string) (common, top string) {
	if dir == "" {
		return "", ""
	}
	top = filepath.Clean(dir)
	for {
		gitPath := filepath.Join(top, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			if info.IsDir() {
				return gitPath, top
			}
			return worktreeCommon(gitPath), top
		}
		parent := filepath.Dir(top)
		if parent == top {
			return "", ""
		}
		top = parent
	}
}

func worktreeCommon(gitFile string) string {
	target, err := os.ReadFile(gitFile) // #nosec G304 -- path derived from the session's own working directory.
	if err != nil {
		return ""
	}
	pointer := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(target)), "gitdir:"))
	if pointer == "" {
		return ""
	}
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(filepath.Dir(gitFile), pointer)
	}
	common, err := os.ReadFile(filepath.Join(pointer, "commondir")) // #nosec G304 -- inside the repository's own git directory.
	if err != nil {
		return pointer
	}
	shared := strings.TrimSpace(string(common))
	if !filepath.IsAbs(shared) {
		shared = filepath.Join(pointer, shared)
	}
	return filepath.Clean(shared)
}

// originURL is the url under [remote "origin"] in a git config file.
func originURL(config string) string {
	inOrigin := false
	for _, line := range strings.Split(config, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inOrigin = strings.Join(strings.Fields(line), " ") == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "url" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ownerName is the last two path parts of a remote url, without .git:
// git@github.com:owner/name.git and https://host/owner/name both give owner/name.
func ownerName(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	if url == "" {
		return ""
	}
	if _, rest, ok := strings.Cut(url, "://"); ok {
		url = rest
	} else if _, rest, ok := strings.Cut(url, ":"); ok {
		url = rest
	}
	parts := strings.Split(strings.Trim(url, "/"), "/")
	if len(parts) < 2 {
		return parts[len(parts)-1]
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

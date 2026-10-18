package provision

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const (
	// DefaultRoot is where a provisioned Linux worker keeps its checkouts.
	DefaultRoot = "/srv/ade"
	Image       = "ghcr.io/owenrumney/ade-provisioner:latest"
)

// Root is where a session's clone lands: the work root, not the scan root.
// A worker that scans the user's own ~/code would otherwise clone into it.
func Root() string {
	for _, name := range []string{"ADE_WORK_DIR", "ADE_SCAN_DIR"} {
		if root := strings.TrimSpace(os.Getenv(name)); root != "" {
			return root
		}
	}
	return DefaultRoot
}

var shortRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var scpRepo = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:[A-Za-z0-9_./-]+$`)

type Runner func(context.Context, string, []string, io.Reader) error

// Clone creates one session-owned checkout through the disposable provisioner.
func Clone(ctx context.Context, ref, token string, run Runner) (string, error) {
	remote, name, err := repository(ref)
	if err != nil {
		return "", err
	}
	id, err := sessionID()
	if err != nil {
		return "", err
	}
	// git clone makes the leading directories, and only it can: the root is
	// the worker's, not this process's.
	root := Root()
	dir := filepath.Join(root, id, name)
	if strings.ContainsAny(token, "\r\n") {
		return "", fmt.Errorf("GitHub token contains a newline")
	}
	// docker run reuses whatever :latest this machine last pulled, so without
	// this a published provisioner never reaches it. A failed pull keeps that copy.
	_ = run(ctx, "docker", []string{"pull", "-q", Image}, strings.NewReader(""))
	input := strings.NewReader(token + "\n")
	args := []string{"run", "--rm", "-i", "-v", root + ":" + root}
	// As whoever owns the work root, not as root: a checkout the session's own
	// user cannot delete is one that survives the purge that should remove it.
	if who := rootOwner(root); who != "" {
		// gh writes its config on setup-git, and a uid with no passwd entry
		// has no home to write it to.
		args = append(args, "--user", who, "-e", "HOME=/tmp")
	}
	args = append(args, Image, remote, dir)
	if err := run(ctx, "docker", args, input); err != nil {
		// A partial checkout under a session id nothing recorded is disk no
		// later run can find, let alone purge.
		_ = os.RemoveAll(filepath.Join(root, id))
		return "", fmt.Errorf("clone %s: %w", ref, err)
	}
	return dir, nil
}

var unsafeBranch = regexp.MustCompile(`[^a-z0-9._-]+`)

// BranchName is the branch a design document names: its slug, the rule
// `ade create` uses for a worktree. Empty when the name gives nothing usable.
func BranchName(doc string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSuffix(doc, filepath.Ext(doc))))
	return strings.Trim(unsafeBranch.ReplaceAllString(base, "-"), "-.")
}

func Branch(ctx context.Context, dir, name string, run Runner) error {
	if name == "" || name != BranchName(name) {
		return fmt.Errorf("%q is not a usable branch name", name)
	}
	if err := run(ctx, "git", []string{"-C", dir, "switch", "-c", name}, strings.NewReader("")); err != nil {
		return fmt.Errorf("branch %s: %w", name, err)
	}
	return nil
}

// OwnedDir is the session directory a clone lives in, when root is one of
// Clone's checkouts. It is what finishing with that work removes.
func OwnedDir(root string) (string, bool) {
	owned := filepath.Dir(filepath.Clean(root))
	if !SessionDir(filepath.Base(owned)) || filepath.Dir(owned) != filepath.Clean(Root()) {
		return "", false
	}
	return owned, true
}

// rootOwner is the uid:gid that owns the work root, or "" when it cannot be
// read — in which case the container's default is no worse than before.
func rootOwner(root string) string {
	info, err := os.Stat(root)
	if err != nil {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)
}

func Run(ctx context.Context, name string, args []string, input io.Reader) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- fixed binary and validated repository
	cmd.Stdin = input
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// repository is the remote to clone and the directory name to put it in.
// Always an https URL: gh takes one, and so does a provisioner too old for gh.
func repository(ref string) (string, string, error) {
	ref = strings.TrimSpace(ref)
	host, path := "github.com", ""
	switch {
	case shortRepo.MatchString(ref):
		path = ref
	case scpRepo.MatchString(ref):
		host, path, _ = strings.Cut(ref[strings.Index(ref, "@")+1:], ":")
	default:
		parsed, err := url.Parse(ref)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "ssh") || parsed.Host == "" {
			return "", "", fmt.Errorf("repository must be org/repo or an https/ssh clone URL")
		}
		host, path = parsed.Host, parsed.Path
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	name := filepath.Base(path)
	if !repositoryName(name) || strings.Count(path, "/") != 1 {
		return "", "", fmt.Errorf("repository URL does not name a repository")
	}
	return "https://" + host + "/" + path + ".git", name, nil
}

func repositoryName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsRune(name, filepath.Separator)
}

// sessionIDBytes is how much randomness a session directory's name carries.
const sessionIDBytes = 8

var sessionDir = regexp.MustCompile(fmt.Sprintf(`^[0-9a-f]{%d}$`, 2*sessionIDBytes))

// SessionDir is whether name is one of Clone's session directories, so a
// scan can tell the level it adds from an owner's directory.
func SessionDir(name string) bool {
	return sessionDir.MatchString(name)
}

func sessionID() (string, error) {
	var raw [sessionIDBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("make session id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

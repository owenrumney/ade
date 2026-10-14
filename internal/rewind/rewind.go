// Package rewind snapshots a working tree before a turn and puts it back.
// The design is .git/ade/docs/turn-rewind.md.
package rewind

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Keep is how many snapshots a repository holds before the oldest go.
const Keep = 20

const refs = "refs/ade/turns/"

// Ref is the git ref a snapshot name resolves to.
func Ref(name string) string { return refs + name }

// ErrGone is a snapshot that has been pruned, or never was.
var ErrGone = errors.New("that snapshot is gone")

// ErrMoved is a commit made since the snapshot: restoring across it would
// show the commit as undone.
var ErrMoved = errors.New("HEAD has moved since then, so the files cannot be put back safely")

// Change is one file a restore would touch, and what it would do to it.
type Change struct {
	Path string `json:"path"`
	// Do is "restore" for a file changed since, "delete" for one created
	// since, and "recreate" for one deleted since.
	Do string `json:"do"`
}

// Save snapshots root's working tree and answers the name to restore it by.
// The real index and HEAD are not touched.
func Save(ctx context.Context, root string) (string, error) {
	tree, err := treeOf(ctx, root)
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", "ade: before a turn"}
	if head, err := git(ctx, root, nil, "rev-parse", "--verify", "-q", "HEAD"); err == nil {
		args = append(args, "-p", head)
	}
	commit, err := git(ctx, root, identity, args...)
	if err != nil {
		return "", err
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	name := hex.EncodeToString(raw)
	if _, err := git(ctx, root, nil, "update-ref", refs+name, commit); err != nil {
		return "", err
	}
	prune(ctx, root)
	return name, nil
}

// Plan is what restoring a snapshot would change, without changing it.
func Plan(ctx context.Context, root, name string) ([]Change, error) {
	if err := check(ctx, root, name); err != nil {
		return nil, err
	}
	now, err := treeOf(ctx, root)
	if err != nil {
		return nil, err
	}
	return diff(ctx, root, refs+name, now)
}

// Restore makes the working tree equal to a snapshot. It answers what it
// changed, and the name of a snapshot of the tree as it was just before.
func Restore(ctx context.Context, root, name string) ([]Change, string, error) {
	changes, err := Plan(ctx, root, name)
	if err != nil || len(changes) == 0 {
		return changes, "", err
	}
	// Resolved first: the save below prunes, and this may be the oldest.
	commit, err := git(ctx, root, nil, "rev-parse", "--verify", "-q", refs+name)
	if err != nil {
		return nil, "", ErrGone
	}
	before, err := Save(ctx, root)
	if err != nil {
		return nil, "", err
	}
	var back bytes.Buffer
	for _, change := range changes {
		if change.Do != "delete" {
			back.WriteString(change.Path)
			back.WriteByte(0)
			continue
		}
		if err := remove(root, change.Path); err != nil {
			return nil, "", err
		}
	}
	if back.Len() == 0 {
		return changes, before, nil
	}
	// A scratch index: checkout stages what it writes, and yours is yours.
	index, done, err := scratchIndex(ctx, root)
	if err != nil {
		return nil, "", err
	}
	defer done()
	cmd := exec.CommandContext(ctx, "git", "checkout", commit, "--pathspec-from-file=-", "--pathspec-file-nul") // #nosec G204 -- fixed
	cmd.Dir, cmd.Stdin = root, &back
	cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+index, "GIT_LITERAL_PATHSPECS=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("put the files back: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return changes, before, nil
}

var identity = []string{
	"GIT_AUTHOR_NAME=ade", "GIT_AUTHOR_EMAIL=ade@localhost",
	"GIT_COMMITTER_NAME=ade", "GIT_COMMITTER_EMAIL=ade@localhost",
}

// check refuses a snapshot that is gone, or one a commit has been made since.
func check(ctx context.Context, root, name string) error {
	if strings.Trim(name, "0123456789abcdef") != "" || name == "" {
		return ErrGone
	}
	if _, err := git(ctx, root, nil, "rev-parse", "--verify", "-q", refs+name); err != nil {
		return ErrGone
	}
	was, _ := git(ctx, root, nil, "rev-parse", "--verify", "-q", refs+name+"^")
	head, _ := git(ctx, root, nil, "rev-parse", "--verify", "-q", "HEAD")
	if was != head {
		return ErrMoved
	}
	return nil
}

// treeOf writes the working tree as a git tree: tracked and untracked files,
// not ignored ones.
func treeOf(ctx context.Context, root string) (string, error) {
	index, done, err := scratchIndex(ctx, root)
	if err != nil {
		return "", err
	}
	defer done()
	env := []string{"GIT_INDEX_FILE=" + index}
	if _, err := git(ctx, root, env, "add", "-A"); err != nil {
		return "", err
	}
	return git(ctx, root, env, "write-tree")
}

// scratchIndex is a copy of the real index, so only what changed is hashed.
func scratchIndex(ctx context.Context, root string) (string, func(), error) {
	real, err := git(ctx, root, nil, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", nil, err
	}
	scratch, err := os.CreateTemp(filepath.Dir(real), "ade-index-*")
	if err != nil {
		return "", nil, err
	}
	done := func() { _ = os.Remove(scratch.Name()) }
	defer func() { _ = scratch.Close() }()
	from, err := os.Open(real) // #nosec G304 -- git's own index
	if errors.Is(err, os.ErrNotExist) {
		// No index yet: git will not read an empty file as one.
		done()
		return scratch.Name(), done, nil
	}
	if err != nil {
		done()
		return "", nil, err
	}
	defer func() { _ = from.Close() }()
	if _, err := io.Copy(scratch, from); err != nil {
		done()
		return "", nil, err
	}
	// Git trusts an entry older than its index. A copy stamped now would make
	// it trust a same-size edit made in the second the real one was written.
	if info, err := from.Stat(); err == nil {
		_ = scratch.Close()
		_ = os.Chtimes(scratch.Name(), info.ModTime(), info.ModTime())
	}
	return scratch.Name(), done, nil
}

// diff is what turns the tree now into the snapshot.
func diff(ctx context.Context, root, snapshot, now string) ([]Change, error) {
	out, err := git(ctx, root, nil, "diff", "--name-status", "--no-renames", "-z", now, snapshot)
	if err != nil {
		return nil, err
	}
	does := map[string]string{"M": "restore", "T": "restore", "D": "delete", "A": "recreate"}
	fields := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	var changes []Change
	for i := 0; i+1 < len(fields); i += 2 {
		if do, ok := does[fields[i]]; ok {
			changes = append(changes, Change{Path: fields[i+1], Do: do})
		}
	}
	return changes, nil
}

// remove deletes a file the snapshot does not have, and the directories that
// leaves empty.
func remove(root, path string) error {
	if !filepath.IsLocal(path) {
		return fmt.Errorf("%q is not inside the checkout", path)
	}
	if err := os.Remove(filepath.Join(root, path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for dir := filepath.Dir(path); dir != "."; dir = filepath.Dir(dir) {
		if os.Remove(filepath.Join(root, dir)) != nil {
			break
		}
	}
	return nil
}

// prune drops the oldest snapshots past Keep. Best effort: a snapshot too
// many costs nothing.
func prune(ctx context.Context, root string) {
	out, err := git(ctx, root, nil, "for-each-ref", "--sort=-creatordate", "--format=%(refname)", refs)
	if err != nil {
		return
	}
	names := strings.Fields(out)
	for _, ref := range names[min(Keep, len(names)):] {
		_, _ = git(ctx, root, nil, "update-ref", "-d", ref)
	}
}

func git(ctx context.Context, root string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are built here
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

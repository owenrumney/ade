package designs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// branch is the one the store keeps. A design is a directory, so two
// machines touching different designs never conflict.
const branch = "main"

// gitWait bounds any one git call: a remote that hangs must not hang the
// launcher's sweep behind it.
const gitWait = 2 * time.Minute

// Store is the designs directory, and the remote it is shared through. With
// no remote it still works, as a directory on this machine alone.
type Store struct {
	root   string
	remote string

	mu  sync.Mutex
	run func(ctx context.Context, dir string, args ...string) (string, error)
}

func New(root, remote string) *Store {
	return &Store{root: root, remote: strings.TrimSpace(remote), run: runGit}
}

func (s *Store) Root() string   { return s.root }
func (s *Store) Shared() bool   { return s.remote != "" }
func (s *Store) Remote() string { return s.remote }

// Ensure makes the store: a clone when a remote is set and nothing is here
// yet, otherwise a repository of its own so the history starts either way.
func (s *Store) Ensure(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensure(ctx)
}

func (s *Store) ensure(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(s.root, ".git")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.root), 0o700); err != nil {
		return err
	}
	if s.remote != "" {
		if err := safeRemote(s.remote); err != nil {
			return err
		}
		if empty(s.root) {
			_, err := s.run(ctx, "", "clone", "--branch", branch, "--single-branch", "--", s.remote, s.root)
			if err == nil {
				return nil
			}
			// A remote with no commits yet cannot be cloned on a branch.
			// Start one here and let the first push create it.
		}
	}
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	if _, err := s.run(ctx, s.root, "init", "-b", branch); err != nil {
		return err
	}
	if s.remote != "" {
		if _, err := s.run(ctx, s.root, "remote", "add", "origin", s.remote); err != nil {
			return err
		}
	}
	_, err := s.run(ctx, s.root, "commit", "--allow-empty", "-m", "designs")
	return err
}

// Sync brings the store level with the remote and sends what is here. A
// conflict is git's to report and yours to resolve, in the store.
func (s *Store) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensure(ctx); err != nil {
		return err
	}
	if err := s.commitAll(ctx); err != nil {
		return err
	}
	if !s.Shared() {
		return nil
	}
	if _, err := s.run(ctx, s.root, "fetch", "origin", branch); err == nil {
		if _, err := s.run(ctx, s.root, "merge", "--no-edit", "origin/"+branch); err != nil {
			return fmt.Errorf("the designs store has a conflict to resolve in %s: %w", s.root, err)
		}
	}
	_, err := s.run(ctx, s.root, "push", "origin", branch)
	return err
}

// commitAll records whatever changed. Nothing to record is not an error:
// the sweep calls this whether or not anything was written.
func (s *Store) commitAll(ctx context.Context) error {
	if _, err := s.run(ctx, s.root, "add", "-A"); err != nil {
		return err
	}
	out, err := s.run(ctx, s.root, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	_, err = s.run(ctx, s.root, "commit", "-m", "designs: "+time.Now().UTC().Format(time.RFC3339))
	return err
}

// List and Read are the store's own directory, with no git in the way: a
// read must never wait on a remote.
func (s *Store) List() ([]Design, error)          { return List(s.root) }
func (s *Store) Read(name string) (Design, error) { return Read(s.root, name) }
func (s *Store) Dir(name string) string           { return filepath.Join(s.root, Slug(name)) }
func (s *Store) Write(name string, meta Meta, body []byte) (Design, error) {
	return Write(s.root, name, meta, body)
}
func (s *Store) Save(name string, meta Meta, body []byte) error {
	return Save(s.root, name, meta, body)
}

// safeRemote refuses one git would read as an option or a command.
func safeRemote(remote string) error {
	if strings.HasPrefix(remote, "-") {
		return errors.New("a designs remote may not start with '-'")
	}
	if strings.HasPrefix(strings.ToLower(remote), "ext::") {
		return errors.New("a designs remote may not use the ext:: transport")
	}
	return nil
}

func empty(dir string) bool {
	found, err := os.ReadDir(dir)
	return err != nil || len(found) == 0
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- ade's own arguments
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

package todo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	sharedBranch = "ade-todos"
	gitTimeout   = 20 * time.Second
	// How stale a read may be: a fetch per poll is the read on the network.
	syncEvery = 30 * time.Second
)

// Shared keeps append-only todo changes on the wiki remote's private branch.
type Shared struct {
	root, remote string
	// mu is the checkout: git and the event files it rewrites.
	mu sync.Mutex
	// smu guards when the last sync was and whether one is running.
	smu      sync.Mutex
	syncedAt time.Time
	syncing  bool
}

type change struct {
	Item    Item      `json:"item,omitempty"`
	Scope   string    `json:"scope,omitempty"`
	Deleted bool      `json:"deleted,omitempty"`
	At      time.Time `json:"at"`
}

func NewShared(root, remote string) *Shared {
	return &Shared{root: root, remote: strings.TrimSpace(remote)}
}

func (s *Shared) Enabled() bool { return s.remote != "" }

// List reads the newest change for every item in scope, global when the
// scope is empty. Only the first read waits for the remote.
func (s *Shared) List(ctx context.Context, scope string) ([]Item, error) {
	if s.cold() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.sync(ctx); err != nil {
			return nil, err
		}
		s.synced()
		return s.list(scope)
	}
	s.refresh()
	return s.list(scope)
}

func (s *Shared) cold() bool {
	s.smu.Lock()
	defer s.smu.Unlock()
	return s.syncedAt.IsZero()
}

func (s *Shared) synced() {
	s.smu.Lock()
	defer s.smu.Unlock()
	s.syncedAt = time.Now()
}

// refresh syncs behind the read: one at a time, not oftener than syncEvery,
// and a failure is timed too, so a dead remote is dialled no harder.
func (s *Shared) refresh() {
	s.smu.Lock()
	if s.syncing || time.Since(s.syncedAt) < syncEvery {
		s.smu.Unlock()
		return
	}
	s.syncing = true
	s.smu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
		defer cancel()
		s.mu.Lock()
		err := s.sync(ctx)
		s.mu.Unlock()
		if err != nil {
			slog.Warn("sync the shared todos", "err", err)
		}
		s.smu.Lock()
		s.syncing, s.syncedAt = false, time.Now()
		s.smu.Unlock()
	}()
}

func (s *Shared) Add(ctx context.Context, scope string, item Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return Item{}, err
	}
	item.Text = strings.TrimSpace(item.Text)
	if item.Text == "" {
		return Item{}, errors.New("a todo needs something to do")
	}
	if item.From == "" {
		item.From = FromYou
	}
	item.ID, item.CreatedAt = newID(), time.Now().UTC()
	if err := s.append(change{Item: item, Scope: scope, At: time.Now().UTC()}); err != nil {
		return Item{}, err
	}
	defer s.synced()
	return item, s.sync(ctx)
}

func (s *Shared) Update(ctx context.Context, scope, id string, p Patch) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return Item{}, err
	}
	items, err := s.list(scope)
	if err != nil {
		return Item{}, err
	}
	var item *Item
	for i := range items {
		if items[i].ID == id {
			item = &items[i]
			break
		}
	}
	if item == nil {
		return Item{}, ErrNotFound
	}
	if p.Text != nil {
		item.Text = strings.TrimSpace(*p.Text)
		if item.Text == "" {
			return Item{}, errors.New("a todo needs something to do")
		}
	}
	if p.Note != nil {
		item.Note = strings.TrimSpace(*p.Note)
	}
	if p.Due != nil {
		if p.Due.IsZero() {
			item.Due = nil
		} else {
			due := *p.Due
			item.Due = &due
		}
	}
	if p.Started != nil {
		item.Started = strings.TrimSpace(*p.Started)
	}
	if p.Done != nil {
		if *p.Done {
			now := time.Now().UTC()
			item.DoneAt = &now
		} else {
			item.DoneAt = nil
		}
	}
	if err := s.append(change{Item: *item, Scope: scope, At: time.Now().UTC()}); err != nil {
		return Item{}, err
	}
	defer s.synced()
	return *item, s.sync(ctx)
}

func (s *Shared) Delete(ctx context.Context, scope, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.sync(ctx); err != nil {
		return err
	}
	items, err := s.list(scope)
	if err != nil {
		return err
	}
	found := false
	for _, item := range items {
		found = found || item.ID == id
	}
	if !found {
		return ErrNotFound
	}
	if err := s.append(change{Item: Item{ID: id}, Scope: scope, Deleted: true, At: time.Now().UTC()}); err != nil {
		return err
	}
	defer s.synced()
	return s.sync(ctx)
}

func (s *Shared) list(scope string) ([]Item, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, "events"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	latest := map[string]change{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		// A sync behind this read may be rewriting the directory, so a
		// file that has gone is skipped rather than failing the read.
		body, err := os.ReadFile(filepath.Join(s.root, "events", entry.Name()))
		if err != nil {
			continue
		}
		var c change
		if json.Unmarshal(body, &c) != nil || c.Scope != scope || c.Item.ID == "" {
			continue
		}
		old, ok := latest[c.Item.ID]
		if !ok || c.At.After(old.At) || c.At.Equal(old.At) && entry.Name() > old.Item.ID {
			latest[c.Item.ID] = c
		}
	}
	out := []Item{}
	for _, c := range latest {
		if !c.Deleted {
			out = append(out, c.Item)
		}
	}
	Sort(out)
	return out, nil
}

func (s *Shared) append(c change) error {
	dir := filepath.Join(s.root, "events")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fmt.Sprintf("%020d-%s.json", c.At.UnixNano(), newID())), body, 0o600)
}

func (s *Shared) sync(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	if _, err := os.Stat(filepath.Join(s.root, ".git")); os.IsNotExist(err) {
		if err := os.MkdirAll(s.root, 0o700); err != nil {
			return err
		}
		if _, err := git(ctx, s.root, "init", "-b", sharedBranch); err != nil {
			return err
		}
		if _, err := git(ctx, s.root, "remote", "add", "origin", s.remote); err != nil {
			return err
		}
	}
	_, _ = git(ctx, s.root, "add", "events")
	// ade's own event log: a signer waiting on a touch or passphrase hung every read.
	_, _ = git(ctx, s.root, "-c", "commit.gpgsign=false", "commit", "-m", "todos")
	_, fetchErr := git(ctx, s.root, "fetch", "origin", sharedBranch)
	if _, err := git(ctx, s.root, "rev-parse", "--verify", "HEAD"); err != nil {
		// Never push an empty branch: git calls that a bad refspec.
		if fetchErr == nil {
			if _, err := git(ctx, s.root, "reset", "--hard", "origin/"+sharedBranch); err != nil {
				return err
			}
		}
		return nil
	}
	if fetchErr == nil {
		if _, err := git(ctx, s.root, "merge", "--no-edit", "origin/"+sharedBranch); err != nil {
			return err
		}
	}
	if _, err := git(ctx, s.root, "push", "origin", sharedBranch); err != nil {
		return err
	}
	return nil
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	limited, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(limited, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// With what git said: "exit status 1" names neither the rejection nor
		// the remote that did it.
		said := strings.TrimSpace(string(out))
		if said == "" {
			return string(out), fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, said)
	}
	return string(out), nil
}

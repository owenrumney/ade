// Package todo stores the things you meant to get to: one file for the
// machine, one per repository, so a repository's travel with its checkout.
package todo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/statedir"
)

// Who filed an item. The row names it, so a chatty agent is visible without a
// tray to accept from.
const (
	FromYou       = "you"
	FromAgent     = "agent"
	FromConcierge = "concierge"
)

// File is the store's name under both homes.
const File = "todos.json"

// ErrNotFound is returned when an id is not in the store it was asked of.
var ErrNotFound = errors.New("no such todo")

// Item is one thing to do. Everything but the text is optional and may arrive
// long after the capture that cost one keystroke.
type Item struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Note string `json:"note,omitempty"`

	// Due drives the badge and the brief. A pointer: a zero time.Time is not
	// omitted, and would read as overdue since year one.
	Due *time.Time `json:"due,omitempty"`

	From    string `json:"from,omitempty"`
	Session string `json:"session,omitempty"`

	// Started is the session this became once it became work.
	Started string `json:"started,omitempty"`

	CreatedAt time.Time  `json:"createdAt"`
	DoneAt    *time.Time `json:"doneAt,omitempty"`

	// Repo is which store answered, filled on the way out and never stored.
	// The file an item lives in is what scopes it.
	Repo string `json:"repo,omitempty"`
}

// Done reports whether this one is finished.
func (i Item) Done() bool { return i.DoneAt != nil && !i.DoneAt.IsZero() }

// Overdue reports whether its due date has passed.
func (i Item) Overdue(now time.Time) bool {
	return !i.Done() && i.Due != nil && !i.Due.IsZero() && now.After(*i.Due)
}

// Store is one todo file: the machine's, or one repository's.
type Store struct {
	mu    sync.Mutex
	path  string
	repo  string
	items []*Item
}

// GlobalPath is the machine's store, beside ade's own state.
func GlobalPath() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, File), nil
}

// RepoPath is a checkout's store, beside its designs and its comments.
func RepoPath(gitDir string) string { return filepath.Join(statedir.Git(gitDir), File) }

// Global opens the machine's store.
func Global() (*Store, error) {
	path, err := GlobalPath()
	if err != nil {
		return nil, err
	}
	return Open(path, "")
}

// Repo opens a checkout's store, labelled with the checkout it answers for.
func Repo(gitDir, repo string) (*Store, error) { return Open(RepoPath(gitDir), repo) }

// Open loads a store, creating nothing until the first write.
func Open(path, repo string) (*Store, error) {
	s := &Store{path: path, repo: repo}
	data, err := os.ReadFile(path) // #nosec G304 -- the path is ade's own state directory
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the todo store: %w", err)
	}
	if err := json.Unmarshal(data, &s.items); err != nil {
		return nil, fmt.Errorf("parse the todo store %s: %w", path, err)
	}
	return s, nil
}

// Path reports where this store is written.
func (s *Store) Path() string { return s.path }

// List returns every item, what is still open first and the soonest due ahead
// of the rest. The order is the whole value of the list being read at a glance.
func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Item, 0, len(s.items))
	for _, item := range s.items {
		copied := *item
		copied.Repo = s.repo
		out = append(out, copied)
	}
	Sort(out)
	return out
}

// Sort orders a list the way it is read: open before done, due before undated,
// soonest first, then newest.
func Sort(items []Item) {
	sort.SliceStable(items, func(a, b int) bool {
		x, y := items[a], items[b]
		if x.Done() != y.Done() {
			return !x.Done()
		}
		if dated(x) != dated(y) {
			return dated(x)
		}
		if dated(x) && !x.Due.Equal(*y.Due) {
			return x.Due.Before(*y.Due)
		}
		return x.CreatedAt.After(y.CreatedAt)
	})
}

func dated(i Item) bool { return i.Due != nil && !i.Due.IsZero() }

// Add files one. The text is all that is required of a capture.
func (s *Store) Add(item Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item.Text = strings.TrimSpace(item.Text)
	if item.Text == "" {
		return Item{}, errors.New("a todo needs something to do")
	}
	if item.From == "" {
		item.From = FromYou
	}
	item.ID = newID()
	item.CreatedAt = time.Now()
	item.DoneAt = nil
	item.Repo = ""

	s.items = append(s.items, &item)
	if err := s.save(); err != nil {
		return Item{}, err
	}
	item.Repo = s.repo
	return item, nil
}

// Patch is a change to one item. Every field is optional, and an unset one
// leaves what is stored alone.
type Patch struct {
	Text    *string
	Note    *string
	Due     *time.Time
	Done    *bool
	Started *string
}

// Update applies a patch. A nil Due means "unchanged", so a zero time is how a
// date is removed.
func (s *Store) Update(id string, p Patch) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := s.find(id)
	if item == nil {
		return Item{}, ErrNotFound
	}
	if p.Text != nil {
		text := strings.TrimSpace(*p.Text)
		if text == "" {
			return Item{}, errors.New("a todo needs something to do")
		}
		item.Text = text
	}
	if p.Note != nil {
		item.Note = strings.TrimSpace(*p.Note)
	}
	if p.Due != nil {
		if p.Due.IsZero() {
			item.Due = nil
		} else {
			when := *p.Due
			item.Due = &when
		}
	}
	if p.Started != nil {
		item.Started = strings.TrimSpace(*p.Started)
	}
	if p.Done != nil {
		if *p.Done {
			now := time.Now()
			item.DoneAt = &now
		} else {
			item.DoneAt = nil
		}
	}
	if err := s.save(); err != nil {
		return Item{}, err
	}
	out := *item
	out.Repo = s.repo
	return out, nil
}

// Delete removes one.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return s.save()
		}
	}
	return ErrNotFound
}

func (s *Store) find(id string) *Item {
	for _, item := range s.items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

// save writes atomically, so an interrupted write cannot lose the list.
func (s *Store) save() error {
	if len(s.items) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove the todo store: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create the todo store directory: %w", err)
	}
	data, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".ade-todo-*")
	if err != nil {
		return fmt.Errorf("create a temporary todo store: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write the todo store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("replace the todo store: %w", err)
	}
	return nil
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

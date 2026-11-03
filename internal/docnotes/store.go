// Package docnotes stores comments pinned to quoted text in a design document:
// the diff's review threads for prose, anchored to the text rather than a line.
package docnotes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/statedir"
)

// AuthorYou is a note you wrote. Anything else is an agent's name.
const AuthorYou = "you"

// ErrNotFound is returned when a note id does not exist.
var ErrNotFound = errors.New("note not found")

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Note is one conversation pinned to a quoted span of the document.
type Note struct {
	ID       string    `json:"id"`
	Anchor   Anchor    `json:"anchor"`
	Comments []Comment `json:"comments"`
	Resolved bool      `json:"resolved"`

	// Orphaned is set when the quote is no longer in the document. The note
	// stays: the sentence an agent deleted is the one worth reading about.
	Orphaned bool `json:"orphaned,omitempty"`

	// OrphanedAt is when the quote went. A pointer so a zero time cannot read
	// as "orphaned in year 1".
	OrphanedAt *time.Time `json:"orphanedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Comment is one message in a note.
type Comment struct {
	ID     string    `json:"id"`
	Author string    `json:"author"`
	Body   string    `json:"body"`
	At     time.Time `json:"at"`
}

// Store is the on-disk set of notes for one document.
type Store struct {
	mu    sync.Mutex
	path  string
	notes []*Note
}

// Open loads the notes for a document, creating nothing until first write.
// State lives under the git dir, so it is never committed and never staged.
func Open(gitDir, doc string) (*Store, error) {
	path := filepath.Join(statedir.Git(gitDir), "notes", Slug(doc)+".json")
	s := &Store{path: path}

	data, err := os.ReadFile(path) // #nosec G304 -- path is derived from the repo's own git dir
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read note store: %w", err)
	}
	if err := json.Unmarshal(data, &s.notes); err != nil {
		return nil, fmt.Errorf("parse note store %s: %w", path, err)
	}
	return s, nil
}

// Slug is a document name made safe to use as a filename.
func Slug(doc string) string {
	s := unsafeName.ReplaceAllString(strings.TrimSpace(doc), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "document"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// Path reports where the notes are persisted.
func (s *Store) Path() string { return s.path }

// List returns every note, oldest first.
func (s *Store) List() []Note {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Note, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n.clone())
	}
	return out
}

// Unresolved is what is still asking for an answer.
func (s *Store) Unresolved() []Note {
	var out []Note
	for _, n := range s.List() {
		if !n.Resolved {
			out = append(out, n)
		}
	}
	return out
}

// Get returns one note.
func (s *Store) Get(id string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := s.find(id)
	if found == nil {
		return Note{}, ErrNotFound
	}
	return found.clone(), nil
}

// Add files a note. The anchor must already have been located against the
// document: a note with no quote has nothing to point at.
func (s *Store) Add(n Note) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(n.Anchor.Quote) == "" {
		return Note{}, errors.New("a note needs a quote")
	}
	if len(n.Comments) == 0 || strings.TrimSpace(n.Comments[0].Body) == "" {
		return Note{}, errors.New("a note needs a first comment")
	}

	n.ID = newID()
	n.CreatedAt = time.Now()
	for i := range n.Comments {
		n.Comments[i].ID = newID()
		n.Comments[i].At = n.CreatedAt
		if n.Comments[i].Author == "" {
			n.Comments[i].Author = AuthorYou
		}
	}

	s.notes = append(s.notes, &n)
	if err := s.save(); err != nil {
		return Note{}, err
	}
	return n.clone(), nil
}

// Reply appends a comment and reopens the note: somebody had more to say.
func (s *Store) Reply(id, author, body string) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(body) == "" {
		return Note{}, errors.New("a reply needs a body")
	}
	found := s.find(id)
	if found == nil {
		return Note{}, ErrNotFound
	}
	if author == "" {
		author = AuthorYou
	}
	found.Comments = append(found.Comments, Comment{ID: newID(), Author: author, Body: body, At: time.Now()})
	found.Resolved = false
	if err := s.save(); err != nil {
		return Note{}, err
	}
	return found.clone(), nil
}

// SetResolved closes a note, or opens it again.
func (s *Store) SetResolved(id string, resolved bool) (Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	found := s.find(id)
	if found == nil {
		return Note{}, ErrNotFound
	}
	found.Resolved = resolved
	if err := s.save(); err != nil {
		return Note{}, err
	}
	return found.clone(), nil
}

// Delete removes a note outright.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, n := range s.notes {
		if n.ID != id {
			continue
		}
		s.notes = append(s.notes[:i], s.notes[i+1:]...)
		return s.save()
	}
	return ErrNotFound
}

// Reanchor moves every note to wherever its quote now lives. Nothing is swept:
// a quote the condenser deleted leaves the note worth reading.
func (s *Store) Reanchor(body string) []Note {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	changed := false
	for _, n := range s.notes {
		at, nth, ok := Find(body, n.Anchor)
		if !ok {
			if !n.Orphaned {
				when := now
				n.Orphaned, n.OrphanedAt, changed = true, &when, true
			}
			continue
		}
		if n.Anchor.At != at || n.Anchor.Nth != nth || n.Orphaned {
			n.Anchor.At, n.Anchor.Nth = at, nth
			n.Orphaned, n.OrphanedAt, changed = false, nil, true
		}
	}
	if changed {
		_ = s.save()
	}

	out := make([]Note, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n.clone())
	}
	return out
}

// save writes the store atomically, so an interrupted write cannot corrupt it.
func (s *Store) save() error {
	if len(s.notes) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove note store: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create note store dir: %w", err)
	}
	data, err := json.MarshalIndent(s.notes, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".ade-*")
	if err != nil {
		return fmt.Errorf("create temp note store: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write note store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("replace note store: %w", err)
	}
	return nil
}

func (n *Note) clone() Note {
	out := *n
	out.Comments = append([]Comment(nil), n.Comments...)
	return out
}

func (s *Store) find(id string) *Note {
	for _, n := range s.notes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("n%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

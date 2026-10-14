package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/settings"
)

// Caps on what reaches a prompt. Instructions are a screenful; memory is
// bounded so a long-remembered project cannot crowd out the conversation.
const (
	MaxInstructions = 4 << 10
	MaxMemory       = 8 << 10
)

const (
	projectFile      = "project.json"
	instructionsFile = "instructions.md"
	memoryFile       = "memory.json"
	proposedFile     = "proposed.json"
	consumedFile     = "consumed.json"
)

// writeMu serialises every read-modify-write in the store.
var writeMu sync.Mutex

// Project is project.json: which project this is, and whether it is remembered.
type Project struct {
	Slug   string `json:"slug"`
	Remote string `json:"remote,omitempty"`
	Path   string `json:"path,omitempty"`
	// Remembered is the opt-in. False stops distillation and injection without
	// discarding anything already gathered.
	Remembered bool      `json:"remembered"`
	Since      time.Time `json:"since,omitempty"`
}

// Store is one project's directory under ~/.ade/projects.
type Store struct {
	dir string
	id  Identity
}

// Root is where every project's store lives.
func Root() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "projects"), nil
}

// Open is the store for an identity. It creates nothing: a project nobody has
// marked remembered has no directory, and reads answer as empty.
func Open(id Identity) (*Store, error) {
	if id.Slug == "" {
		return nil, fmt.Errorf("no project identity")
	}
	root, err := Root()
	if err != nil {
		return nil, err
	}
	return &Store{dir: filepath.Join(root, id.Slug), id: id}, nil
}

// Identity is the project this store is for.
func (s *Store) Identity() Identity { return s.id }

// Dir is the store's directory, whether or not it exists.
func (s *Store) Dir() string { return s.dir }

// Project reads project.json. The identity wins where it has an answer; the
// file fills the rest, for a store opened from the ledger by slug alone.
func (s *Store) Project() Project {
	var p Project
	readJSON(s.path(projectFile), &p)
	p.Slug = s.id.Slug
	if s.id.Remote != "" {
		p.Remote = s.id.Remote
	}
	if s.id.Path != "" {
		p.Path = s.id.Path
	}
	return p
}

// Remembered is the switch, and the only thing distillation and injection ask.
func (s *Store) Remembered() bool { return s.Project().Remembered }

// Remember turns the project on or off, creating the store on the way on.
func (s *Store) Remember(on bool) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	p := s.Project()
	if p.Remembered == on {
		return nil
	}
	p.Remembered = on
	if on && p.Since.IsZero() {
		p.Since = time.Now()
	}
	return writeJSON(s.path(projectFile), p)
}

// Instructions is what the user wrote, or empty.
func (s *Store) Instructions() string {
	data, err := os.ReadFile(s.path(instructionsFile)) // #nosec G304 -- ade's own state directory
	if err != nil {
		return ""
	}
	return string(data)
}

// SetInstructions replaces them. Over the cap is refused rather than truncated:
// silently dropping the end of an instruction is worse than not saving it.
func (s *Store) SetInstructions(text string) error {
	if len(text) > MaxInstructions {
		return fmt.Errorf("instructions are %d bytes, over the %d byte cap", len(text), MaxInstructions)
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", s.dir, err)
	}
	return replace(s.path(instructionsFile), []byte(text))
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

// readJSON leaves the value alone when the file is missing or unreadable, which
// is the ordinary state of a project nobody has remembered yet.
func readJSON(path string, into any) {
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, into); err != nil {
		return
	}
}

func writeJSON(path string, from any) error {
	data, err := json.MarshalIndent(from, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	return replace(path, data)
}

// replace writes atomically, and 0600: instructions may hold dev credentials.
func replace(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

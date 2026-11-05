// Package runlog is ade's own record of the session in one checkout: which
// agent CLI, which of its conversations, and what Owen tagged it with.
package runlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/statedir"
)

// Record is the file's shape. One per checkout: a worktree has its own git
// dir, so its record is its own.
type Record struct {
	// Session is the CLI's own conversation id, Provider claude or codex.
	Session  string `json:"session,omitempty"`
	Provider string `json:"provider,omitempty"`

	Ticket string   `json:"ticket,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	// Worktrees are the other checkouts a session across repositories holds.
	Worktrees []string `json:"worktrees,omitempty"`

	Used time.Time `json:"used,omitzero"`
}

// Log is one checkout's record, read once and written through. A nil Log and
// one with no path remember nothing and write nothing.
type Log struct {
	mu       sync.Mutex
	path     string
	session  string
	provider string
}

func Open(path string) *Log {
	held := Read(path)
	return &Log{path: path, session: held.Session, provider: held.Provider}
}

// Read is the record at path. A missing or unreadable file is an empty one.
func Read(path string) Record {
	var held Record
	if path == "" {
		return held
	}
	data, err := os.ReadFile(path) // #nosec G304 -- derived from the repo's own git dir
	if err != nil {
		return held
	}
	if err := json.Unmarshal(data, &held); err != nil {
		return Record{}
	}
	return held
}

// writing serialises Update within this process; the rename keeps a reader whole.
var writing sync.Mutex

// Update changes the record at path, read fresh so another writer's fields stay.
func Update(path string, change func(*Record)) error {
	if path == "" {
		return nil
	}
	writing.Lock()
	defer writing.Unlock()
	held := Read(path)
	change(&held)
	data, err := json.MarshalIndent(held, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the session record: %w", err)
	}
	return writeFile(path, data)
}

func (l *Log) Session() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.session
}

func (l *Log) Provider() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.provider
}

func (l *Log) Worktrees() []string {
	if l == nil {
		return nil
	}
	return Read(l.path).Worktrees
}

func (l *Log) SetSession(id string) error { return l.SetAgentSession(id, "claude") }

// SetAgentSession records the conversation and its CLI, and that it was used.
func (l *Log) SetAgentSession(id, provider string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.session, l.provider = id, provider
	l.mu.Unlock()
	return Update(l.path, func(r *Record) {
		r.Session, r.Provider, r.Used = id, provider, time.Now()
	})
}

func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Log) Clear() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.session, l.provider = "", ""
	path := l.path
	l.mu.Unlock()

	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the session record: %w", err)
	}
	return nil
}

// writeFile replaces the file atomically, so a crash cannot leave half of one.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create the session record dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".ade-agent-*")
	if err != nil {
		return fmt.Errorf("create a temp session record: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write the session record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close the session record: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("chmod the session record: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replace the session record: %w", err)
	}
	return nil
}

// Path is where a checkout's record lives, keyed by its git dir alone.
func Path(gitDir string) string {
	if gitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(gitDir), "agent.json")
}

// Brief is the newest file under a git dir's ade/docs, the first dir that has
// one: the worktree's own before the repository's.
func Brief(gitDirs ...string) string {
	for _, gitDir := range gitDirs {
		if gitDir == "" {
			continue
		}
		if found := newestIn(filepath.Join(statedir.Git(gitDir), "docs")); found != "" {
			return found
		}
	}
	return ""
}

func newestIn(dir string) string {
	var found string
	var at time.Time
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().After(at) {
			found, at = path, info.ModTime()
		}
		return nil
	})
	return found
}
